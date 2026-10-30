//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"
)

func TestSettingIntelligenceRulesCompareAndSet(t *testing.T) {
	for _, tc := range []struct {
		name     string
		exists   bool
		affected int64
		fail     bool
	}{
		{"update", true, 1, false},
		{"stale update", true, 0, false},
		{"insert missing", false, 1, false},
		{"insert conflict", false, 0, false},
		{"database update error", true, 0, true},
		{"database insert error", false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &settingRepository{client: ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))}
			dbErr := errors.New("database failed")
			if tc.exists {
				q := mock.ExpectExec(regexp.QuoteMeta(`UPDATE "settings" SET "value" = $1, "updated_at" = $2 WHERE "settings"."key" = $3 AND "settings"."value" = $4`)).
					WithArgs("new", sqlmock.AnyArg(), "intelligence_routing_rules_v1", "old")
				if tc.fail {
					q.WillReturnError(dbErr)
				} else {
					q.WillReturnResult(sqlmock.NewResult(0, tc.affected))
				}
			} else {
				q := mock.ExpectQuery(`INSERT INTO "settings".*ON CONFLICT.*DO NOTHING.*RETURNING`).
					WithArgs("intelligence_routing_rules_v1", "new", sqlmock.AnyArg())
				if tc.fail {
					q.WillReturnError(dbErr)
				} else if tc.affected == 1 {
					q.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
				} else {
					q.WillReturnRows(sqlmock.NewRows([]string{"id"}))
				}
			}
			ok, err := repo.CompareAndSetIntelligenceRules(context.Background(), "intelligence_routing_rules_v1", "old", "new", tc.exists)
			if tc.fail {
				require.ErrorIs(t, err, dbErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.affected == 1, ok)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
