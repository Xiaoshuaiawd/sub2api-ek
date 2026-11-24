//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountWaitCounterResultCache struct {
	ConcurrencyCache
	allowed bool
	err     error
}

func (c accountWaitCounterResultCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	return c.allowed, c.err
}

func TestAccountWaitCounterPreservesReservationOwnership(t *testing.T) {
	failure := errors.New("synthetic Redis result unavailable")
	for _, tc := range []struct {
		name    string
		allowed bool
		err     error
	}{
		{name: "granted", allowed: true},
		{name: "full"},
		{name: "unconfirmed", err: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewConcurrencyService(accountWaitCounterResultCache{allowed: tc.allowed, err: tc.err})
			allowed, err := svc.TryIncrementAccountWaitCount(context.Background(), 123, 2)
			require.Equal(t, tc.allowed, allowed)
			require.ErrorIs(t, err, tc.err)
			if tc.err != nil {
				legacyAllowed, legacyErr := svc.IncrementAccountWaitCount(context.Background(), 123, 2)
				require.True(t, legacyAllowed, "the legacy service API retains its explicit fail-open policy")
				require.NoError(t, legacyErr)
			}
		})
	}
	var unavailable *ConcurrencyService
	allowed, err := unavailable.TryIncrementAccountWaitCount(context.Background(), 123, 2)
	require.False(t, allowed)
	require.Error(t, err)
}
