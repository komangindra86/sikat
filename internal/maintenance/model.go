package maintenance

import (
	"database/sql"
	"time"
)

// Entry adalah satu entri riwayat pemeliharaan beserta labelnya.
type Entry struct {
	ID              int64
	AssetID         int64
	Type            string
	Source          string
	TicketID        sql.NullInt64
	TicketNo        sql.NullString
	TicketCycle     sql.NullInt64
	ActivityID      sql.NullInt64
	ActivityNo      sql.NullString
	CategoryName    sql.NullString
	PerformerName   sql.NullString
	Complaint       sql.NullString
	Action          string
	Result          string
	StartedAt       sql.NullTime
	FinishedAt      time.Time
	RepairMinutes   sql.NullInt64
	DowntimeMinutes sql.NullInt64
	ServiceCost     sql.NullString
	StatusAfter     sql.NullString
	Notes           sql.NullString
	VoidedAt        sql.NullTime
	VoidReason      sql.NullString
	VoiderName      sql.NullString
	Parts           []PartUsed
}

// TotalCost adalah biaya jasa/vendor entri (string desimal, "" bila tidak ada).
// Sparepart tidak berharga; pemakaiannya mengurangi stok.
func (e Entry) TotalCost() string { return e.ServiceCost.String }

// PartUsed adalah sparepart yang dipakai pada satu entri.
type PartUsed struct {
	Name string
	Unit string
	Qty  string
}

// Stats adalah statistik perangkat.
type Stats struct {
	WindowDays      int
	DamagesAll      int
	DamagesWindow   int
	RepairsAll      int
	RepairsWindow   int
	DowntimeAll     int // menit
	DowntimeWindow  int
	CostAll         string
	CostWindow      string
	LastMaintenance sql.NullTime
}

// Alert adalah peringatan kerusakan berulang.
type Alert struct {
	ID             int64
	AssetID        int64
	AssetCode      string
	AssetName      string
	DepartmentName sql.NullString
	RoomName       sql.NullString
	Status         string
	TicketCount    int
	WindowDays     int
	Threshold      int
	Recommendation string
	Reasons        sql.NullString
	Decision       sql.NullString
	DecisionNote   sql.NullString
	DeciderName    sql.NullString
	DecidedAt      sql.NullTime
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Option adalah pilihan sparepart: label, stok (terformat), dan apakah stok habis.
type Option struct {
	ID    int64
	Label string
	Stock string
	Empty bool
}

// AssetView adalah seluruh data pemeliharaan untuk halaman detail perangkat.
type AssetView struct {
	Stats     Stats
	History   []Entry
	Alert     *Alert
	Parts     []Option
	CanRecord bool
	CanReview bool
	Form      map[string]string
	Errors    map[string]string
}

// Err mengembalikan pesan error field form entri manual.
func (v AssetView) Err(k string) string { return v.Errors[k] }

// HumanMinutes menampilkan menit sebagai "2 j 10 mnt" / "3 hari 4 j".
func HumanMinutes(m int) string {
	switch {
	case m <= 0:
		return "0 mnt"
	case m < 60:
		return itoa(m) + " mnt"
	case m < 24*60:
		if m%60 == 0 {
			return itoa(m/60) + " j"
		}
		return itoa(m/60) + " j " + itoa(m%60) + " mnt"
	default:
		d, h := m/(24*60), (m%(24*60))/60
		if h == 0 {
			return itoa(d) + " hari"
		}
		return itoa(d) + " hari " + itoa(h) + " j"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
