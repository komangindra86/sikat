// Package notifications berisi notifikasi berbasis event: outbox (ditulis dalam
// transaksi perubahan data), fan-out ke inbox in-app sesuai preferensi per role,
// dan pengiriman Web Push (VAPID) dengan retry.
package notifications

import (
	"context"
	"encoding/json"
	"time"

	"sikat/internal/database"
)

// Jenis event notifikasi.
const (
	TicketCreated  = "TICKET_CREATED"
	TicketCritical = "TICKET_CRITICAL"
	TicketClaimed  = "TICKET_CLAIMED"
	TicketStarted  = "TICKET_STARTED"
	TicketPending  = "TICKET_PENDING"
	TicketResolved = "TICKET_RESOLVED"
	TicketReopened = "TICKET_REOPENED"
	TicketAssigned = "TICKET_ASSIGNED"
	// TicketTransferred: jenis gangguan dikoreksi sehingga tiket pindah bidang.
	TicketTransferred = "TICKET_TRANSFERRED"
	SLAWarning        = "SLA_WARNING"
	SLABreach         = "SLA_BREACH"
	RepeatDamage      = "REPEAT_DAMAGE"
	// Test: notifikasi uji dari halaman pengaturan (hanya ke pengirimnya, selalu push).
	Test = "TEST"
)

// EventInfo menjelaskan satu jenis event untuk halaman preferensi.
type EventInfo struct {
	Type, Label, Help string
}

// Events adalah daftar event yang dapat diatur per role (urutan tampilan).
var Events = []EventInfo{
	{TicketCreated, "Tiket baru", "Petugas: hanya tiket sesuai kompetensinya (atau semua bila tanpa kompetensi). Tiket kritis memakai event \"Tiket kritis\"."},
	{TicketCritical, "Tiket kritis", "Tiket berprioritas Kritis dibuat atau prioritasnya dinaikkan ke Kritis."},
	{TicketClaimed, "Tiket diambil", "Petugas: dikirim ke petugas yang sesuai kompetensinya, agar tahu tiket sudah ditangani."},
	{TicketStarted, "Tiket mulai dikerjakan", "Petugas: hanya anggota tim tiket."},
	{TicketPending, "Tiket tertunda", "Petugas: hanya anggota tim tiket."},
	{TicketResolved, "Tiket selesai", "Petugas: hanya anggota tim tiket."},
	{TicketReopened, "Tiket dibuka kembali", "Pelapor menyatakan belum beres atau koordinator membuka ulang. Petugas: anggota tim tiket."},
	{TicketAssigned, "Penugasan", "Ditambahkan ke tim tiket atau tiketnya diambil alih. Hanya untuk orang yang bersangkutan."},
	{TicketTransferred, "Dialihkan ke bidang", "Jenis gangguan dikoreksi (mis. dari Triase) sehingga tiket pindah bidang. Petugas: tiket belum diambil → sesuai kompetensi bidang baru; sudah diambil → anggota tim."},
	{SLAWarning, "SLA hampir habis", "Petugas: target respons → petugas sesuai kompetensi; target penyelesaian → anggota tim tiket."},
	{SLABreach, "SLA terlewati", "Petugas: target respons → petugas sesuai kompetensi; target penyelesaian → anggota tim tiket."},
	{RepeatDamage, "Perangkat sering rusak", "Perangkat mencapai ambang kerusakan berulang."},
}

// ValidEvent memeriksa jenis event yang dapat diatur.
func ValidEvent(t string) bool {
	for _, e := range Events {
		if e.Type == t {
			return true
		}
	}
	return false
}

// Kanal preferensi.
const (
	ChannelNone  = "NONE"
	ChannelInbox = "INBOX"
	ChannelPush  = "PUSH"
)

// Event adalah satu kejadian yang perlu dinotifikasi.
type Event struct {
	Type string
	// DedupeKey unik per kejadian (mis. "h:123" untuk histori tiket 123).
	DedupeKey string
	TicketID  int64
	AssetID   int64
	ActorID   int64 // pelaku (tidak ikut menerima notifikasi)
	TargetID  int64 // penerima khusus (penugasan, uji)
	Payload   map[string]any
}

// Publish menulis event ke outbox dalam transaksi pemanggil. Event dengan
// dedupe_key yang sudah ada diabaikan; mengembalikan ID event (baru maupun lama).
func Publish(ctx context.Context, tx database.DBTX, e Event, now time.Time) (int64, error) {
	var payload any
	if len(e.Payload) > 0 {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return 0, err
		}
		payload = string(b)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (event_type, dedupe_key, ticket_id, asset_id, actor_user_id, target_user_id, payload, available_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		e.Type, e.DedupeKey, database.NullInt64(e.TicketID), database.NullInt64(e.AssetID),
		database.NullInt64(e.ActorID), database.NullInt64(e.TargetID), payload, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
