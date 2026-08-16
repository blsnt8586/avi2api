package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
)

func (s *Store) WriteAPIRequestLog(ctx context.Context, entry domain.APIRequestLog) error {
	return s.WriteAPIRequestLogs(ctx, []domain.APIRequestLog{entry})
}

func (s *Store) WriteAPIRequestLogs(ctx context.Context, entries []domain.APIRequestLog) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows := make([][]any, 0, len(entries))
	usage := make(map[uuid.UUID]int64)
	for _, entry := range entries {
		providerID := strings.ToLower(strings.TrimSpace(entry.ProviderID))
		if providerID == "" {
			providerID = "leonardo"
		}
		parameters := entry.Parameters
		if len(parameters) == 0 || !json.Valid(parameters) {
			parameters = json.RawMessage(`{}`)
		}
		rows = append(rows, []any{
			entry.RequestID, providerID, entry.APIKeyID, entry.APIKeyPrefix, entry.AccountID, entry.TaskID,
			entry.Method, entry.Path, entry.Kind, entry.Model, parameters, entry.PromptChars,
			entry.EstimatedTokens, entry.Status, entry.ErrorCode, entry.DurationMS, entry.ClientIP,
		})
		if entry.APIKeyID != nil {
			usage[*entry.APIKeyID]++
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"api_request_logs"}, []string{
		"request_id", "provider_id", "api_key_id", "api_key_prefix", "account_id", "task_id", "method", "path", "kind", "model",
		"parameters", "prompt_chars", "estimated_tokens", "status", "error_code", "duration_ms", "client_ip",
	}, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	for keyID, count := range usage {
		if _, err := tx.Exec(ctx, `UPDATE api_keys SET request_count=request_count+$2,last_used_at=$3 WHERE id=$1`, keyID, count, time.Now()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListAPIRequestLogsPage(ctx context.Context, page, pageSize int, search string, status int) ([]domain.APIRequestLog, int64, error) {
	return s.ListAPIRequestLogsPageFiltered(ctx, page, pageSize, APIRequestLogPageFilter{Search: search, Status: status})
}

type APIRequestLogPageFilter struct {
	Search      string
	ProviderID  string
	Status      int
	Method      string
	Kind        string
	Path        string
	Model       string
	APIKey      string
	ClientIP    string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

func (s *Store) ListAPIRequestLogsPageFiltered(ctx context.Context, page, pageSize int, filter APIRequestLogPageFilter) ([]domain.APIRequestLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.ProviderID = strings.ToLower(strings.TrimSpace(filter.ProviderID))
	filter.Method = strings.ToUpper(strings.TrimSpace(filter.Method))
	filter.Kind = strings.TrimSpace(filter.Kind)
	filter.Path = strings.TrimSpace(filter.Path)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.APIKey = strings.TrimSpace(filter.APIKey)
	filter.ClientIP = strings.TrimSpace(filter.ClientIP)
	const where = ` WHERE ($1='' OR l.request_id ILIKE '%'||$1||'%' OR l.api_key_prefix ILIKE '%'||$1||'%'
		OR l.path ILIKE '%'||$1||'%' OR l.model ILIKE '%'||$1||'%' OR l.error_code ILIKE '%'||$1||'%'
		OR l.client_ip ILIKE '%'||$1||'%' OR l.provider_id ILIKE '%'||$1||'%' OR coalesce(a.name,'') ILIKE '%'||$1||'%')
		AND ($2='' OR l.provider_id=$2) AND ($3=0 OR l.status=$3) AND ($4='' OR l.method=$4) AND ($5='' OR l.kind=$5)
		AND ($6='' OR l.path ILIKE '%'||$6||'%') AND ($7='' OR l.model=$7)
		AND ($8='' OR l.api_key_prefix ILIKE '%'||$8||'%') AND ($9='' OR l.client_ip ILIKE '%'||$9||'%')
		AND ($10::timestamptz IS NULL OR l.created_at>=$10) AND ($11::timestamptz IS NULL OR l.created_at<$11)`
	args := []any{filter.Search, filter.ProviderID, filter.Status, filter.Method, filter.Kind, filter.Path, filter.Model, filter.APIKey, filter.ClientIP, filter.CreatedFrom, filter.CreatedTo}
	var total int64
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM api_request_logs l LEFT JOIN accounts a ON a.id=l.account_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(ctx, `SELECT l.id,l.request_id,l.provider_id,l.api_key_id,l.api_key_prefix,l.account_id,
		coalesce(a.name,''),l.task_id,l.method,l.path,l.kind,l.model,l.parameters,l.prompt_chars,
		l.estimated_tokens,l.status,l.error_code,l.duration_ms,l.client_ip,l.created_at
		FROM api_request_logs l LEFT JOIN accounts a ON a.id=l.account_id`+where+`
		ORDER BY l.created_at DESC,l.id DESC LIMIT $12 OFFSET $13`, append(args, pageSize, int64(page-1)*int64(pageSize))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.APIRequestLog, 0, pageSize)
	for rows.Next() {
		var entry domain.APIRequestLog
		if err := rows.Scan(&entry.ID, &entry.RequestID, &entry.ProviderID, &entry.APIKeyID, &entry.APIKeyPrefix,
			&entry.AccountID, &entry.AccountName, &entry.TaskID, &entry.Method, &entry.Path,
			&entry.Kind, &entry.Model, &entry.Parameters, &entry.PromptChars, &entry.EstimatedTokens,
			&entry.Status, &entry.ErrorCode, &entry.DurationMS, &entry.ClientIP, &entry.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, entry)
	}
	return out, total, rows.Err()
}
