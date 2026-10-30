//go:build unit

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type scopedIntelligenceSettingsRepo struct {
	service.SettingRepository
	mu     sync.Mutex
	value  string
	exists bool
}

func (r *scopedIntelligenceSettingsRepo) GetValue(_ context.Context, _ string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.exists {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}

func (r *scopedIntelligenceSettingsRepo) CompareAndSetIntelligenceRules(_ context.Context, _, previous, next string, previousExists bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exists != previousExists || (r.exists && r.value != previous) {
		return false, nil
	}
	r.value, r.exists = next, true
	return true, nil
}

func TestScopedAdminIntelligenceRulesRegisteredRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		permissions []string
		read, write bool
	}{
		{"no permission", nil, false, false},
		{"read only", []string{"intelligence_rules.read"}, true, false},
		{"write only", []string{"intelligence_rules.write"}, false, true},
		{"read and write", []string{"intelligence_rules.read", "intelligence_rules.write"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &scopedIntelligenceSettingsRepo{}
			settings := service.NewSettingService(repo, nil)
			settings.SetScopedAdminKeyRepository(&scopedRouteKeys{})
			key, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{
				Name: "intelligence rules test", Enabled: true, Policy: adminaccess.Policy{GlobalPermissions: tc.permissions},
			}, 9)
			require.NoError(t, err)
			users := service.NewUserService(&scopedRouteUsers{}, nil, nil, nil)
			auth := middleware.NewAdminAuthMiddleware(nil, users, settings, nil)
			accounts := adminhandler.NewAccountHandler(&scopedRouteAccounts{t: t}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			RegisterAdminRoutes(router.Group("/api/v1"), &handler.Handlers{Admin: &handler.AdminHandlers{
				Account: accounts, Setting: adminhandler.NewSettingHandler(settings, nil, nil, nil, nil, nil, nil),
			}}, auth, middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
			call := func(method, path string, body []byte, authKey string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if authKey != "" {
					req.Header.Set("x-api-key", authKey)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			path := "/api/v1/admin/settings/intelligence-rules"
			require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, path, nil, "").Code)
			read := call(http.MethodGet, path, nil, key)
			readStatus := http.StatusForbidden
			if tc.read {
				readStatus = http.StatusOK
			}
			require.Equal(t, readStatus, read.Code, read.Body.String())
			current, err := settings.GetIntelligenceRules(context.Background())
			require.NoError(t, err)
			if tc.read {
				require.Contains(t, read.Body.String(), service.IntelligenceRulesRevision(current))
			}
			body, err := json.Marshal(service.IntelligenceRulePatch{Revision: service.IntelligenceRulesRevision(current), Operation: "add_rule", Rule: &service.IntelligenceRule{Name: "rule", Type: "contains", Pattern: "sample", Enabled: true}})
			require.NoError(t, err)
			write := call(http.MethodPatch, path, body, key)
			writeStatus := http.StatusForbidden
			if tc.write {
				writeStatus = http.StatusOK
			}
			require.Equal(t, writeStatus, write.Code, write.Body.String())
			if tc.write {
				updated, err := settings.GetIntelligenceRules(context.Background())
				require.NoError(t, err)
				require.Len(t, updated.Rules, 1)
				require.Equal(t, "sample", updated.Rules[0].Pattern)
				if tc.read {
					loaded := call(http.MethodGet, path, nil, key)
					require.Equal(t, http.StatusOK, loaded.Code)
					require.Contains(t, loaded.Body.String(), `"pattern":"sample"`)
				}
			}
			for _, denied := range []struct{ method, path string }{
				{http.MethodPut, path},
				{http.MethodGet, "/api/v1/admin/settings"},
				{http.MethodGet, "/api/v1/admin/usage/intelligence-contexts"},
			} {
				w := call(denied.method, denied.path, []byte(`{}`), key)
				require.Equal(t, http.StatusForbidden, w.Code, "%s %s: %s", denied.method, denied.path, w.Body.String())
			}
		})
	}
}
