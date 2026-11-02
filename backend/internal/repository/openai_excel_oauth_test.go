package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// useOAuthTestWARP forwards through a controlled local HTTP proxy, preserving
// request paths, headers and bodies while verifying the mandatory proxy path.
func useOAuthTestWARP(t *testing.T, upstream *httptest.Server) *atomic.Int32 {
	t.Helper()
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	forward := httputil.NewSingleHostReverseProxy(target)
	forward.Transport = &http.Transport{Proxy: nil}
	calls := &atomic.Int32{}
	proxy := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("OPENAI_WARP_PROXY_URL", proxy.URL)
	return calls
}

func TestExcelOAuthUsesOfficialTokenContract(t *testing.T) {
	for _, grant := range []string{"refresh_token", "authorization_code"} {
		t.Run(grant, func(t *testing.T) {
			var got *http.Request
			server := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				got = r
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"new-at","refresh_token":"new-rt","expires_in":3600}`))
			}))
			defer server.Close()
			proxyCalls := useOAuthTestWARP(t, server)
			client := &openaiOAuthService{tokenURL: server.URL + "/oauth/token?existing=kept"}
			var result *openai.TokenResponse
			var err error
			if grant == "refresh_token" {
				result, err = client.RefreshTokenWithClientID(context.Background(), "excel-rt", "", openai.ExcelClientID)
			} else {
				result, err = client.ExchangeCode(context.Background(), "code", "verifier", "https://auth.openai.com/basispoints/deviceauth/callback", "", openai.ExcelClientID)
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, proxyCalls.Load(), "token exchange must use WARP")
			require.Equal(t, "new-at", result.AccessToken)
			require.NotNil(t, got)
			require.Equal(t, "true", got.URL.Query().Get("unified"))
			require.Equal(t, "kept", got.URL.Query().Get("existing"))
			require.Equal(t, openai.ExcelClientID, got.PostForm.Get("client_id"))
			require.Equal(t, grant, got.PostForm.Get("grant_type"))
			require.False(t, got.PostForm.Has("scope"))
			require.Empty(t, got.Header.Get("originator"))
		})
	}
}

func TestExcelOAuthRejectsWithoutEchoingCredentialBody(t *testing.T) {
	server := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"private-credential"}`))
	}))
	defer server.Close()
	proxyCalls := useOAuthTestWARP(t, server)
	client := &openaiOAuthService{tokenURL: server.URL}
	_, err := client.RefreshTokenWithClientID(context.Background(), "excel-rt", "", openai.ExcelClientID)
	require.ErrorContains(t, err, "status 401")
	require.NotContains(t, err.Error(), "private-credential")
	_, err = client.ExchangeCode(context.Background(), "code", "verifier", openai.ExcelRedirectURI, "", openai.ExcelClientID)
	require.ErrorContains(t, err, "status 401")
	require.NotContains(t, err.Error(), "private-credential")
	require.EqualValues(t, 2, proxyCalls.Load(), "both token requests must use WARP")
}
