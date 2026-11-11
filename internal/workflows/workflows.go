// Package workflows mendefinisikan state machine status tiket.
//
// Hanya transisi di tabel ini yang sah. Siapa yang boleh menjalankan setiap
// aksi (penanggung jawab, koordinator, pelapor) diperiksa di service tiket.
//
//	NEW ──claim──> CLAIMED ──start──> IN_PROGRESS ──resolve──> RESOLVED ──close──> CLOSED
//	 ^               │  │                │   ^                    │
//	 └───release─────┘  └──pend──> PENDING ─┘resume               └──reopen──> IN_PROGRESS
//	NEW/CLAIMED/IN_PROGRESS/PENDING ──cancel──> CANCELLED
package workflows

// Status tiket.
const (
	New        = "NEW"
	Claimed    = "CLAIMED"
	InProgress = "IN_PROGRESS"
	Pending    = "PENDING"
	Resolved   = "RESOLVED"
	Closed     = "CLOSED"
	Cancelled  = "CANCELLED"
)

// Action adalah aksi yang mengubah status.
type Action string

// Daftar aksi.
const (
	Claim   Action = "claim"
	Start   Action = "start"
	Pend    Action = "pend"
	Resume  Action = "resume"
	Resolve Action = "resolve"
	Release Action = "release"
	Close   Action = "close"
	Reopen  Action = "reopen"
	Cancel  Action = "cancel"
)

type transition struct {
	from []string
	to   string
}

var table = map[Action]transition{
	Claim:   {from: []string{New}, to: Claimed},
	Start:   {from: []string{Claimed}, to: InProgress},
	Pend:    {from: []string{Claimed, InProgress}, to: Pending},
	Resume:  {from: []string{Pending}, to: InProgress},
	Resolve: {from: []string{InProgress}, to: Resolved},
	Release: {from: []string{Claimed}, to: New},
	Close:   {from: []string{Resolved}, to: Closed},
	Reopen:  {from: []string{Resolved}, to: InProgress},
	Cancel:  {from: []string{New, Claimed, InProgress, Pending}, to: Cancelled},
}

// order menentukan urutan tombol aksi di layar.
var order = []Action{Claim, Start, Resolve, Pend, Resume, Release, Close, Reopen, Cancel}

// Next mengembalikan status tujuan bila aksi sah dari status from.
func Next(a Action, from string) (string, bool) {
	t, ok := table[a]
	if !ok {
		return "", false
	}
	for _, f := range t.from {
		if f == from {
			return t.to, true
		}
	}
	return "", false
}

// Allowed mengembalikan aksi yang sah dari status from, berurutan.
func Allowed(from string) []Action {
	var out []Action
	for _, a := range order {
		if _, ok := Next(a, from); ok {
			out = append(out, a)
		}
	}
	return out
}

// IsActive bernilai true untuk status yang masih dikerjakan/menunggu.
func IsActive(s string) bool {
	return s == New || s == Claimed || s == InProgress || s == Pending
}

// IsTerminal bernilai true untuk status akhir.
func IsTerminal(s string) bool { return s == Closed || s == Cancelled }

// Statuses mengembalikan seluruh status.
func Statuses() []string {
	return []string{New, Claimed, InProgress, Pending, Resolved, Closed, Cancelled}
}
