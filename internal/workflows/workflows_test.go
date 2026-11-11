package workflows

import "testing"

// Matriks lengkap: setiap aksi × setiap status. Sel yang tidak tercantum harus ditolak.
func TestTransitionMatrix(t *testing.T) {
	valid := map[Action]map[string]string{
		Claim:   {New: Claimed},
		Start:   {Claimed: InProgress},
		Pend:    {Claimed: Pending, InProgress: Pending},
		Resume:  {Pending: InProgress},
		Resolve: {InProgress: Resolved},
		Release: {Claimed: New},
		Close:   {Resolved: Closed},
		Reopen:  {Resolved: InProgress},
		Cancel:  {New: Cancelled, Claimed: Cancelled, InProgress: Cancelled, Pending: Cancelled},
	}
	checked := 0
	for a, froms := range valid {
		for _, from := range Statuses() {
			to, ok := Next(a, from)
			want, shouldOK := froms[from]
			if ok != shouldOK || to != want {
				t.Errorf("Next(%s, %s) = %q,%v; ingin %q,%v", a, from, to, ok, want, shouldOK)
			}
			checked++
		}
	}
	if checked != len(valid)*7 {
		t.Fatalf("matriks tidak lengkap: %d", checked)
	}
	if _, ok := Next("tidak-ada", New); ok {
		t.Fatal("aksi tak dikenal harus ditolak")
	}
}

func TestTerminalStatesHaveNoActions(t *testing.T) {
	for _, s := range []string{Closed, Cancelled} {
		if acts := Allowed(s); len(acts) != 0 {
			t.Errorf("status akhir %s tidak boleh punya aksi: %v", s, acts)
		}
		if !IsTerminal(s) || IsActive(s) {
			t.Errorf("klasifikasi %s salah", s)
		}
	}
	if got := Allowed(InProgress); len(got) != 3 || got[0] != Resolve || got[1] != Pend || got[2] != Cancel {
		t.Fatalf("aksi IN_PROGRESS = %v", got)
	}
}
