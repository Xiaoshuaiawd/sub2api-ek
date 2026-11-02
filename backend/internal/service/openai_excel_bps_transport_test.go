package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type bpsTestLease struct{ releases, failures int }

func (l *bpsTestLease) Release()               { l.releases++ }
func (l *bpsTestLease) ReportFailure()         { l.failures++ }
func (l *bpsTestLease) ReportStreamFailure()   { l.failures++ }
func (l *bpsTestLease) ReportSuccess()         {}
func (l *bpsTestLease) ReportUpstreamFailure() { l.failures++ }

type bpsTestUpstream struct {
	httpUpstreamRecorder
	send func(*http.Request, string) (*http.Response, error)
}

func (u *bpsTestUpstream) Do(r *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	return u.send(r, proxy)
}

func TestExcelBPSWarpNeverRetriesTransportFailures(t *testing.T) {
	for _, scenario := range []string{"dial_failure", "connection_obtained", "header_started", "body_read", "replay_body_read", "cancelled", "deadline", "http_403", "response_and_error"} {
		t.Run(scenario, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			account := excelAccount()
			account.Extra["openai_excel_bps_mihomo"] = true
			account.Proxy = &Proxy{Protocol: "http", Host: "legacy-proxy", Port: 8080}
			acquired, calls := 0, 0
			acquire := func(context.Context, string, ...string) (string, excelBPSLease, error) {
				acquired++
				return "http://legacy-proxy:8080", &bpsTestLease{}, nil
			}
			upstream := &bpsTestUpstream{send: func(r *http.Request, proxy string) (*http.Response, error) {
				calls++
				require.Equal(t, OpenAIWarpProxyURL(), proxy)
				require.NotEmpty(t, proxy)
				trace := httptrace.ContextClientTrace(r.Context())
				trace.GetConn("bps.openai.com:443")
				switch scenario {
				case "connection_obtained":
					trace.GotConn(httptrace.GotConnInfo{})
				case "header_started":
					trace.WroteHeaderField("Authorization", []string{"secret"})
				case "body_read":
					_, _ = r.Body.Read(make([]byte, 1))
				case "replay_body_read":
					replay, err := r.GetBody()
					require.NoError(t, err)
					_, _ = replay.Read(make([]byte, 1))
					_ = replay.Close()
				case "cancelled":
					cancel()
					return nil, context.Canceled
				case "deadline":
					return nil, context.DeadlineExceeded
				case "http_403":
					return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("denied"))}, nil
				case "response_and_error":
					return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("failed"))}, io.EOF
				}
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNRESET}
			}}
			s := &OpenAIGatewayService{httpUpstream: upstream}
			resp, lease, proxy, err := s.doExcelBPSRequest(ctx, c, account, "session", []byte("body"), "token", "account", acquire)
			require.Equal(t, OpenAIWarpProxyURL(), proxy)
			require.Zero(t, acquired)
			require.Equal(t, 1, calls)
			require.Nil(t, lease)
			if scenario == "http_403" {
				require.NoError(t, err)
				require.Equal(t, 403, resp.StatusCode)
				require.NoError(t, resp.Body.Close())
			} else {
				require.Error(t, err)
				require.Nil(t, resp)
			}
		})
	}
}

func TestExcelBPSTransportDiagnosticsAreCredentialFree(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "req-43885")))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	account := excelAccount()
	account.Extra["openai_excel_bps_mihomo"] = true
	err := &url.Error{Op: "Post", URL: "https://secret-user:proxy-password@bps.openai.com/?token=secret-token", Err: fmt.Errorf("credential=raw-secret: %w", syscall.ECONNRESET)}
	recordExcelBPSTransportFailure(ctx, c, account, "private-session-key", "http://127.0.0.1:19007", err, "transport", 1, false)
	events, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	attempts, ok := events.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, attempts, 1)
	require.Equal(t, "connection_reset", attempts[0].Reason)
	require.Contains(t, attempts[0].Detail, "19007")
	require.Equal(t, 0, attempts[0].UpstreamStatusCode)
	require.Equal(t, "req-43885", logs.All()[0].ContextMap()["request_id"])
	all := fmt.Sprint(attempts[0], logs.All()[0].ContextMap())
	for _, secret := range []string{"secret-user", "proxy-password", "secret-token", "raw-secret", "private-session-key"} {
		require.NotContains(t, all, secret)
	}
	require.Zero(t, excelBPSLocalProxyPort("http://user:password@127.0.0.1:19000"))
}

func TestExcelBPSTransportErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{&net.DNSError{Err: "private host"}, "dns_error"}, {syscall.ECONNREFUSED, "connection_refused"},
		{io.ErrUnexpectedEOF, "unexpected_eof"}, {errors.New("net/http: TLS handshake timeout"), "tls_handshake_timeout"},
		{errors.New("http2: connection lost"), "http2_error"}, {nil, "stream_incomplete"},
	} {
		require.Equal(t, tc.want, transportdiag.Classify(tc.err))
	}
}

// Exercise actual net/http tracing without listening sockets or external traffic.
func TestExcelBPSWarpRealHTTPTransportNeverReplays(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprintf("sent_%t", sent), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			account := excelAccount()
			account.Extra["openai_excel_bps_mihomo"] = true
			acquired, calls := 0, 0
			acquire := func(context.Context, string, ...string) (string, excelBPSLease, error) {
				acquired++
				return "http://legacy-proxy:8080", &bpsTestLease{}, nil
			}
			observed := make(chan string, 1)
			transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				if !sent {
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
				}
				local, remote := net.Pipe()
				go func() {
					defer remote.Close()
					req, e := http.ReadRequest(bufio.NewReader(remote))
					if e != nil {
						observed <- "read error"
						return
					}
					b, _ := io.ReadAll(req.Body)
					_ = req.Body.Close()
					observed <- string(b)
				}()
				return local, nil
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			upstream := &bpsTestUpstream{send: func(r *http.Request, proxy string) (*http.Response, error) {
				calls++
				require.Equal(t, OpenAIWarpProxyURL(), proxy)
				r.URL.Scheme = "http" // in-memory peer only
				return client.Do(r)
			}}
			s := &OpenAIGatewayService{httpUpstream: upstream}
			resp, lease, proxy, err := s.doExcelBPSRequest(context.Background(), c, account, "session", []byte("must not duplicate"), "token", "account", acquire)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Nil(t, lease)
			require.Equal(t, OpenAIWarpProxyURL(), proxy)
			require.Equal(t, 1, calls)
			require.Zero(t, acquired)
			if sent {
				require.Equal(t, "must not duplicate", <-observed)
			}
		})
	}
}

func TestExcelBPSTransportAndStreamFailureAreServerErrors(t *testing.T) {
	for _, streamFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(streamFailure), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{err: io.EOF}
			if streamFailure {
				upstream.err = nil
				upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\",\"status\":\"in_progress\"}}\n\n"))}
			}
			svc := openAIClientToolsTestService(nil)
			svc.httpUpstream = upstream
			body := []byte("{\"model\":\"gpt-6-astra\",\"stream\":true,\"input\":\"test\"}")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			_, err := svc.Forward(context.Background(), c, excelAccount(), body)
			require.Error(t, err)
			require.Contains(t, rec.Body.String(), "server_error")
			if streamFailure {
				require.Equal(t, 200, rec.Code)
				require.Contains(t, rec.Body.String(), "basispoints_stream_incomplete")
				_, marked := GetOpsStreamError(c)
				require.True(t, marked, "HTTP 200 stream failure must enter Ops errors")
			} else {
				require.Equal(t, 502, rec.Code)
				require.Contains(t, rec.Body.String(), "basispoints_transport_error")
			}
		})
	}
}

func TestExcelBPSAttachmentPinnedLegacyLeaseIsNotUsedForOpenAI(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			lease := &bpsTestLease{}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			account := excelAccount()
			account.Extra["openai_excel_bps_mihomo"] = true
			calls := 0
			upstream := &bpsTestUpstream{send: func(r *http.Request, proxy string) (*http.Response, error) {
				calls++
				require.Equal(t, OpenAIWarpProxyURL(), proxy)
				if fail {
					return nil, errors.New("dial failed")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			}}
			svc := openAIClientToolsTestService(nil)
			svc.httpUpstream = upstream
			resp, borrowed, proxy, err := svc.doExcelBPSRequest(context.Background(), c, account, "scope", []byte("{}"), "token", "account", pinnedExcelBPSAcquire("http://127.0.0.1:19007", lease))
			if fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
			}
			require.Equal(t, OpenAIWarpProxyURL(), proxy)
			require.Nil(t, borrowed)
			require.Equal(t, 1, calls)
			require.Zero(t, lease.failures)
			require.Zero(t, lease.releases)
		})
	}
}
