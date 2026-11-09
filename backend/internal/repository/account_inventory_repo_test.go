//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestAccountInventoryClaimReservesOnlyMissingSlots(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT target_count, enabled FROM openai_account_inventory_config.*FOR UPDATE").WillReturnRows(
		sqlmock.NewRows([]string{"target_count", "enabled"}).AddRow(400, true))
	mock.ExpectExec("(?s)UPDATE openai_account_inventory SET state='pending'.*lease_until < NOW\\(\\)").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE openai_account_inventory inventory SET state='retired'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(399))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM openai_account_inventory WHERE state='activating'").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT id, email, password_ciphertext, mfa_ciphertext").WithArgs(1).WillReturnRows(
		sqlmock.NewRows([]string{"id", "email", "password_ciphertext", "mfa_ciphertext"}).AddRow(7, "new@example.com", "encrypted-password", "encrypted-mfa"))
	mock.ExpectExec("UPDATE openai_account_inventory SET state='activating'").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	repo := NewAccountInventoryRepository(db)
	claims, err := repo.Claim(context.Background(), 4)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, int64(7), claims[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountInventoryCanActivateRechecksTargetAndEarlierClaims(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("(?s)SELECT config.enabled.*id < ").WithArgs(int64(8)).WillReturnRows(
		sqlmock.NewRows([]string{"allowed"}).AddRow(false))
	allowed, err := NewAccountInventoryRepository(db).CanActivate(context.Background(), 8)
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, mock.ExpectationsWereMet())
}
