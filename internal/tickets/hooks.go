package tickets

import (
	"context"
	"database/sql"
	"time"
)

// CreatedEvent dikirim ke Hooks saat tiket yang terkait perangkat dibuat.
type CreatedEvent struct {
	TicketID         int64
	AssetID          int64
	TicketCategoryID int64
	CreatedAt        time.Time
}

// PartLine adalah satu baris sparepart dari form penyelesaian (nilai mentah).
type PartLine struct {
	PartID string
	Qty    string
}

// ResolvedEvent dikirim ke Hooks saat tiket yang terkait perangkat diselesaikan.
type ResolvedEvent struct {
	TicketID         int64
	AssetID          int64
	TicketCategoryID int64
	ActorID          int64
	Cycle            int
	Complaint        string
	Action           string
	Result           string
	Note             string
	ProblemStartedAt time.Time
	StartedAt        *time.Time
	FinishedAt       time.Time
	ServiceCost      string
	AssetStatusAfter string
	Parts            []PartLine
}

// Hooks dipanggil di dalam transaksi tiket sehingga efek lanjutan (riwayat
// perangkat, deteksi kerusakan berulang) ikut batal bila transaksi gagal.
type Hooks interface {
	TicketCreated(ctx context.Context, tx *sql.Tx, ev CreatedEvent) error
	// ValidateResolution memeriksa isian khusus perangkat (sparepart, biaya, kondisi).
	ValidateResolution(ctx context.Context, tx *sql.Tx, ev ResolvedEvent) (map[string]string, error)
	TicketResolved(ctx context.Context, tx *sql.Tx, ev ResolvedEvent) error
}

// SetHooks memasang Hooks (dipanggil saat perakitan server).
func (s *Service) SetHooks(h Hooks) { s.hooks = h }
