package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/mihomo"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSWarpDoesNotAcquireLegacyProxy(t *testing.T) {
	for _, reason := range []string{"candidate_checks_exhausted", "acquisition_timeout", "no_eligible_nodes", "session_draining", "session_capacity", "manager_unavailable", "warm_pool_empty"} {
		t.Run(reason, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			account := excelAccount()
			account.Extra["openai_excel_bps_mihomo"] = true
			acquired, calls := 0, 0
			acquire := func(context.Context, string, ...string) (string, excelBPSLease, error) {
				acquired++
				return "", nil, fmt.Errorf("legacy proxy: %w", &mihomo.BPSAcquireError{Reason: reason, Candidates: 2})
			}
			svc := &OpenAIGatewayService{httpUpstream: &bpsTestUpstream{send: func(_ *http.Request, proxy string) (*http.Response, error) {
				calls++
				require.Equal(t, OpenAIWarpProxyURL(), proxy)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			}}}
			resp, lease, proxy, err := svc.doExcelBPSRequest(context.Background(), c, account, "private-session", []byte("body"), "secret-token", "secret-account", acquire)
			require.NoError(t, err)
			require.Nil(t, lease)
			require.Equal(t, OpenAIWarpProxyURL(), proxy)
			require.Zero(t, acquired)
			require.Equal(t, 1, calls)
			require.NoError(t, resp.Body.Close())
		})
	}
}
