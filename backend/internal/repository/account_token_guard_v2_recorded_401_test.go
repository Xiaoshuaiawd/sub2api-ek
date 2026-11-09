//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRecorded401ListRequiresEnabledGuardCooldownAndNoActiveTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("(?s)SELECT account.id.*Token revoked \\(401\\).*Authentication failed \\(401\\).*guard.enabled=TRUE.*guard.cooldown_until.*NOT EXISTS.*task.status IN.*ORDER BY guard.updated_at").
		WithArgs(20).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	ids, err := (&accountTokenGuardV2Repository{db: db}).ListRecorded401(context.Background(), 20)
	require.NoError(t, err)
	require.Equal(t, []int64{42}, ids)
	require.NoError(t, mock.ExpectationsWereMet())
}
