//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeOpenAIWarpStatusOnlyReportsVerifiedWarpExit(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
		connected  bool
		ip         string
	}{
		{"connected", "ip=104.28.195.192\nwarp=on\n", 200, true, "104.28.195.192"},
		{"plus", "ip=2606:4700::1111\nwarp=plus\n", 200, true, "2606:4700::1111"},
		{"direct", "ip=203.0.113.1\nwarp=off\n", 200, false, ""},
		{"invalid ip", "ip=not-an-ip\nwarp=on\n", 200, false, ""},
		{"upstream error", "ip=104.28.195.192\nwarp=on\n", 503, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			status := probeOpenAIWarpStatus(t.Context(), srv.Client(), srv.URL)
			require.Equal(t, tc.connected, status.Connected)
			require.Equal(t, tc.ip, status.IP)
			require.False(t, status.CheckedAt.IsZero())
		})
	}
}
