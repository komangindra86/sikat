package tickets

import (
	"context"
	"database/sql"
	"errors"

	"sikat/internal/database"
	"sikat/internal/rbac"
	"sikat/internal/workflows"
)

// Triase & koreksi jenis gangguan (16/11/2026). Pelapor yang tidak menemukan
// masalahnya di daftar memilih "Lainnya" → bidang Triase. Koordinator bidang
// tiket (atau penanggung jawab tiket) mengoreksi jenis gangguan; bidang ikut
// berubah, kebijakan SLA dipilih ulang, koordinator/petugas bidang baru diberi
// tahu, dan perubahan tercatat di histori.

// ChangeCategory mengoreksi jenis gangguan tiket aktif.
func (s *Service) ChangeCategory(ctx context.Context, id int64, u *rbac.User, categoryID int64, reason string) (map[string]string, error) {
	errs := map[string]string{}
	reason = cleanText(reason, 255, "Alasan", false, errs, "category_reason")
	if len(errs) > 0 {
		return errs, nil
	}
	now := s.clock.Now()
	err := database.WithTxRetry(ctx, s.db, 3, func(tx *sql.Tx) error {
		t, err := lockTicket(ctx, tx, id)
		if err != nil {
			return err
		}
		// Penanggung jawab boleh mengoreksi tiketnya; koordinator hanya untuk bidangnya.
		if !(t.isPrimary(u) && u.Has(rbac.PermTicketWork)) {
			if !u.Has(rbac.PermTicketTakeover) {
				return ErrForbidden
			}
			if err := requireField(ctx, tx, u, t.ITCategoryID); err != nil {
				return err
			}
		}
		if workflows.IsTerminal(t.Status) || t.Status == workflows.Resolved {
			return ErrTransition
		}
		var newField int64
		var oldName, newName, fieldName string
		var oldTriage bool
		err = tx.QueryRowContext(ctx, `
			SELECT tc.it_category_id, tc.name, ic.name
			  FROM ticket_categories tc JOIN it_categories ic ON ic.id = tc.it_category_id
			 WHERE tc.id = ? AND tc.is_active = TRUE AND ic.is_active = TRUE`, categoryID).Scan(&newField, &newName, &fieldName)
		if errors.Is(err, sql.ErrNoRows) {
			errs["ticket_category_id"] = "Pilih jenis gangguan yang aktif."
			return nil
		}
		if err != nil {
			return err
		}
		if categoryID == t.TicketCategoryID {
			errs["ticket_category_id"] = "Jenis gangguan tidak berubah."
			return nil
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT tc.name, ic.is_triage FROM ticket_categories tc JOIN it_categories ic ON ic.id = tc.it_category_id
			 WHERE tc.id = ?`, t.TicketCategoryID).Scan(&oldName, &oldTriage); err != nil {
			return err
		}
		// Alasan wajib kecuali memilah dari Triase (alasannya sudah jelas).
		if reason == "" && !oldTriage {
			errs["category_reason"] = "Jelaskan alasan koreksi."
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tickets SET ticket_category_id = ?, it_category_id = ?,
			       reported_ticket_category_id = COALESCE(reported_ticket_category_id, ?),
			       version = version + 1, updated_at = ?
			 WHERE id = ?`, categoryID, newField, t.TicketCategoryID, now, id); err != nil {
			return err
		}
		fieldChanged := newField != t.ITCategoryID
		meta := map[string]any{"from_category_id": t.TicketCategoryID, "to_category_id": categoryID,
			"from_category": oldName, "to_category": newName, "from_field_id": t.ITCategoryID, "to_field_id": newField}
		note := oldName + " → " + newName
		if fieldChanged {
			note += " (bidang " + fieldName + ")"
		}
		if reason != "" {
			note += ": " + reason
		}
		// Kebijakan SLA dapat bergantung pada jenis gangguan/bidang: pilih ulang.
		t.TicketCategoryID, t.ITCategoryID = categoryID, newField
		if err := s.reassignSLA(ctx, tx, t, t.Priority, t.FirstResponseAt.Valid, now); err != nil {
			return err
		}
		// Bidang sama: cukup histori, tanpa notifikasi "dialihkan ke bidang".
		return s.repo.addHistory(ctx, tx, historyEntry{TicketID: id, Event: EventCategoryChanged, ActorType: "USER",
			ActorID: u.ID, Note: note, Meta: jsonMeta(meta), NoNotify: !fieldChanged}, now)
	})
	if err != nil || len(errs) > 0 {
		return errs, err
	}
	return nil, nil
}
