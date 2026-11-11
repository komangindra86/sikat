package users

import (
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,49}$`)
var phoneRe = regexp.MustCompile(`^[0-9+ ()-]{3,30}$`)

// ValidateUsername memeriksa format username.
func ValidateUsername(u string) string {
	if u == "" {
		return "Username wajib diisi."
	}
	if !usernameRe.MatchString(u) {
		return "Username 3–50 karakter: huruf kecil, angka, titik, garis bawah, atau strip; diawali huruf/angka."
	}
	return ""
}

// Validate memeriksa input form pengguna. Bila withUsername false (edit),
// username tidak dibaca dari form.
func Validate(get func(string) string, getAll func(string) []string, withUsername bool) (Input, map[string]string, map[string]string) {
	errs := map[string]string{}
	form := map[string]string{}
	trim := func(k string) string {
		v := strings.TrimSpace(get(k))
		form[k] = v
		return v
	}

	var in Input
	if withUsername {
		in.Username = strings.ToLower(trim("username"))
		form["username"] = in.Username
		if msg := ValidateUsername(in.Username); msg != "" {
			errs["username"] = msg
		}
	}

	in.FullName = trim("full_name")
	switch {
	case in.FullName == "":
		errs["full_name"] = "Nama lengkap wajib diisi."
	case utf8.RuneCountInString(in.FullName) > 150:
		errs["full_name"] = "Nama lengkap maksimal 150 karakter."
	}

	in.NIP = trim("nip")
	if utf8.RuneCountInString(in.NIP) > 30 {
		errs["nip"] = "NIP maksimal 30 karakter."
	}
	in.Phone = trim("phone")
	if in.Phone != "" && !phoneRe.MatchString(in.Phone) {
		errs["phone"] = "Nomor HP/ekstensi hanya boleh berisi angka, spasi, +, -, ( )."
	}
	in.Email = strings.ToLower(trim("email"))
	form["email"] = in.Email
	if in.Email != "" {
		if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email || len(in.Email) > 150 {
			errs["email"] = "Format email tidak valid."
		}
	}

	if v := trim("role_id"); v == "" {
		errs["role_id"] = "Role wajib dipilih."
	} else if n, err := strconv.ParseInt(v, 10, 64); err != nil || n <= 0 {
		errs["role_id"] = "Role tidak valid."
	} else {
		in.RoleID = n
	}

	if v := trim("department_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err != nil || n <= 0 {
			errs["department_id"] = "Unit tidak valid."
		} else {
			in.DepartmentID = n
		}
	}

	seen := map[int64]bool{}
	for _, v := range getAll("competencies") {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || n <= 0 {
			errs["competencies"] = "Pilihan kompetensi tidak valid."
			continue
		}
		if !seen[n] {
			seen[n] = true
			in.Competencies = append(in.Competencies, n)
		}
	}
	return in, form, errs
}
