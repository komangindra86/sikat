package notifications

import (
	"fmt"
	"strings"
	"time"

	"sikat/internal/clock"
)

var priorityLabel = map[string]string{"CRITICAL": "Kritis", "HIGH": "Tinggi", "MEDIUM": "Sedang", "LOW": "Rendah"}

// content adalah teks satu notifikasi.
type content struct {
	Title, Body string
	// PushBody: teks lock screen. Hanya nomor tiket, prioritas, dan jenis
	// kejadian — tanpa lokasi, kategori/deskripsi, nama perangkat, atau nama orang.
	PushBody string
	URL      string
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func num(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

func join(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func dueText(m map[string]any) string {
	s := str(m, "due_at")
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return ""
	}
	return t.In(clock.Location).Format("02/01 15:04") + " WITA"
}

// compose menyusun teks inbox (lengkap, hanya terlihat setelah login) dan teks
// push (generik) untuk satu event.
func compose(ev outboxRow, t *ticketInfo, actor, asset string) content {
	p := ev.Payload
	c := content{URL: "/app/notifications"}
	if t == nil {
		t = &ticketInfo{}
	}
	if ev.TicketID.Valid {
		c.URL = fmt.Sprintf("/app/tickets/%d", ev.TicketID.Int64)
	}
	prio := priorityLabel[t.Priority]
	where := join(t.Category, t.location())
	by := actor
	if by == "" {
		by = "pelapor"
	}
	target := "penyelesaian"
	if strings.HasPrefix(str(p, "kind"), "RESPONSE") {
		target = "respons"
	}

	switch ev.Type {
	case TicketCreated:
		c.Title = "Tiket baru " + t.No
		c.Body = join("Prioritas "+prio, where)
		c.PushBody = "Tiket baru prioritas " + prio + " · " + t.No
		if t.IsTriage {
			c.Title += " — perlu dipilah"
			c.Body = join("Di luar daftar jenis gangguan; pilih jenis yang tepat", "Prioritas "+prio, t.location())
			c.PushBody = "Tiket baru perlu dipilah · " + t.No
		}
	case TicketTransferred:
		c.Title = "Tiket " + t.No + " dialihkan ke bidang " + t.FieldName
		c.Body = join("Jenis: "+t.Category, "Oleh "+by, str(p, "note"), t.location())
		c.PushBody = "Tiket " + t.No + " dialihkan ke bidang Anda"
	case TicketCritical:
		c.Title = "Tiket KRITIS " + t.No
		c.Body = join(where, "Segera ditangani.")
		c.PushBody = "Tiket kritis · " + t.No + " · segera ditangani"
	case TicketClaimed:
		c.Title = "Tiket " + t.No + " diambil"
		c.Body = join("Diambil oleh "+by, where)
		c.PushBody = "Tiket " + t.No + " sudah diambil petugas"
	case TicketStarted:
		c.Title = "Tiket " + t.No + " mulai dikerjakan"
		c.Body = join("Oleh "+by, where)
		c.PushBody = "Tiket " + t.No + " mulai dikerjakan"
	case TicketPending:
		c.Title = "Tiket " + t.No + " tertunda"
		c.Body = join("Oleh "+by, str(p, "reason"), str(p, "note"))
		c.PushBody = "Tiket " + t.No + " ditunda"
	case TicketResolved:
		c.Title = "Tiket " + t.No + " selesai"
		c.Body = join("Diselesaikan oleh "+by, where, "Menunggu konfirmasi pelapor.")
		c.PushBody = "Tiket " + t.No + " telah diselesaikan"
	case TicketReopened:
		c.Title = "Tiket " + t.No + " dibuka kembali"
		c.Body = join("Oleh "+by, str(p, "note"))
		c.PushBody = "Tiket " + t.No + " dibuka kembali"
	case TicketAssigned:
		if str(p, "reason") == "TAKEN_OVER" {
			c.Title = "Tiket " + t.No + " diambil alih"
			c.Body = join("Penanggung jawab kini "+by, str(p, "note"))
			c.PushBody = "Tiket " + t.No + " dialihkan ke petugas lain"
		} else {
			c.Title = "Anda ditambahkan ke tim tiket " + t.No
			c.Body = join("Oleh "+by, where)
			c.PushBody = "Anda ditambahkan ke tim tiket " + t.No
		}
	case SLAWarning:
		c.Title = "SLA hampir habis · " + t.No
		c.Body = join(fmt.Sprintf("Target %s %d%% terpakai", target, num(p, "used_pct")), dueText(p), where)
		c.PushBody = "SLA " + target + " hampir habis · " + t.No
	case SLABreach:
		c.Title = "SLA terlewati · " + t.No
		due := dueText(p)
		if due != "" {
			due = "batas " + due
		}
		c.Body = join("Target "+target+" terlewati", due, where)
		c.PushBody = "SLA " + target + " terlewati · " + t.No
	case RepeatDamage:
		c.Title = "Perangkat sering rusak"
		c.Body = fmt.Sprintf("Perhatian: %s telah mengalami %d gangguan dalam %d hari terakhir.",
			asset, num(p, "count"), num(p, "window_days"))
		c.PushBody = "Perangkat sering rusak perlu ditinjau"
		if ev.AssetID.Valid {
			c.URL = fmt.Sprintf("/app/assets/%d", ev.AssetID.Int64)
		}
	case Test:
		c.Title = "Notifikasi uji"
		c.Body = "Notifikasi push di perangkat ini berfungsi."
		c.PushBody = "Notifikasi uji: push berfungsi"
		c.URL = "/app/notifications/settings"
	default:
		c.Title = "Notifikasi"
		c.Body = ev.Type
		c.PushBody = "Ada notifikasi baru"
	}
	return c
}
