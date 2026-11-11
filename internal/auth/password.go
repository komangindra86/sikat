package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Parameter Argon2id (rekomendasi OWASP: m=19 MiB, t=2, p=1).
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// Batas panjang password.
const (
	PasswordMinLen = 10
	PasswordMaxLen = 128
)

var errBadHash = errors.New("format hash password tidak dikenali")

// HashPassword menghasilkan hash Argon2id dalam format PHC.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword membandingkan password dengan hash secara constant-time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash dipakai saat username tidak ditemukan agar waktu respons login
// tidak membocorkan apakah username terdaftar.
var dummyHash = func() string {
	h, err := HashPassword("sikat-dummy-password-untuk-penyamaan-waktu")
	if err != nil {
		panic(err)
	}
	return h
}()

// ValidatePasswordPolicy memeriksa aturan password baru.
func ValidatePasswordPolicy(password, username string) error {
	n := utf8.RuneCountInString(password)
	if n < PasswordMinLen {
		return fmt.Errorf("password minimal %d karakter", PasswordMinLen)
	}
	if n > PasswordMaxLen {
		return fmt.Errorf("password maksimal %d karakter", PasswordMaxLen)
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password tidak boleh hanya spasi")
	}
	if username != "" && strings.Contains(strings.ToLower(password), strings.ToLower(username)) {
		return errors.New("password tidak boleh mengandung username")
	}
	return nil
}

// tempAlphabet tanpa karakter yang mudah tertukar (0/O, 1/l/I).
const tempAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateTempPassword membuat password sementara acak 12 karakter.
func GenerateTempPassword() (string, error) {
	const n = 12
	buf := make([]byte, n)
	out := make([]byte, n)
	max := byte(256 - 256%len(tempAlphabet)) // tolak nilai yang menyebabkan bias modulo
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if i >= n {
				break
			}
			if b >= max {
				continue
			}
			out[i] = tempAlphabet[int(b)%len(tempAlphabet)]
			i++
		}
	}
	return string(out), nil
}
