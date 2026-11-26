// Package simrs membaca data dari API SIMRS rumah sakit (dashboard EMR), sama dengan
// yang dipakai aplikasi CSSD.
package simrs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Lama daftar dianggap segar, dan jeda sebelum mencoba lagi setelah gagal.
const (
	CacheTTL   = time.Hour
	RetryAfter = time.Minute
	maxBody    = 32 << 20 // daftar pegawai mentah memuat banyak kolom
	maxNames   = 10000
)

// PegawaiConfig: endpoint dropdown pegawai SIMRS dan kredensialnya.
type PegawaiConfig struct {
	URL     string
	Token   string
	Cookie  string
	Timeout time.Duration
}

// Pegawai menyediakan daftar nama pegawai dari SIMRS. Daftar disimpan di memori
// selama CacheTTL; bila SIMRS gangguan, daftar terakhir yang berhasil tetap dipakai.
// Hanya nama yang disimpan: data mentah memuat NIK, NPWP, rekening, dan alamat.
type Pegawai struct {
	cfg    PegawaiConfig
	client *http.Client
	log    *slog.Logger
	now    func() time.Time

	mu         sync.Mutex
	names      []string
	fetchedAt  time.Time // terakhir berhasil
	failedAt   time.Time // terakhir gagal
	refreshing bool
}

// NewPegawai mengembalikan nil bila URL atau token belum diisi (fitur nonaktif).
func NewPegawai(cfg PegawaiConfig, log *slog.Logger) *Pegawai {
	if cfg.URL == "" || cfg.Token == "" {
		return nil
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Pegawai{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}, log: log, now: time.Now}
}

// Names mengembalikan nama pegawai (urut abjad, tanpa duplikat). Daftar lama
// dikembalikan segera dan diperbarui di latar belakang; hanya pemanggilan pertama
// (belum ada daftar) yang menunggu SIMRS. Gagal = daftar kosong, form tetap bisa diisi bebas.
func (p *Pegawai) Names(ctx context.Context) []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	names, fresh := p.names, p.now().Sub(p.fetchedAt) < CacheTTL
	cooling := p.now().Sub(p.failedAt) < RetryAfter
	if fresh || cooling || p.refreshing {
		p.mu.Unlock()
		return names
	}
	p.refreshing = true
	p.mu.Unlock()

	if names != nil {
		go p.refresh(context.WithoutCancel(ctx))
		return names
	}
	return p.refresh(ctx)
}

// Warm memuat daftar di latar belakang saat aplikasi mulai, agar form pertama tidak menunggu.
func (p *Pegawai) Warm(ctx context.Context) {
	if p != nil {
		go p.Names(ctx)
	}
}

func (p *Pegawai) refresh(ctx context.Context) []string {
	names, err := p.fetch(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refreshing = false
	if err != nil {
		p.failedAt = p.now()
		p.log.Warn("daftar pegawai SIMRS tidak bisa dimuat", "err", err, "cadangan", len(p.names))
		return p.names
	}
	p.names, p.fetchedAt = names, p.now()
	return names
}

func (p *Pegawai) fetch(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("token", headerValue("token", p.cfg.Token))
	if p.cfg.Cookie != "" {
		req.Header.Set("Cookie", headerValue("Cookie", p.cfg.Cookie))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke SIMRS: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("membaca respons SIMRS: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SIMRS menjawab HTTP %d", resp.StatusCode)
	}
	return ParseNames(body)
}

// ParseNames membaca respons dropdown pegawai: daftar objek langsung, atau di dalam
// "response"/"data". Nama diambil dari namalengkap (atau label/nama_lengkap/nama).
func ParseNames(body []byte) ([]string, error) {
	var raw json.RawMessage = body
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) == nil {
		switch {
		case wrap["response"] != nil:
			raw = wrap["response"]
		case wrap["data"] != nil:
			raw = wrap["data"]
		}
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, errors.New("respons SIMRS bukan daftar pegawai")
	}
	seen := map[string]bool{}
	var names []string
	for _, r := range rows {
		for _, k := range []string{"namalengkap", "label", "nama_lengkap", "nama"} {
			s, _ := r[k].(string)
			if s = strings.Join(strings.Fields(s), " "); s != "" {
				if !seen[strings.ToLower(s)] && len(names) < maxNames {
					seen[strings.ToLower(s)] = true
					names = append(names, s)
				}
				break
			}
		}
	}
	if len(names) == 0 {
		return nil, errors.New("daftar pegawai dari SIMRS kosong")
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names, nil
}

// headerValue membuang awalan "token:" / "Cookie:" bila nilai di .env disalin beserta nama header-nya.
func headerValue(name, v string) string {
	v = strings.TrimSpace(v)
	if len(v) > len(name) && strings.EqualFold(v[:len(name)+1], name+":") {
		return strings.TrimSpace(v[len(name)+1:])
	}
	return v
}
