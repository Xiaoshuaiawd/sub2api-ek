package admin

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/base64"
	"github.com/klauspost/compress/zstd"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceContextEncodedBody(t *testing.T) {
	text := []byte(`{"text":"智商测试"}`)
	for _, encoding := range []string{"gzip", "deflate", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			var buf bytes.Buffer
			switch encoding {
			case "gzip":
				w := gzip.NewWriter(&buf)
				_, err := w.Write(text)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			case "deflate":
				w := zlib.NewWriter(&buf)
				_, err := w.Write(text)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			case "zstd":
				w, err := zstd.NewWriter(&buf)
				require.NoError(t, err)
				_, err = w.Write(text)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			}
			raw := buf.Bytes()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			intelligenceContextDetail(c, &service.IntelligenceRequestContext{ID: "ctx", Body: raw, ContentEncoding: encoding}, nil)
			require.Contains(t, w.Body.String(), `智商测试`)
			require.Contains(t, w.Body.String(), base64.StdEncoding.EncodeToString(raw))
			require.Contains(t, w.Body.String(), `"content_encoding":"`+encoding+`"`)
		})
	}
}

func TestIntelligenceContextAmbiguousUsage(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	intelligenceContextDetail(c, nil, service.ErrIntelligenceContextAmbiguous)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "multiple contexts")
}

func TestIntelligenceContextInvalidEncodingPreservesOriginal(t *testing.T) {
	raw := []byte("invalid-gzip")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	intelligenceContextDetail(c, &service.IntelligenceRequestContext{Body: raw, ContentEncoding: "gzip"}, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"request_body":null`)
	require.Contains(t, w.Body.String(), base64.StdEncoding.EncodeToString(raw))
}

type fakeIntelligenceContextRepo struct {
	service.IntelligenceContextRepository
	item *service.IntelligenceRequestContext
}

func (r *fakeIntelligenceContextRepo) FindByUsage(context.Context, int64) (*service.IntelligenceRequestContext, error) {
	return r.item, nil
}
func (r *fakeIntelligenceContextRepo) Available(_ context.Context, ids []int64) (map[int64]bool, error) {
	return map[int64]bool{ids[0]: true}, nil
}

type fakeIntelligenceUsageRepo struct{ service.UsageLogRepository }

func (fakeIntelligenceUsageRepo) ListWithFilters(context.Context, pagination.PaginationParams, usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return []service.UsageLog{{ID: 3}}, &pagination.PaginationResult{Total: 1}, nil
}

func TestIntelligenceContextUsageListOnlyFullJWTAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := service.NewSettingService(nil, nil)
	settings.SetIntelligenceContextRepository(&fakeIntelligenceContextRepo{})
	h := ProvideUsageHandler(service.NewUsageService(fakeIntelligenceUsageRepo{}, nil, nil, nil), nil, nil, nil, settings)
	for _, scoped := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/usage", nil)
		c.Set("auth_method", "jwt")
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		if scoped {
			c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{}))
		}
		h.List(c)
		require.Equal(t, 200, w.Code)
		if scoped {
			require.NotContains(t, w.Body.String(), "intelligence_context_available")
		} else {
			require.Contains(t, w.Body.String(), `"intelligence_context_available":true`)
		}
	}
}

func TestIntelligenceContextAdminAccessAndBinaryBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte{0, 255, 2}
	settings := service.NewSettingService(nil, nil)
	settings.SetIntelligenceContextRepository(&fakeIntelligenceContextRepo{item: &service.IntelligenceRequestContext{ID: "00000000-0000-4000-8000-000000000001", Body: body, MatchedRules: []service.IntelligenceRuleMatch{}, CreatedAt: time.Now()}})
	h := ProvideUsageHandler(nil, nil, nil, nil, settings)
	for _, tc := range []struct {
		name, method, role string
		scoped             bool
		want               int
	}{
		{"jwt admin", "jwt", service.RoleAdmin, false, 200},
		{"observer", "jwt", service.RoleObserver, false, 403},
		{"legacy admin key", "admin_api_key", service.RoleAdmin, false, 403},
		{"scoped admin", "jwt", service.RoleAdmin, true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/admin/usage/3/intelligence-context", nil)
			c.Params = gin.Params{{Key: "id", Value: "3"}}
			c.Set("auth_method", tc.method)
			c.Set(string(middleware.ContextKeyUserRole), tc.role)
			if tc.scoped {
				c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{}))
			}
			h.IntelligenceContext(c)
			require.Equal(t, tc.want, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if tc.want == 200 {
				require.Contains(t, w.Body.String(), base64.StdEncoding.EncodeToString(body))
				require.Contains(t, w.Body.String(), `"request_body":null`)
			} else {
				require.NotContains(t, w.Body.String(), base64.StdEncoding.EncodeToString(body))
			}
		})
	}
}
