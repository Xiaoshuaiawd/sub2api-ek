package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExcelBPSWarmTargetsIgnoreLegacyOpenAIProxySettings(t *testing.T) {
	makeAccount := func(n int, source string) Account {
		a := *excelAccount()
		a.Concurrency = n
		a.Status = StatusActive
		a.Schedulable = true
		a.Extra["openai_excel_bps_mihomo"] = true
		a.Extra[ExcelBPSProxySourceKey] = source
		return a
	}
	a := makeAccount(4, "mihomo")
	b := makeAccount(3, "mihomo")
	c := makeAccount(2, ExcelBPSProxySourceIPPool)
	off := makeAccount(90, "mihomo")
	off.Extra["openai_excel_bps_mihomo"] = false
	inactive := makeAccount(90, "mihomo")
	inactive.Status = "disabled"
	paused := makeAccount(90, "mihomo")
	paused.Schedulable = false
	pending := makeAccount(90, "mihomo")
	pending.Extra["openai_excel_bps"] = false
	pending.Extra[ExcelBPSAutoRecoverOn403Key] = true
	pending.Extra["openai_excel_bps_auto_disable_on_403"] = true
	pending.Extra[ExcelBPS403DisabledAtKey] = time.Now().Format(time.RFC3339Nano)
	require.True(t, pending.IsExcelBPS403RecoveryPending())
	m, s := excelBPSWarmTargets([]Account{a, b, c, off, inactive, paused, pending})
	require.Zero(t, m)
	require.Zero(t, s)
	a.Concurrency = 9
	m, s = excelBPSWarmTargets([]Account{a, c})
	require.Zero(t, m)
	require.Zero(t, s)
	large := makeAccount(100000, "mihomo")
	m, _ = excelBPSWarmTargets([]Account{large, large})
	require.Zero(t, m)
}

type warmProjectionRepo struct {
	AccountRepository
	projectionCalls int
	legacyCalls     int
}

func (r *warmProjectionRepo) ListBPSWarmAccounts(context.Context) ([]Account, error) {
	r.projectionCalls++
	return nil, errors.New("projection unavailable")
}

func (r *warmProjectionRepo) ListByPlatform(context.Context, string) ([]Account, error) {
	r.legacyCalls++
	return nil, errors.New("full hydration must not be retried")
}

func TestBPSWarmUsesProjectionWithoutFullHydrationFallback(t *testing.T) {
	repo := &warmProjectionRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.syncBPSWarmPool(context.Background())
	require.Equal(t, 1, repo.projectionCalls)
	require.Zero(t, repo.legacyCalls)
}

type warmLifecycleRepo struct {
	AccountRepository
	started chan struct{}
}

func (r *warmLifecycleRepo) ListByPlatform(ctx context.Context, _ string) ([]Account, error) {
	close(r.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestExcelBPSWarmWorkerStartsBeforeTrafficAndStops(t *testing.T) {
	repo := &warmLifecycleRepo{started: make(chan struct{})}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.StartBPSWarmPool()
	svc.StartBPSWarmPool()
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start without a request")
	}
	done := make(chan struct{})
	go func() { svc.StopBPSWarmPool(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	svc.StartBPSWarmPool()
	svc.StopBPSWarmPool()
}
