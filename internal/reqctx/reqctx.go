// Package reqctx menyimpan informasi per-request di context: ID request,
// alamat IP klien, user agent, dan pengguna yang login.
package reqctx

import (
	"context"

	"sikat/internal/rbac"
)

type key int

const (
	infoKey key = iota
	userKey
)

// Info adalah metadata request untuk log dan audit.
type Info struct {
	RequestID string
	IP        string
	UserAgent string
}

// WithInfo menyimpan Info ke context.
func WithInfo(ctx context.Context, info Info) context.Context {
	return context.WithValue(ctx, infoKey, info)
}

// InfoFrom membaca Info dari context (nilai kosong bila tidak ada).
func InfoFrom(ctx context.Context) Info {
	info, _ := ctx.Value(infoKey).(Info)
	return info
}

// WithUser menyimpan pengguna login ke context.
func WithUser(ctx context.Context, u *rbac.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFrom membaca pengguna login dari context (nil bila anonim).
func UserFrom(ctx context.Context) *rbac.User {
	u, _ := ctx.Value(userKey).(*rbac.User)
	return u
}
