// Package audit mencatat jejak audit (append-only, dijaga trigger database).
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"sikat/internal/database"
	"sikat/internal/reqctx"
)

// Entry adalah satu catatan audit.
type Entry struct {
	Action     string // contoh: login.success, master.departments.update
	EntityType string
	EntityID   int64
	Before     any
	After      any
	// ActorUserID mengisi pelaku secara eksplisit (mis. login gagal sebelum
	// session ada). Bila 0, pelaku diambil dari context.
	ActorUserID int64
	// System menandai aksi dilakukan sistem/CLI, bukan pengguna web.
	System bool
}

// Record menyimpan entry audit menggunakan exec (bisa *sql.DB atau *sql.Tx).
func Record(ctx context.Context, exec database.DBTX, now time.Time, e Entry) error {
	info := reqctx.InfoFrom(ctx)

	actorType := "SYSTEM"
	var actor sql.NullInt64
	if e.ActorUserID != 0 {
		actorType = "USER"
		actor = sql.NullInt64{Int64: e.ActorUserID, Valid: true}
	} else if u := reqctx.UserFrom(ctx); u != nil && !e.System {
		actorType = "USER"
		actor = sql.NullInt64{Int64: u.ID, Valid: true}
	}

	before, err := toJSON(e.Before)
	if err != nil {
		return err
	}
	after, err := toJSON(e.After)
	if err != nil {
		return err
	}

	_, err = exec.ExecContext(ctx, `
		INSERT INTO audit_logs (actor_type, actor_user_id, action, entity_type, entity_id,
		                        before_data, after_data, ip_address, user_agent, request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		actorType, actor, e.Action, database.NullString(e.EntityType), database.NullInt64(e.EntityID),
		before, after, database.NullString(info.IP), database.NullString(truncate(info.UserAgent, 255)),
		database.NullString(info.RequestID), now)
	if err != nil {
		return fmt.Errorf("mencatat audit %s: %w", e.Action, err)
	}
	return nil
}

func toJSON(v any) (sql.NullString, error) {
	if v == nil {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("serialisasi audit: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Log adalah satu baris audit untuk ditampilkan.
type Log struct {
	ID         int64
	ActorType  string
	ActorName  sql.NullString
	Action     string
	EntityType sql.NullString
	EntityID   sql.NullInt64
	BeforeData sql.NullString
	AfterData  sql.NullString
	IPAddress  sql.NullString
	CreatedAt  time.Time
}

// Filter untuk daftar audit.
type Filter struct {
	EntityType string
	EntityID   int64
	Action     string
	Limit      int
	Offset     int
}

// List mengambil log audit terbaru sesuai filter, beserta total baris.
func List(ctx context.Context, db database.DBTX, f Filter) ([]Log, int, error) {
	where := "WHERE 1=1"
	var args []any
	if f.EntityType != "" {
		where += " AND a.entity_type = ?"
		args = append(args, f.EntityType)
	}
	if f.EntityID != 0 {
		where += " AND a.entity_id = ?"
		args = append(args, f.EntityID)
	}
	if f.Action != "" {
		where += " AND a.action LIKE ?"
		args = append(args, f.Action+"%")
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs a "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT a.id, a.actor_type, u.full_name, a.action, a.entity_type, a.entity_id,
		       a.before_data, a.after_data, a.ip_address, a.created_at
		  FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_user_id `+where+`
		 ORDER BY a.id DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Log
	for rows.Next() {
		var l Log
		if err := rows.Scan(&l.ID, &l.ActorType, &l.ActorName, &l.Action, &l.EntityType, &l.EntityID,
			&l.BeforeData, &l.AfterData, &l.IPAddress, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}
