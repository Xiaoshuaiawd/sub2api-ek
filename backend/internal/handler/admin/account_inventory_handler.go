package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) InventoryStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	status, err := h.inventory.Status(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "库存状态读取失败")
		return
	}
	response.Success(c, status)
}

func (h *AccountHandler) InventorySaveConfig(c *gin.Context) {
	var input struct {
		TargetCount *int  `json:"target_count" binding:"required"`
		Enabled     *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.TargetCount == nil || input.Enabled == nil {
		response.BadRequest(c, "库存设置格式不正确")
		return
	}
	if *input.TargetCount < 0 || *input.TargetCount > service.OpenAIInventoryMaxRows {
		response.BadRequest(c, "目标数量必须在 0 到 10000 之间")
		return
	}
	if err := h.inventory.SaveConfig(c.Request.Context(), *input.TargetCount, *input.Enabled); err != nil {
		response.Error(c, http.StatusServiceUnavailable, "库存设置保存失败")
		return
	}
	h.InventoryStatus(c)
}

func (h *AccountHandler) InventoryImport(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.OpenAIInventoryMaxBytes+(1<<20))
	var input struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "库存文件格式或大小不正确")
		return
	}
	result, err := h.inventory.Import(c.Request.Context(), input.Content)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *AccountHandler) InventoryList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		response.BadRequest(c, "分页参数无效")
		return
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if err != nil {
		response.BadRequest(c, "分页参数无效")
		return
	}
	rows, total, err := h.inventory.List(c.Request.Context(), page, pageSize)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"items": rows, "total": total, "page": page, "page_size": pageSize})
}
