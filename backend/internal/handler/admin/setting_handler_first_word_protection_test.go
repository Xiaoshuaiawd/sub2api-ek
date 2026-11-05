package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestFirstWordProtectionSettingRoundTripAndOmission(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	read := func() bool {
		t.Helper()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
		h.GetSettings(c)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return gjson.Get(rec.Body.String(), "data.openai_first_word_protection_enabled").Bool()
	}
	require.False(t, read(), "the current config disables first-word protection")
	rec := doUpdateSettings(t, h, map[string]any{"openai_first_word_protection_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyOpenAIFirstWordProtectionEnabled])
	require.True(t, read())
	rec = doUpdateSettings(t, h, map[string]any{"site_name": "preserve first word"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, repo.lastUpdates, service.SettingKeyOpenAIFirstWordProtectionEnabled,
		"an unrelated settings save must not overwrite a concurrent switch change")
	require.True(t, read())
	rec = doUpdateSettings(t, h, map[string]any{"openai_first_word_protection_enabled": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, read())
}
