package tickets_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"sikat/internal/notifications"
	"sikat/internal/testutil"
	"sikat/internal/tickets"
)

// fakeSender mencatat push dan mengembalikan status sesuai antrean.
type fakeSender struct {
	mu       sync.Mutex
	statuses []int
	sent     []notifications.Message
}

func (f *fakeSender) Send(_ context.Context, _ notifications.Subscription, msg notifications.Message, _ time.Duration) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	if len(f.statuses) == 0 {
		return 201, nil
	}
	s := f.statuses[0]
	f.statuses = f.statuses[1:]
	if s == 0 {
		return 0, errors.New("jaringan terputus")
	}
	return s, nil
}

func notifSvc(w wf, s notifications.Sender) *notifications.Service {
	key := ""
	if s != nil {
		key = "kunci-uji"
	}
	return notifications.NewService(w.db, w.clk, nil, s, key)
}

func process(t *testing.T, n *notifications.Service) notifications.ProcessResult {
	t.Helper()
	r, err := n.Process(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func inbox(t *testing.T, w wf, userID int64, eventType string) int {
	return testutil.Count(t, w.db, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND event_type = ?`, userID, eventType)
}

func subscribe(t *testing.T, n *notifications.Service, userID int64, endpoint string) int64 {
	t.Helper()
	k, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	in := notifications.SubscribeInput{Endpoint: endpoint, UserAgent: "Mozilla/5.0 (Linux; Android 14) Chrome/130.0"}
	in.Keys.P256dh = base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
	in.Keys.Auth = base64.RawURLEncoding.EncodeToString(auth)
	id, err := n.Subscribe(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestNotificationFanOutFollowsRoleAndRelevance(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	n := notifSvc(w, nil)
	// tek1: kompetensi sesuai kategori tiket; tek2: kompetensi lain (tidak relevan).
	var itCat int64
	if err := w.db.QueryRow(`SELECT it_category_id FROM ticket_categories WHERE id = ?`, w.cat).Scan(&itCat); err != nil {
		t.Fatal(err)
	}
	now := w.clk.Now()
	if _, err := w.db.Exec(`INSERT INTO user_competencies (user_id, it_category_id, created_at) VALUES (?, ?, ?), (?, (SELECT MIN(id) FROM it_categories WHERE id <> ?), ?)`,
		w.tech.ID, itCat, now, w.other.ID, itCat, now); err != nil {
		t.Fatal(err)
	}

	c, errs, err := w.svc.Create(ctx, w.draft(nonCritical), nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events WHERE ticket_id = ? AND event_type = 'TICKET_CREATED'`, c.ID) != 1 {
		t.Fatal("tiket baru harus menulis satu event outbox dalam transaksi yang sama")
	}
	r := process(t, n)
	if r.Events != 1 {
		t.Fatalf("event diproses = %d", r.Events)
	}
	if inbox(t, w, w.tech.ID, notifications.TicketCreated) != 1 || inbox(t, w, w.coord.ID, notifications.TicketCreated) != 1 {
		t.Fatal("petugas berkompetensi & koordinator menerima tiket baru")
	}
	if inbox(t, w, w.other.ID, notifications.TicketCreated) != 0 {
		t.Fatal("petugas dengan kompetensi lain tidak menerima tiket baru")
	}
	if inbox(t, w, w.admin.ID, notifications.TicketCreated) != 0 {
		t.Fatal("admin tidak menerima notifikasi tiket secara default (Q6)")
	}
	var ch string
	_ = w.db.QueryRow(`SELECT channel FROM notifications WHERE user_id = ? AND event_type = 'TICKET_CREATED'`, w.tech.ID).Scan(&ch)
	if ch != "PUSH" {
		t.Fatalf("kanal petugas untuk tiket baru = %s, mau PUSH", ch)
	}

	// Diproses ulang: tidak ada notifikasi ganda.
	if r := process(t, n); r.Events != 0 || r.Notifications != 0 {
		t.Fatalf("pemrosesan ulang tidak boleh membuat notifikasi baru: %+v", r)
	}

	// Klaim → koordinator diberi tahu; pelaku tidak menerima notifikasi tindakannya sendiri.
	if res, err := w.svc.Claim(ctx, c.ID, w.tech.ID); err != nil || !res.OK {
		t.Fatalf("klaim: %v", err)
	}
	if err := w.svc.Start(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}
	process(t, n)
	if inbox(t, w, w.coord.ID, notifications.TicketClaimed) != 1 || inbox(t, w, w.tech.ID, notifications.TicketClaimed) != 0 {
		t.Fatal("klaim: koordinator menerima, pelaku tidak")
	}
	if inbox(t, w, w.coord.ID, notifications.TicketStarted) != 0 {
		t.Fatal("mulai dikerjakan: koordinator default tidak menerima (preferensi NONE)")
	}

	// Anggota pendukung → notifikasi penugasan hanya untuk orang tersebut.
	if err := w.svc.AddSupport(ctx, c.ID, w.tech, w.other.ID); err != nil {
		t.Fatal(err)
	}
	process(t, n)
	if inbox(t, w, w.other.ID, notifications.TicketAssigned) != 1 || inbox(t, w, w.coord.ID, notifications.TicketAssigned) != 0 {
		t.Fatal("penugasan hanya untuk anggota yang ditambahkan")
	}

	// Tunda oleh PJ → anggota tim lain (tek2) menerima, PJ tidak.
	if errs, err := w.svc.Pend(ctx, c.ID, w.tech, w.reasonID, "PSU dipesan"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	process(t, n)
	if inbox(t, w, w.other.ID, notifications.TicketPending) != 1 || inbox(t, w, w.tech.ID, notifications.TicketPending) != 0 {
		t.Fatal("tunda: anggota tim menerima, pelaku tidak")
	}
	var body string
	_ = w.db.QueryRow(`SELECT body FROM notifications WHERE user_id = ? AND event_type = 'TICKET_PENDING'`, w.other.ID).Scan(&body)
	if body == "" {
		t.Fatal("isi inbox tunda kosong")
	}

	// Preferensi role diubah ke NONE → tidak ada notifikasi.
	if _, err := w.db.Exec(`UPDATE notification_preferences p JOIN roles r ON r.id = p.role_id SET p.channel = 'NONE' WHERE r.code = 'COORDINATOR' AND p.event_type = 'TICKET_CREATED'`); err != nil {
		t.Fatal(err)
	}
	c2, _, _ := w.svc.Create(ctx, w.draft(nonCritical), nil)
	process(t, n)
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND ticket_id = ?`, w.coord.ID, c2.ID) != 0 {
		t.Fatal("preferensi NONE harus dihormati")
	}
}

func TestCriticalTicketAndSLANotificationsAreNotDuplicated(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	n := notifSvc(w, nil)
	slaPolicies(t, w.db, "24 jam", 60, 240, false)

	// Tiket kritis: satu event "Tiket kritis" (dari eskalasi SLA), tanpa "Tiket baru".
	crit, errs, err := w.svc.Create(ctx, w.draft(func(d *tickets.Draft) { d.RoomID = w.critRoom; d.Scope = 3 }), nil)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if crit.Priority != "CRITICAL" {
		t.Fatalf("unit kritis berhenti harus Kritis, dapat %s", crit.Priority)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events WHERE ticket_id = ? AND event_type = 'TICKET_CREATED'`, crit.ID) != 0 ||
		testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events WHERE ticket_id = ? AND event_type = 'TICKET_CRITICAL'`, crit.ID) != 1 {
		t.Fatal("tiket kritis: satu event TICKET_CRITICAL, tanpa TICKET_CREATED")
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM sla_escalations WHERE ticket_id = ? AND outbox_event_id IS NOT NULL`, crit.ID) != 1 {
		t.Fatal("eskalasi kritis harus menunjuk event outbox-nya")
	}

	c, _, _ := w.svc.Create(ctx, w.draft(nonCritical), nil)
	process(t, n)
	// Pengingat respons (77%) lalu pelanggaran; worker SLA diulang berkali-kali.
	w.clk.Advance(46 * time.Minute)
	for i := 0; i < 3; i++ {
		scan(t, w)
		process(t, n)
	}
	w.clk.Advance(20 * time.Minute)
	for i := 0; i < 3; i++ {
		scan(t, w)
		process(t, n)
	}
	for _, ev := range []string{notifications.SLAWarning, notifications.SLABreach} {
		if k := testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events WHERE ticket_id = ? AND event_type = ?`, c.ID, ev); k != 1 {
			t.Fatalf("%s: harus tepat 1 event, dapat %d", ev, k)
		}
		if k := testutil.Count(t, w.db, `SELECT COUNT(*) FROM notifications WHERE ticket_id = ? AND event_type = ? AND user_id = ?`, c.ID, ev, w.coord.ID); k != 1 {
			t.Fatalf("%s: koordinator harus menerima tepat 1 notifikasi, dapat %d", ev, k)
		}
	}
	// Respons belum diambil → petugas (pool) menerima peringatan respons.
	if inbox(t, w, w.tech.ID, notifications.SLABreach) == 0 {
		t.Fatal("pelanggaran respons tiket yang belum diambil dikirim ke petugas")
	}

	// Pelanggaran yang baru diketahui saat diselesaikan (notify = FALSE) tidak menghasilkan notifikasi.
	before := testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events`)
	if res, err := w.svc.Claim(ctx, c.ID, w.tech.ID); err != nil || !res.OK {
		t.Fatal(err)
	}
	w.clk.Advance(6 * time.Hour) // lewat batas penyelesaian tanpa worker berjalan
	if err := w.svc.Start(ctx, c.ID, w.tech); err != nil {
		t.Fatal(err)
	}
	if errs, err := w.svc.Resolve(ctx, c.ID, w.tech, tickets.ResolveInput{Action: "Ganti PSU", Result: "FIXED"}); err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM sla_escalations WHERE ticket_id = ? AND kind = 'RESOLUTION_BREACH' AND notify = FALSE`, c.ID) != 1 {
		t.Fatal("pelanggaran penyelesaian terlambat dicatat tanpa notifikasi")
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events WHERE ticket_id = ? AND event_type = 'SLA_BREACH' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.kind')) = 'RESOLUTION_BREACH'`, c.ID) != 0 {
		t.Fatal("pelanggaran notify = FALSE tidak boleh menulis outbox")
	}
	// Event lain (klaim, mulai, selesai) tetap tercatat.
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events`) <= before {
		t.Fatal("event alur kerja tetap tercatat")
	}
}

func TestPushDeliveryRetryAndExpiredSubscription(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	fs := &fakeSender{statuses: []int{503, 0, 201}}
	n := notifSvc(w, fs)
	subID := subscribe(t, n, w.tech.ID, "https://fcm.googleapis.com/fcm/send/uji-1")
	// Langganan yang sama didaftarkan ulang (mis. setelah login lagi) tidak menggandakan perangkat.
	if again := subscribe(t, n, w.tech.ID, "https://fcm.googleapis.com/fcm/send/uji-1"); again != subID {
		t.Fatalf("endpoint sama harus memperbarui baris yang sama: %d vs %d", again, subID)
	}

	if _, _, err := w.svc.Create(ctx, w.draft(nonCritical), nil); err != nil {
		t.Fatal(err)
	}
	if r := process(t, n); r.Deliveries != 1 {
		t.Fatalf("satu push dijadwalkan untuk perangkat petugas, dapat %+v", r)
	}
	send := func() notifications.PushResult {
		t.Helper()
		r, err := n.SendPush(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := send(); r.Retry != 1 {
		t.Fatalf("503 → dicoba lagi: %+v", r)
	}
	if r := send(); r.Sent+r.Retry+r.Failed != 0 {
		t.Fatalf("belum jatuh tempo, tidak boleh dikirim lagi: %+v", r)
	}
	w.clk.Advance(time.Minute)
	if r := send(); r.Retry != 1 {
		t.Fatalf("error jaringan → dicoba lagi: %+v", r)
	}
	w.clk.Advance(2 * time.Minute)
	if r := send(); r.Sent != 1 {
		t.Fatalf("percobaan ketiga terkirim: %+v", r)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM push_deliveries WHERE status = 'SENT' AND attempts = 3`) != 1 {
		t.Fatal("status pengiriman harus SENT setelah 3 percobaan")
	}
	if fs.sent[0].Title != "SIKAT" {
		t.Fatalf("judul push = %q", fs.sent[0].Title)
	}

	// 410 Gone → langganan dinonaktifkan, push lain untuk perangkat itu tidak dicoba.
	fs.statuses = []int{410}
	w.svc.Create(ctx, w.draft(nonCritical), nil)
	w.svc.Create(ctx, w.draft(nonCritical), nil)
	process(t, n)
	if r := send(); r.Gone != 1 {
		t.Fatalf("410 → langganan kedaluwarsa: %+v", r)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM push_subscriptions WHERE id = ? AND disabled_at IS NOT NULL`, subID) != 1 {
		t.Fatal("langganan 410 harus dinonaktifkan")
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM push_deliveries WHERE status = 'PENDING'`) != 0 {
		t.Fatal("push tertunda ke perangkat nonaktif harus dihentikan")
	}
	// Inbox tetap terisi walau push gagal.
	if inbox(t, w, w.tech.ID, notifications.TicketCreated) != 3 {
		t.Fatal("inbox tetap menerima semua notifikasi")
	}

	// Push basi (> 1 jam) tidak dikirim.
	subscribe(t, n, w.tech.ID, "https://fcm.googleapis.com/fcm/send/uji-2")
	w.svc.Create(ctx, w.draft(nonCritical), nil)
	process(t, n)
	w.clk.Advance(61 * time.Minute)
	if r := send(); r.Expired != 1 || r.Sent != 0 {
		t.Fatalf("push lebih dari 1 jam harus kedaluwarsa: %+v", r)
	}
}

func TestOutboxEventRetriedThenFailed(t *testing.T) {
	w := setupWF(t)
	ctx := context.Background()
	n := notifSvc(w, nil)
	// Event dengan tiket yang tidak ada → fan-out gagal → dicoba lagi lalu FAILED.
	conn, err := w.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 0`); err != nil {
		t.Fatal(err)
	}
	_, err = notifications.Publish(ctx, conn, notifications.Event{Type: notifications.TicketCreated, DedupeKey: "uji:rusak", TicketID: 999999}, w.clk.Now())
	_, _ = conn.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 1`)
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		r := process(t, n)
		if r.Failed != 1 {
			t.Fatalf("putaran %d: event harus gagal, dapat %+v", i+1, r)
		}
		if r := process(t, n); r.Failed != 0 {
			t.Fatal("event gagal tidak dicoba lagi sebelum jedanya habis")
		}
		w.clk.Advance(10 * time.Minute)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events WHERE dedupe_key = 'uji:rusak' AND status = 'FAILED' AND attempts = 5`) != 1 {
		t.Fatal("setelah 5 percobaan event berstatus FAILED")
	}
	// Publish ulang kunci yang sama tidak membuat event baru.
	if _, err := notifications.Publish(ctx, w.db, notifications.Event{Type: notifications.TicketCreated, DedupeKey: "uji:rusak"}, w.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if testutil.Count(t, w.db, `SELECT COUNT(*) FROM outbox_events WHERE dedupe_key = 'uji:rusak'`) != 1 {
		t.Fatal("dedupe_key mencegah event ganda")
	}
}
