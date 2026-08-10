package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"strings"
)

func (s *Store) GetSettingString(ctx context.Context, key, fallback string) string {
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&raw); err != nil {
		return fallback
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" {
		return fallback
	}
	return value
}

func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,now()) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=now()`, key, raw)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string, value any) error {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func (s *Store) WriteAudit(ctx context.Context, actor, action, target string, metadata any) error {
	raw := []byte(`{}`)
	if metadata != nil {
		var err error
		raw, err = json.Marshal(metadata)
		if err != nil {
			return err
		}
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO audit_logs(actor,action,target,metadata) VALUES($1,$2,$3,$4)`, actor, action, target, raw)
	return err
}

func (s *Store) ListAuditLogs(ctx context.Context, limit int) ([]domain.AuditLog, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	return s.listAuditLogsPage(ctx, limit, 0)
}

func (s *Store) ListAuditLogsPage(ctx context.Context, page, pageSize int) ([]domain.AuditLog, int64, error) {
	return s.ListAuditLogsPageFiltered(ctx, page, pageSize, AuditLogPageFilter{})
}

func (s *Store) ListAuditLogsPageFiltered(ctx context.Context, page, pageSize int, filter AuditLogPageFilter) ([]domain.AuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Action = strings.TrimSpace(filter.Action)
	const where = ` WHERE ($1='' OR actor ILIKE '%'||$1||'%' OR action ILIKE '%'||$1||'%' OR target ILIKE '%'||$1||'%')
		AND ($2='' OR action=$2) AND ($3::timestamptz IS NULL OR created_at>=$3) AND ($4::timestamptz IS NULL OR created_at<$4)`
	args := []any{filter.Search, filter.Action, filter.CreatedFrom, filter.CreatedTo}
	var total int64
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(ctx, `SELECT id,actor,action,target,metadata,created_at FROM audit_logs`+where+` ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6`, append(args, pageSize, int64(page-1)*int64(pageSize))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	logs := make([]domain.AuditLog, 0, pageSize)
	for rows.Next() {
		var a domain.AuditLog
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Target, &a.Metadata, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, a)
	}
	return logs, total, rows.Err()
}

func (s *Store) ListAuditActions(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT action FROM audit_logs WHERE action<>'' ORDER BY action LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	actions := make([]string, 0)
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (s *Store) listAuditLogsPage(ctx context.Context, limit int, offset int64) ([]domain.AuditLog, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,actor,action,target,metadata,created_at FROM audit_logs ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AuditLog, 0, limit)
	for rows.Next() {
		var a domain.AuditLog
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Target, &a.Metadata, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
