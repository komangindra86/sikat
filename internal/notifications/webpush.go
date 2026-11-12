package notifications

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Web Push tanpa dependensi luar:
//   - enkripsi payload "aes128gcm" (RFC 8291 + RFC 8188),
//   - autentikasi server VAPID (RFC 8292, JWT ES256).
// Kunci VAPID memakai format yang lazim (web-push / webpush-go): base64url tanpa
// padding, kunci privat = skalar P-256 32 byte, kunci publik = titik tak
// terkompresi 65 byte.

var b64 = base64.RawURLEncoding

// VAPID adalah pasangan kunci server untuk Web Push.
type VAPID struct {
	priv      *ecdsa.PrivateKey
	PublicKey string // base64url, dipakai browser sebagai applicationServerKey
	Subject   string // mailto: atau https: kontak pengelola
}

// GenerateVAPID membuat pasangan kunci baru (perintah "sikat vapid-keys").
func GenerateVAPID() (public, private string, err error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		return "", "", err
	}
	raw, err := k.Bytes()
	if err != nil {
		return "", "", err
	}
	return b64.EncodeToString(pub), b64.EncodeToString(raw), nil
}

// ParseVAPID memvalidasi kunci dari konfigurasi.
func ParseVAPID(public, private, subject string) (*VAPID, error) {
	raw, err := decodeB64(private)
	if err != nil {
		return nil, errors.New("SIKAT_VAPID_PRIVATE_KEY bukan base64url yang valid")
	}
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, errors.New("SIKAT_VAPID_PRIVATE_KEY bukan kunci P-256 yang valid")
	}
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	given, err := decodeB64(public)
	if err != nil || !bytes.Equal(given, pub) {
		return nil, errors.New("SIKAT_VAPID_PUBLIC_KEY tidak cocok dengan kunci privat (buat ulang dengan: sikat vapid-keys)")
	}
	u, err := url.Parse(subject)
	if err != nil || (u.Scheme != "mailto" && u.Scheme != "https") {
		return nil, errors.New("SIKAT_VAPID_SUBJECT harus mailto:alamat atau URL https://")
	}
	return &VAPID{priv: k, PublicKey: b64.EncodeToString(pub), Subject: subject}, nil
}

// decodeB64 menerima base64url atau base64 standar, dengan/tanpa padding.
func decodeB64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("base64 tidak valid")
}

// token membuat JWT VAPID untuk origin push service.
func (v *VAPID) token(audience string, now time.Time) (string, error) {
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": audience, "exp": now.Add(12 * time.Hour).Unix(), "sub": v.Subject})
	signing := header + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, v.priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

// verifyToken memeriksa JWT VAPID (dipakai test).
func verifyToken(publicKey, token string) (map[string]any, error) {
	parts := bytes.Split([]byte(token), []byte("."))
	if len(parts) != 3 {
		return nil, errors.New("format JWT")
	}
	pub, err := decodeB64(publicKey)
	if err != nil {
		return nil, err
	}
	pk, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pub)
	if err != nil {
		return nil, err
	}
	sig, err := b64.DecodeString(string(parts[2]))
	if err != nil || len(sig) != 64 {
		return nil, errors.New("tanda tangan")
	}
	sum := sha256.Sum256(append(append(append([]byte{}, parts[0]...), '.'), parts[1]...))
	if !ecdsa.Verify(pk, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return nil, errors.New("tanda tangan tidak valid")
	}
	raw, err := b64.DecodeString(string(parts[1]))
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	return claims, json.Unmarshal(raw, &claims)
}

// Subscription adalah tujuan push satu perangkat.
type Subscription struct {
	ID       int64
	Endpoint string
	P256dh   string
	Auth     string
}

// encrypt mengenkripsi payload untuk satu langganan (RFC 8291, satu record).
func encrypt(sub Subscription, plaintext []byte) ([]byte, error) {
	uaPub, err := decodeB64(sub.P256dh)
	if err != nil {
		return nil, fmt.Errorf("%w: p256dh", errInvalidSub)
	}
	authSecret, err := decodeB64(sub.Auth)
	if err != nil || len(authSecret) < 16 {
		return nil, fmt.Errorf("%w: auth", errInvalidSub)
	}
	curve := ecdh.P256()
	uaKey, err := curve.NewPublicKey(uaPub)
	if err != nil {
		return nil, fmt.Errorf("%w: p256dh bukan titik P-256", errInvalidSub)
	}
	asKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := asKey.ECDH(uaKey)
	if err != nil {
		return nil, err
	}
	asPub := asKey.PublicKey().Bytes()

	prkKey, err := hkdf.Extract(sha256.New, shared, authSecret)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPub) + string(asPub)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Header: salt(16) | rs(4) | idlen(1) | keyid(65). Record tunggal diakhiri delimiter 0x02.
	var out bytes.Buffer
	out.Write(salt)
	_ = binary.Write(&out, binary.BigEndian, uint32(4096))
	out.WriteByte(byte(len(asPub)))
	out.Write(asPub)
	out.Write(gcm.Seal(nil, nonce, append(append([]byte{}, plaintext...), 0x02), nil))
	return out.Bytes(), nil
}

// Message adalah isi push (JSON yang dibaca service worker).
type Message struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Tag    string `json:"tag,omitempty"`
	Urgent bool   `json:"urgent,omitempty"`
}

// Sender mengirim satu push. Mengembalikan status HTTP dari push service.
type Sender interface {
	Send(ctx context.Context, sub Subscription, msg Message, ttl time.Duration) (int, error)
}

// WebPush adalah Sender sungguhan via HTTP.
type WebPush struct {
	VAPID  *VAPID
	Client *http.Client
	Now    func() time.Time
}

// maxPayload: batas aman ukuran plaintext (push service menerima ~4 KB terenkripsi).
const maxPayload = 3000

// Send mengenkripsi dan mengirim pesan ke endpoint langganan.
func (w *WebPush) Send(ctx context.Context, sub Subscription, msg Message, ttl time.Duration) (int, error) {
	u, err := url.Parse(sub.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return 0, fmt.Errorf("%w: endpoint", errInvalidSub)
	}
	plain, _ := json.Marshal(msg)
	if len(plain) > maxPayload {
		return 0, fmt.Errorf("%w: payload terlalu besar", errInvalidSub)
	}
	body, err := encrypt(sub, plain)
	if err != nil {
		return 0, err
	}
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	jwt, err := w.VAPID.token(u.Scheme+"://"+u.Host, now())
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	secs := int(ttl / time.Second)
	if secs < 0 {
		secs = 0
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(secs))
	urgency := "normal"
	if msg.Urgent {
		urgency = "high"
	}
	req.Header.Set("Urgency", urgency)
	if msg.Tag != "" {
		req.Header.Set("Topic", topic(msg.Tag))
	}
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s, k=%s", jwt, w.VAPID.PublicKey))
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

// topic: header Topic maksimal 32 karakter base64url; push service mengganti
// pesan tertunda dengan topic yang sama (mis. beberapa kabar untuk tiket yang sama).
func topic(tag string) string {
	sum := sha256.Sum256([]byte(tag))
	return b64.EncodeToString(sum[:])[:32]
}
