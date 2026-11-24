package service

import (
	"context"

	"github.com/gin-gonic/gin"
)

// A turn captures these values synchronously after MapRequestModel. Pool and
// relay callbacks use the immutable copy, never a concurrently changed Request.
// The wire marker is serialization evidence, not fresh account authorization.
type openAIWSModelMappingSnapshot struct {
	source    string
	channel   string
	accountID int64
	wireModel string
}

func captureOpenAIWSModelMapping(c *gin.Context, account *Account, wireModel string) openAIWSModelMappingSnapshot {
	mapping := openAIWSModelMappingSnapshot{wireModel: wireModel}
	if account != nil {
		mapping.accountID = account.ID
	}
	if c != nil && c.Request != nil {
		mapping.source, mapping.channel, _ = AccountModelMappingModels(c.Request.Context())
	}
	return mapping
}

func (m openAIWSModelMappingSnapshot) withContext(ctx context.Context) context.Context {
	// An absent current basis must also clear any inherited handshake basis.
	// Context cancellation, node ownership and leases still belong to ctx.
	ctx = WithAccountModelMapping(ctx, m.source, m.channel)
	return WithResolvedAccountModel(ctx, m.accountID, m.wireModel)
}
