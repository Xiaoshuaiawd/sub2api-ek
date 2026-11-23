package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUserSubscriptionGetByIDForUpdateLocksRow(t *testing.T) {
	var capturedSQL string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &capturedSQL}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewUserSubscriptionRepository(client)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery("locked subscription").WillReturnRows(
		sqlmock.NewRows(usersubscription.Columns).AddRow(
			int64(7), now, now, nil, int64(11), int64(13), []byte("[13]"), nil, nil, nil, nil, nil, nil, nil, nil,
			now, now.AddDate(0, 0, 30), "active",
			nil, nil, nil, 0.0, 0.0, 0.0, nil, now, "renewal",
		),
	)

	sub, err := repo.GetByIDForUpdate(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), sub.ID)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Contains(t, strings.ToUpper(normalizeSQLWhitespace(capturedSQL)), "FOR UPDATE")
}

func TestSubscriptionWindowCASAppliesPendingQuotaFromCurrentRow(t *testing.T) {
	_, client := newUserEntRepo(t)
	ctx := context.Background()
	u, err := client.User.Create().SetEmail("rollover@example.com").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	g, err := client.Group.Create().SetName("rollover").Save(ctx)
	require.NoError(t, err)
	previous := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	current := previous.AddDate(0, 0, 1)
	sub, err := client.UserSubscription.Create().
		SetUserID(u.ID).SetGroupID(g.ID).SetStartsAt(previous).SetExpiresAt(current.AddDate(0, 0, 30)).
		SetDailyWindowStart(previous).SetDailyUsageUsd(3).SetDailyLimitUsd(5).SetWeeklyLimitUsd(60).
		SetPendingPlanID(8).SetPendingDailyLimitUsd(2).SetPendingWeeklyLimitUsd(10).Save(ctx)
	require.NoError(t, err)
	repo := NewUserSubscriptionRepository(client)

	require.NoError(t, repo.ResetDailyUsage(ctx, sub.ID, &previous, current))
	got, err := client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, current, *got.DailyWindowStart)
	require.Equal(t, 2.0, *got.DailyLimitUsd)
	require.Zero(t, got.DailyUsageUsd)
	require.Nil(t, got.PendingDailyLimitUsd)
	require.Equal(t, 60.0, *got.WeeklyLimitUsd)
	require.Equal(t, 10.0, *got.PendingWeeklyLimitUsd)
	require.Equal(t, int64(8), *got.PendingPlanID)

	// A late loser must neither clear usage in the new window nor consume
	// the pending quota from a purchase made in that window.
	_, err = client.UserSubscription.UpdateOneID(sub.ID).SetDailyUsageUsd(1).SetPendingDailyLimitUsd(1).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.ResetDailyUsage(ctx, sub.ID, &previous, current))
	got, err = client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, 1.0, got.DailyUsageUsd)
	require.Equal(t, 2.0, *got.DailyLimitUsd)
	require.Equal(t, 1.0, *got.PendingDailyLimitUsd)
}
