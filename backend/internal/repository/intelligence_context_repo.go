package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type intelligenceContextRepository struct{ db *sql.DB }

func NewIntelligenceContextRepository(db *sql.DB) service.IntelligenceContextRepository {
	return &intelligenceContextRepository{db: db}
}

func (r *intelligenceContextRepository) Create(ctx context.Context, value *service.IntelligenceRequestContext) error {
	if value == nil || value.ID == "" || value.APIKeyID <= 0 || value.UserID <= 0 {
		return errors.New("invalid intelligence context")
	}
	rules, err := json.Marshal(value.MatchedRules)
	if err != nil {
		return err
	}
	created := value.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO intelligence_request_contexts (id, api_key_id, user_id, matched_rules, request_body, content_type, content_encoding, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, value.APIKeyID, value.UserID, string(rules), value.Body, value.ContentType, value.ContentEncoding, created)
	return err
}

func (r *intelligenceContextRepository) Link(ctx context.Context, id string, apiKeyID int64, requestID string) error {
	if id == "" || apiKeyID <= 0 || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid intelligence context link")
	}
	res, err := r.db.ExecContext(ctx, `UPDATE intelligence_request_contexts ic SET usage_request_id=$1 WHERE ic.id=$2 AND ic.api_key_id=$3 AND (ic.usage_request_id IS NULL OR ic.usage_request_id=$1) AND NOT EXISTS (SELECT 1 FROM usage_logs u WHERE u.api_key_id=ic.api_key_id AND u.user_id=ic.user_id AND u.request_id=$1 AND u.created_at < ic.created_at)`, requestID, id, apiKeyID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var prior bool
		err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM intelligence_request_contexts ic JOIN usage_logs u ON u.api_key_id=ic.api_key_id AND u.user_id=ic.user_id AND u.request_id=$3 WHERE ic.id=$1 AND ic.api_key_id=$2 AND u.created_at < ic.created_at)`, id, apiKeyID, requestID).Scan(&prior)
		if err != nil {
			return err
		}
		if prior {
			return service.ErrIntelligenceContextPriorUsage
		}
		return service.ErrIntelligenceContextNotFound
	}
	return nil
}

type intelligenceContextScanner interface{ Scan(...any) error }

func scanIntelligenceContext(row intelligenceContextScanner) (*service.IntelligenceRequestContext, error) {
	var result service.IntelligenceRequestContext
	var raw []byte
	var requestID sql.NullString
	var usageID sql.NullInt64
	err := row.Scan(&result.ID, &result.APIKeyID, &result.UserID, &requestID, &raw, &result.Body, &result.ContentType, &result.ContentEncoding, &result.CreatedAt, &usageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrIntelligenceContextNotFound
	}
	if err != nil {
		return nil, err
	}
	if requestID.Valid {
		result.UsageRequestID = &requestID.String
	}
	if usageID.Valid {
		result.UsageLogID = &usageID.Int64
	}
	if err := json.Unmarshal(raw, &result.MatchedRules); err != nil {
		return nil, err
	}
	return &result, nil
}

const intelligenceContextUsageID = `(SELECT u2.id FROM usage_logs u2 WHERE u2.api_key_id=ic.api_key_id AND u2.user_id=ic.user_id AND u2.request_id=ic.usage_request_id AND u2.created_at >= ic.created_at LIMIT 1)`
const intelligenceContextColumns = `ic.id::text,ic.api_key_id,ic.user_id,ic.usage_request_id,ic.matched_rules,ic.request_body,ic.content_type,ic.content_encoding,ic.created_at,` + intelligenceContextUsageID

func (r *intelligenceContextRepository) FindByUsage(ctx context.Context, usageID int64) (*service.IntelligenceRequestContext, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+intelligenceContextColumns+` FROM intelligence_request_contexts ic JOIN usage_logs u ON u.api_key_id=ic.api_key_id AND u.user_id=ic.user_id AND u.request_id=ic.usage_request_id AND u.created_at >= ic.created_at WHERE u.id=$1 LIMIT 2`, usageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrIntelligenceContextNotFound
	}
	value, err := scanIntelligenceContext(rows)
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, service.ErrIntelligenceContextAmbiguous
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return value, nil
}

func (r *intelligenceContextRepository) FindByID(ctx context.Context, id string) (*service.IntelligenceRequestContext, error) {
	return scanIntelligenceContext(r.db.QueryRowContext(ctx, `SELECT `+intelligenceContextColumns+` FROM intelligence_request_contexts ic WHERE ic.id=$1`, id))
}

func (r *intelligenceContextRepository) Available(ctx context.Context, ids []int64) (map[int64]bool, error) {
	result := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, len(ids))
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT u.id FROM usage_logs u JOIN intelligence_request_contexts ic ON ic.api_key_id=u.api_key_id AND ic.user_id=u.user_id AND ic.usage_request_id=u.request_id AND u.created_at >= ic.created_at WHERE u.id IN (`+strings.Join(placeholders, ",")+`) GROUP BY u.id HAVING COUNT(*)=1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (r *intelligenceContextRepository) List(ctx context.Context, page, pageSize int) ([]service.IntelligenceRequestContext, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM intelligence_request_contexts`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT ic.id::text,ic.api_key_id,ic.user_id,ic.usage_request_id,ic.matched_rules,NULL::bytea,ic.content_type,ic.content_encoding,ic.created_at,`+intelligenceContextUsageID+` FROM intelligence_request_contexts ic ORDER BY ic.created_at DESC,ic.id DESC LIMIT $1 OFFSET $2`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	values := make([]service.IntelligenceRequestContext, 0)
	for rows.Next() {
		value, err := scanIntelligenceContext(rows)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, *value)
	}
	return values, total, rows.Err()
}
