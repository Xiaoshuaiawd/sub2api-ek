//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRecorded401TaskCreationRechecksCurrentAccountErrorUnderRowLock(t *testing.T) {
	const message = "Token revoked (401): revoked"
	for _, tc := range []struct {
		name   string
		rows   *sqlmock.Rows
		rowErr error
		want   bool
	}{
		{name: "still failed", rows: sqlmock.NewRows([]string{"id"}).AddRow(42), want: true},
		{name: "already recovered", rowErr: sql.ErrNoRows, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectBegin()
			expect := mock.ExpectQuery("(?s)SELECT account.id FROM accounts account.*account.status='error'.*account.error_message=\\$2.*FOR SHARE").WithArgs(int64(42), message)
			if tc.rowErr != nil {
				expect.WillReturnError(tc.rowErr)
			} else {
				expect.WillReturnRows(tc.rows)
			}
			mock.ExpectRollback()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			stillFailed, err := recorded401FailureStillCurrent(context.Background(), tx, 42, message)
			require.NoError(t, err)
			require.Equal(t, tc.want, stillFailed)
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
