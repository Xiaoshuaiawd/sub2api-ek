//go:build unit

package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestInventoryImportDisablesWebSocketTransportWithoutChangingManualImportDefaults(t *testing.T) {
	req := inventoryAddAccountImportRequest(`{"access_token":"token"}`, "stock@example.com")
	require.Equal(t, service.OpenAIWSIngressModeOff, req.Extra["openai_oauth_responses_websockets_v2_mode"])
	require.Equal(t, false, req.Extra["openai_oauth_responses_websockets_v2_enabled"])
	require.Equal(t, false, req.Extra["openai_oauth_ws_sse_acceleration"])

	manual := defaultAddAccountImportRequest(`{"access_token":"token"}`, "manual@example.com", "manual@example.com", nil)
	require.Equal(t, service.OpenAIWSIngressModeCtxPool, manual.Extra["openai_oauth_responses_websockets_v2_mode"])
	require.Equal(t, true, manual.Extra["openai_oauth_responses_websockets_v2_enabled"])
	require.Equal(t, true, manual.Extra["openai_oauth_ws_sse_acceleration"])
}
