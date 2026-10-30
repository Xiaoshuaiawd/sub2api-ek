package admin

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func isIntelligenceContextAdmin(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.GetString("auth_method") == "jwt" && c.GetString(string(middleware.ContextKeyUserRole)) == service.RoleAdmin && adminaccess.FromContext(c.Request.Context()) == nil
}

func requireIntelligenceContextAdmin(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if !isIntelligenceContextAdmin(c) {
		response.Error(c, http.StatusForbidden, "administrator login required")
		return false
	}
	return true
}

func intelligenceContextDetail(c *gin.Context, value *service.IntelligenceRequestContext, err error) {
	if errors.Is(err, service.ErrIntelligenceContextAmbiguous) {
		response.Error(c, http.StatusConflict, "multiple contexts match this usage; view them from the intelligence context list")
		return
	}
	if errors.Is(err, service.ErrIntelligenceContextNotFound) {
		response.Error(c, http.StatusNotFound, "intelligence context not found")
		return
	}
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "intelligence context unavailable")
		return
	}
	if value == nil {
		response.Error(c, http.StatusNotFound, "intelligence context not found")
		return
	}
	var text any
	decoded := value.Body
	if value.ContentEncoding != "" {
		req := &http.Request{Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(value.Body)), ContentLength: int64(len(value.Body))}
		req.Header.Set("Content-Encoding", value.ContentEncoding)
		decoded, err = pkghttputil.ReadRequestBodyWithPreallocLimit(req, 64<<20)
		if err != nil {
			decoded = nil
		}
	}
	if decoded != nil && utf8.Valid(decoded) {
		text = string(decoded)
	}
	response.Success(c, gin.H{"id": value.ID, "request_id": value.UsageRequestID, "usage_log_id": value.UsageLogID, "content_type": value.ContentType, "content_encoding": value.ContentEncoding, "request_body": text, "request_body_base64": base64.StdEncoding.EncodeToString(value.Body), "matched_rules": value.MatchedRules, "created_at": value.CreatedAt})
}

func (h *UsageHandler) IntelligenceContext(c *gin.Context) {
	if !requireIntelligenceContextAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid usage id")
		return
	}
	value, err := h.intelligenceSettings.IntelligenceContextByUsage(c.Request.Context(), id)
	intelligenceContextDetail(c, value, err)
}

func (h *UsageHandler) IntelligenceContexts(c *gin.Context) {
	if !requireIntelligenceContextAdmin(c) {
		return
	}
	page, size := response.ParsePagination(c)
	values, total, err := h.intelligenceSettings.IntelligenceContexts(c.Request.Context(), page, size)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "intelligence context unavailable")
		return
	}
	items := make([]gin.H, 0, len(values))
	for _, value := range values {
		requestID := ""
		if value.UsageRequestID != nil {
			requestID = *value.UsageRequestID
		}
		items = append(items, gin.H{"id": value.ID, "request_id": requestID, "usage_log_id": value.UsageLogID, "created_at": value.CreatedAt, "matched_rules": value.MatchedRules, "user_id": value.UserID, "api_key_id": value.APIKeyID, "content_encoding": value.ContentEncoding})
	}
	response.Paginated(c, items, total, page, size)
}

func (h *UsageHandler) IntelligenceContextDetail(c *gin.Context) {
	if !requireIntelligenceContextAdmin(c) {
		return
	}
	id := c.Param("context_id")
	if _, err := uuid.Parse(id); err != nil {
		response.BadRequest(c, "invalid intelligence context id")
		return
	}
	value, err := h.intelligenceSettings.IntelligenceContextByID(c.Request.Context(), id)
	intelligenceContextDetail(c, value, err)
}
