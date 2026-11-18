package tickets

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"sikat/internal/attachments"
	"sikat/internal/clock"
	"sikat/internal/database"
	"sikat/internal/notifications"
	"sikat/internal/priority"
	"sikat/internal/sla"
)

// Photo adalah foto yang sudah diproses beserta nama aslinya.
type Photo struct {
	attachments.Processed
	OriginalName string
}

// Service berisi aturan bisnis tiket.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	store attachments.Store
	repo  repository
	hooks Hooks
	sla   *sla.Service
}

// NewService membuat Service.
func NewService(db *sql.DB, c clock.Clock, store attachments.Store) *Service {
	return &Service{db: db, clock: c, store: store, sla: sla.NewService(db, c)}
}

// NewToken membuat token akses pelapor (32 byte acak, base64url).
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidTokenFormat memeriksa bentuk token pelapor sebelum query.
func ValidTokenFormat(t string) bool {
	if len(t) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(t)
	return err == nil
}

// resolveLocation memvalidasi lokasi & perangkat, melengkapi unit/ruangan dari
// perangkat, dan menentukan apakah lokasi termasuk layanan klinis kritis.
func (s *Service) resolveLocation(ctx context.Context, tx *sql.Tx, d *Draft, errs map[string]string) (bool, error) {
	if d.AssetID != 0 {
		roomID, deptID, ok, err := s.repo.asset(ctx, tx, d.AssetID)
		if err != nil {
			return false, err
		}
		switch {
		case !ok:
			errs["asset_id"] = "Perangkat tidak ditemukan atau sudah dihapus dari inventaris."
		case d.RoomFromQR && roomID.Int64 != d.RoomID:
			errs["asset_id"] = "Perangkat yang dipilih tidak berada di ruangan ini."
		case d.RoomID != 0 && roomID.Valid && roomID.Int64 != d.RoomID:
			errs["asset_id"] = "Perangkat yang dipilih tidak berada di ruangan tersebut."
		default:
			if d.RoomID == 0 && roomID.Valid {
				d.RoomID = roomID.Int64
			}
			if d.DepartmentID == 0 && deptID.Valid {
				d.DepartmentID = deptID.Int64
			}
		}
	}
	critical := false
	if d.RoomID != 0 {
		dept, crit, ok, err := s.repo.room(ctx, tx, d.RoomID)
		if err != nil {
			return false, err
		}
		switch {
		case !ok:
			errs["room_id"] = "Ruangan tidak ditemukan atau nonaktif."
		case d.DepartmentID != 0 && d.DepartmentID != dept:
			errs["room_id"] = "Ruangan ini bukan milik unit yang dipilih."
		default:
			d.DepartmentID = dept
			critical = crit
		}
		return critical, nil
	}
	if d.DepartmentID != 0 {
		crit, ok, err := s.repo.departmentActive(ctx, tx, d.DepartmentID)
		if err != nil {
			return false, err
		}
		if !ok {
			errs["department_id"] = "Unit tidak ditemukan atau nonaktif."
		}
		critical = crit
	}
	return critical, nil
}

// Create membuat tiket: memvalidasi referensi, menentukan prioritas otomatis,
// memberi nomor tiket atomik, menyimpan foto, dan mencatat histori — semuanya
// dalam satu transaksi. Bila errs tidak kosong, tidak ada yang disimpan.
func (s *Service) Create(ctx context.Context, d Draft, photos []Photo) (Created, map[string]string, error) {
	errs := map[string]string{}
	now := s.clock.Now()

	// Foto ditulis ke disk sebelum transaksi; dihapus lagi bila transaksi gagal.
	var keys []string
	cleanup := func() {
		for _, k := range keys {
			s.store.Remove(k)
		}
	}
	for _, p := range photos {
		k, err := s.store.Save(p.Data, now)
		if err != nil {
			cleanup()
			return Created{}, nil, err
		}
		keys = append(keys, k)
	}

	var out Created
	base := d // salinan awal; transaksi yang diulang mulai dari draft yang sama
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		d := base
		errs = map[string]string{}
		cat, ok, err := s.repo.category(ctx, tx, d.TicketCategoryID)
		if err != nil {
			return err
		}
		if !ok {
			errs["ticket_category_id"] = "Jenis masalah tidak tersedia. Pilih yang lain."
		}
		if d.ReporterDepartmentID != 0 {
			if _, ok, err := s.repo.departmentActive(ctx, tx, d.ReporterDepartmentID); err != nil {
				return err
			} else if !ok {
				errs["reporter_department_id"] = "Unit pelapor tidak ditemukan atau nonaktif."
			}
		}
		critical, err := s.resolveLocation(ctx, tx, &d, errs)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			return nil
		}
		if d.ReporterDepartmentID == 0 {
			d.ReporterDepartmentID = d.DepartmentID // unit pelapor otomatis dari lokasi
		}

		rules, err := priority.LoadActive(ctx, tx)
		if err != nil {
			return err
		}
		res := priority.Evaluate(rules, priority.Input{
			TicketCategoryID: d.TicketCategoryID, ITCategoryID: cat.itCategoryID, DepartmentID: d.DepartmentID,
			Impact: d.Impact, Scope: d.Scope, Replacement: d.Replacement, CriticalService: critical,
			DefaultPriority: cat.defaultPriority,
		})
		var ruleID int64
		meta := map[string]any{"priority": res.Priority, "critical_service": critical}
		if res.Rule != nil {
			ruleID = res.Rule.ID
			meta["priority_rule_id"] = ruleID
			meta["priority_rule"] = res.Rule.Name
		} else {
			meta["priority_rule"] = "bawaan jenis gangguan"
		}

		year := now.In(clock.Location).Year()
		seq, err := database.NextSequence(ctx, tx, "TKT", year)
		if err != nil {
			return err
		}
		token, err := NewToken()
		if err != nil {
			return err
		}
		number := formatNumber(year, seq)
		id, err := s.repo.insert(ctx, tx, insertTicket{
			Draft: d, Number: number, TokenHash: hashToken(token), ITCategoryID: cat.itCategoryID,
			Critical: critical, Priority: res.Priority, PriorityRuleID: ruleID,
		}, now)
		if err != nil {
			return err
		}
		metaJSON, _ := json.Marshal(meta)
		actorType := "REPORTER"
		if d.CreatedBy != 0 {
			actorType = "USER"
		}
		if err := s.repo.addHistory(ctx, tx, historyEntry{
			TicketID: id, Event: EventCreated, ToStatus: StatusNew, ActorType: actorType, ActorID: d.CreatedBy, Meta: string(metaJSON),
		}, now); err != nil {
			return err
		}
		if err := s.assignSLA(ctx, tx, id, sla.Match{Priority: res.Priority, TicketCategoryID: d.TicketCategoryID,
			ITCategoryID: cat.itCategoryID, DepartmentID: d.DepartmentID}, now); err != nil {
			return err
		}
		// Tiket kritis dinotifikasi lewat eskalasi SLA (event "Tiket kritis").
		if res.Priority != "CRITICAL" {
			if _, err := notifications.Publish(ctx, tx, notifications.Event{Type: notifications.TicketCreated,
				DedupeKey: fmt.Sprintf("ticket:%d:created", id), TicketID: id, ActorID: d.CreatedBy}, now); err != nil {
				return err
			}
		}
		if d.AssetID != 0 && s.hooks != nil {
			if err := s.hooks.TicketCreated(ctx, tx, CreatedEvent{
				TicketID: id, AssetID: d.AssetID, TicketCategoryID: d.TicketCategoryID, CreatedAt: now,
			}); err != nil {
				return err
			}
		}
		for i, p := range photos {
			if err := s.repo.addAttachment(ctx, tx, id, attachmentRow{
				Key: keys[i], Mime: p.Mime, Size: len(p.Data), Width: p.Width, Height: p.Height,
				SHA: p.SHA256, ByUser: d.CreatedBy, OriginalName: p.OriginalName,
			}, now); err != nil {
				return err
			}
		}
		out = Created{ID: id, Number: number, Token: token, Priority: res.Priority}
		return nil
	})
	if err != nil || len(errs) > 0 {
		cleanup()
		if err != nil {
			return Created{}, nil, err
		}
		return Created{}, errs, nil
	}
	return out, nil, nil
}

// Claim mengambil tiket dari antrean. Hanya satu petugas yang dapat berhasil:
// UPDATE bersyarat (status NEW & belum ada penanggung jawab) dieksekusi atomik;
// UNIQUE index di ticket_assignments menjadi pertahanan kedua.
func (s *Service) Claim(ctx context.Context, ticketID, userID int64) (ClaimResult, error) {
	now := s.clock.Now()
	var res ClaimResult
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		// Data SLA dibaca tanpa kunci; klaim tetap ditentukan UPDATE bersyarat di bawah.
		var responded bool
		var due sql.NullTime
		var cycle, rev int
		var prio string
		if err := tx.QueryRowContext(ctx, `SELECT first_response_at IS NOT NULL, response_due_at, sla_cycle, sla_revision, priority
			FROM tickets WHERE id = ?`, ticketID).Scan(&responded, &due, &cycle, &rev, &prio); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		r, err := tx.ExecContext(ctx, `
			UPDATE tickets SET assignee_id = ?, status = 'CLAIMED', first_response_at = COALESCE(first_response_at, ?),
			                   version = version + 1, updated_at = ?
			 WHERE id = ? AND status = 'NEW' AND assignee_id IS NULL`, userID, now, now, ticketID)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var status string
			var by sql.NullString
			err := tx.QueryRowContext(ctx, `
				SELECT t.status, u.full_name FROM tickets t LEFT JOIN users u ON u.id = t.assignee_id WHERE t.id = ?`, ticketID).Scan(&status, &by)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			res = ClaimResult{OK: false, Status: status, ClaimedBy: by.String}
			_ = tx.QueryRowContext(ctx, `
				SELECT MAX(created_at) FROM ticket_histories WHERE ticket_id = ? AND event_type = 'CLAIMED'`, ticketID).Scan(&res.ClaimedAt)
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ticket_assignments (ticket_id, user_id, role, assigned_by, assigned_at) VALUES (?, ?, 'PRIMARY', ?, ?)`,
			ticketID, userID, userID, now); err != nil {
			return err
		}
		if err := s.repo.addHistory(ctx, tx, historyEntry{
			TicketID: ticketID, Event: EventClaimed, FromStatus: StatusNew, ToStatus: StatusClaimed, ActorType: "USER", ActorID: userID,
		}, now); err != nil {
			return err
		}
		if !responded {
			if err := recordLateBreach(ctx, tx, ticketID, sla.KindResponseBreach, due, cycle, rev, prio, now); err != nil {
				return err
			}
		}
		res = ClaimResult{OK: true, Status: StatusClaimed, ClaimedAt: sql.NullTime{Time: now, Valid: true}}
		return nil
	})
	return res, err
}

// Get mengambil tiket beserta histori dan lampiran (tampilan petugas).
func (s *Service) Get(ctx context.Context, id int64) (Ticket, []History, []Attachment, error) {
	t, err := s.repo.get(ctx, s.db, id)
	if err != nil {
		return Ticket{}, nil, nil, err
	}
	one := []Ticket{t}
	if err := s.attachSLA(ctx, one); err != nil {
		return Ticket{}, nil, nil, err
	}
	t = one[0]
	h, err := s.repo.histories(ctx, s.db, id)
	if err != nil {
		return Ticket{}, nil, nil, err
	}
	a, err := s.repo.attachments(ctx, s.db, id, false)
	return t, h, a, err
}

// GetPublic mengambil tiket berdasarkan token pelapor (hanya lampiran yang boleh dilihat pelapor).
func (s *Service) GetPublic(ctx context.Context, token string) (Ticket, []History, []Attachment, error) {
	if !ValidTokenFormat(token) {
		return Ticket{}, nil, nil, ErrNotFound
	}
	t, err := s.repo.getByToken(ctx, s.db, token)
	if err != nil {
		return Ticket{}, nil, nil, err
	}
	h, err := s.repo.histories(ctx, s.db, t.ID)
	if err != nil {
		return Ticket{}, nil, nil, err
	}
	a, err := s.repo.attachments(ctx, s.db, t.ID, true)
	return t, h, a, err
}

// AttachmentKey mengembalikan kunci file lampiran untuk petugas.
func (s *Service) AttachmentKey(ctx context.Context, attachmentID int64) (string, error) {
	return s.repo.attachmentKey(ctx, s.db, `SELECT storage_key FROM attachments WHERE id = ? AND ticket_id IS NOT NULL AND file_deleted_at IS NULL`, attachmentID)
}

// PublicAttachmentKey mengembalikan kunci file hanya bila lampiran milik tiket token tsb dan boleh dilihat pelapor.
func (s *Service) PublicAttachmentKey(ctx context.Context, token string, attachmentID int64) (string, error) {
	if !ValidTokenFormat(token) {
		return "", ErrNotFound
	}
	return s.repo.attachmentKey(ctx, s.db, `
		SELECT a.storage_key FROM attachments a JOIN tickets t ON t.id = a.ticket_id
		 WHERE a.id = ? AND t.public_token_hash = ? AND a.visible_to_reporter = TRUE AND a.file_deleted_at IS NULL`, attachmentID, hashToken(token))
}

// Store memberi akses ke penyimpanan file (untuk handler yang menyajikan foto).
func (s *Service) Store() attachments.Store { return s.store }

// Queue mengembalikan antrean/daftar tiket petugas.
func (s *Service) Queue(ctx context.Context, f QueueFilter) ([]Ticket, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	list, total, err := s.repo.queue(ctx, s.db, f)
	if err != nil {
		return nil, 0, err
	}
	return list, total, s.attachSLA(ctx, list)
}

// SLA memberi akses ke layanan SLA (dashboard, worker).
func (s *Service) SLA() *sla.Service { return s.sla }

// Workload mengembalikan beban kerja aktif per petugas.
func (s *Service) Workload(ctx context.Context) ([]Workload, error) {
	return s.repo.workload(ctx, s.db)
}

// HasCompetencies memeriksa apakah user punya data kompetensi.
func (s *Service) HasCompetencies(ctx context.Context, userID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_competencies WHERE user_id = ?`, userID).Scan(&n)
	return n > 0, err
}

// Categories mengembalikan jenis gangguan aktif. Bila preferIT != 0, jenis
// gangguan dari bidang yang sama dengan kategori perangkat ditampilkan lebih dulu.
func (s *Service) Categories(ctx context.Context, preferIT int64) ([]Category, error) {
	cats, err := s.repo.categories(ctx, s.db)
	if err != nil || preferIT == 0 {
		return cats, err
	}
	sort.SliceStable(cats, func(i, j int) bool {
		return cats[i].ITCategoryID == preferIT && cats[j].ITCategoryID != preferIT
	})
	return cats, nil
}

// Departments mengembalikan unit aktif.
func (s *Service) Departments(ctx context.Context) ([]Option, error) {
	return s.repo.options(ctx, s.db, `SELECT id, name FROM departments WHERE is_active = TRUE ORDER BY name`)
}

// RoomAssets mengembalikan perangkat aktif di ruangan.
func (s *Service) RoomAssets(ctx context.Context, roomID int64) ([]Option, error) {
	return s.repo.options(ctx, s.db, `
		SELECT id, CONCAT(name, ' (', asset_code, ')') FROM assets
		 WHERE room_id = ? AND status <> 'DISPOSED' ORDER BY name`, roomID)
}

// LocationAssets mengembalikan perangkat aktif di ruangan, atau di unit bila ruangan kosong.
func (s *Service) LocationAssets(ctx context.Context, departmentID, roomID int64) ([]Option, error) {
	if roomID != 0 {
		return s.RoomAssets(ctx, roomID)
	}
	if departmentID == 0 {
		return nil, nil
	}
	return s.repo.options(ctx, s.db, `
		SELECT id, CONCAT(name, ' (', asset_code, ')') FROM assets
		 WHERE department_id = ? AND status <> 'DISPOSED' ORDER BY name LIMIT 300`, departmentID)
}

// Summary menghitung ringkasan tiket untuk beranda.
func (s *Service) Summary(ctx context.Context, userID int64) (Summary, error) {
	sum := Summary{ByStatus: map[string]int{}}
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM tickets GROUP BY status`)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return sum, err
		}
		sum.ByStatus[st] = n
	}
	rows.Close()
	startOfDay := func() time.Time {
		y, m, d := s.clock.Now().In(clock.Location).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, clock.Location).UTC()
	}()
	err = s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM tickets WHERE status = 'NEW' AND priority = 'CRITICAL'),
		       (SELECT COUNT(*) FROM tickets WHERE assignee_id = ? AND status IN ('CLAIMED','IN_PROGRESS','PENDING')),
		       (SELECT COUNT(*) FROM tickets WHERE status = 'NEW'
		          AND (NOT EXISTS (SELECT 1 FROM user_competencies WHERE user_id = ?)
		               OR it_category_id IN (SELECT it_category_id FROM user_competencies WHERE user_id = ?)
		               OR it_category_id IN (SELECT id FROM it_categories WHERE is_triage))),
		       (SELECT COUNT(*) FROM tickets WHERE created_at >= ?)`, userID, userID, userID, startOfDay).
		Scan(&sum.CriticalNew, &sum.MineActive, &sum.AvailableForMe, &sum.CreatedToday)
	return sum, err
}

// RelatedToAsset mengembalikan tiket terbaru untuk perangkat.
func (s *Service) RelatedToAsset(ctx context.Context, assetID int64, limit int) ([]Ticket, error) {
	rows, err := s.db.QueryContext(ctx, selectTicket+" WHERE t.asset_id = ? ORDER BY t.created_at DESC LIMIT ?", assetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// EmployeeOption adalah saran nama pelapor dari direktori pegawai.
type EmployeeOption struct {
	Name, Department string
}

// EmployeeOptions mengembalikan pegawai aktif (maks. 3000) untuk saran nama pelapor.
func (s *Service) EmployeeOptions(ctx context.Context) ([]EmployeeOption, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.full_name, COALESCE(d.name, '') FROM employees e LEFT JOIN departments d ON d.id = e.department_id
		 WHERE e.is_active = TRUE ORDER BY e.full_name LIMIT 3000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmployeeOption
	for rows.Next() {
		var o EmployeeOption
		if err := rows.Scan(&o.Name, &o.Department); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
