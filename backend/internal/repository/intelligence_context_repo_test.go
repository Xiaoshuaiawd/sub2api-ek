package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceContextPersistsMatchedKeywords(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewIntelligenceContextRepository(db)
	id := "00000000-0000-4000-8000-000000000001"
	matched := []service.IntelligenceRuleMatch{{ID: "builtin.pelican_bicycle", Name: "鹈鹕骑自行车", Type: "builtin", MatchedKeywords: []string{"鹈鹕", "自行车"}}}
	raw, err := json.Marshal(matched)
	require.NoError(t, err)
	now := time.Unix(100, 0)
	item := &service.IntelligenceRequestContext{ID: id, APIKeyID: 3, UserID: 7, Body: []byte("body"), MatchedRules: matched, CreatedAt: now}
	mock.ExpectExec("INSERT INTO intelligence_request_contexts").WithArgs(id, int64(3), int64(7), string(raw), item.Body, "", "", now).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Create(context.Background(), item))
	cols := []string{"id", "key", "user", "request", "rules", "body", "type", "encoding", "created", "usage"}
	mock.ExpectQuery("SELECT .* FROM intelligence_request_contexts ic WHERE ic.id=").WithArgs(id).WillReturnRows(sqlmock.NewRows(cols).AddRow(id, 3, 7, nil, raw, item.Body, "", "", now, nil))
	got, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, []string{"鹈鹕", "自行车"}, got.MatchedRules[0].MatchedKeywords)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIntelligenceContextRepositoryPreservesBodyAndScopesLink(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewIntelligenceContextRepository(db)
	id := "00000000-0000-4000-8000-000000000001"
	body := []byte{0, 255, 2, 3}
	item := &service.IntelligenceRequestContext{ID: id, APIKeyID: 3, UserID: 7, Body: body, ContentType: "multipart/form-data", ContentEncoding: "gzip", MatchedRules: []service.IntelligenceRuleMatch{}, CreatedAt: time.Unix(100, 0)}
	mock.ExpectExec("INSERT INTO intelligence_request_contexts").WithArgs(id, int64(3), int64(7), "[]", body, item.ContentType, item.ContentEncoding, item.CreatedAt).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Create(context.Background(), item))
	mock.ExpectExec("UPDATE intelligence_request_contexts").WithArgs("client:abc", id, int64(4)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`(?s)SELECT EXISTS.*u\.created_at\s*<\s*ic\.created_at`).WithArgs(id, int64(4), "client:abc").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	require.ErrorIs(t, repo.Link(context.Background(), id, 4, "client:abc"), service.ErrIntelligenceContextNotFound)
	mock.ExpectExec("UPDATE intelligence_request_contexts").WithArgs("client:abc", id, int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Link(context.Background(), id, 3, "client:abc"))
	query := "SELECT .* FROM intelligence_request_contexts ic JOIN usage_logs u ON u.api_key_id=ic.api_key_id AND u.user_id=ic.user_id AND u.request_id=ic.usage_request_id AND u.created_at >= ic.created_at WHERE u.id=\\$1"
	cols := []string{"id", "key", "user", "request", "rules", "body", "type", "encoding", "created", "usage"}
	mock.ExpectQuery(query).WithArgs(int64(22)).WillReturnRows(sqlmock.NewRows(cols).AddRow(id, 3, 7, "client:abc", "[]", body, item.ContentType, item.ContentEncoding, item.CreatedAt, 22))
	got, err := repo.FindByUsage(context.Background(), 22)
	require.NoError(t, err)
	require.Equal(t, body, got.Body)
	require.Equal(t, "gzip", got.ContentEncoding)
	require.EqualValues(t, 22, *got.UsageLogID)
	mock.ExpectQuery(query).WithArgs(int64(22)).WillReturnRows(sqlmock.NewRows(cols).AddRow(id, 3, 7, "client:abc", "[]", body, item.ContentType, item.ContentEncoding, item.CreatedAt, 22).AddRow("00000000-0000-4000-8000-000000000002", 3, 7, "client:abc", "[]", body, item.ContentType, item.ContentEncoding, item.CreatedAt, 22))
	_, err = repo.FindByUsage(context.Background(), 22)
	require.ErrorIs(t, err, service.ErrIntelligenceContextAmbiguous)
	mock.ExpectQuery("SELECT u.id FROM usage_logs .* GROUP BY u.id HAVING COUNT\\(\\*\\)=1").WithArgs(int64(22)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	available, err := repo.Available(context.Background(), []int64{22})
	require.NoError(t, err)
	require.False(t, available[22])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIntelligenceContextRejectsPreviouslyExistingUsage(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewIntelligenceContextRepository(db)
	id := "00000000-0000-4000-8000-000000000001"
	mock.ExpectExec(`(?s)UPDATE intelligence_request_contexts.*NOT EXISTS.*u\.created_at\s*<\s*ic\.created_at`).WithArgs("same-response", id, int64(3)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`(?s)SELECT EXISTS.*u\.created_at\s*<\s*ic\.created_at`).WithArgs(id, int64(3), "same-response").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	require.ErrorIs(t, repo.Link(context.Background(), id, 3, "same-response"), service.ErrIntelligenceContextPriorUsage)
	require.NoError(t, mock.ExpectationsWereMet())
}
