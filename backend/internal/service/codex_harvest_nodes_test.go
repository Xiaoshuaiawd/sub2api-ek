package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/mihomo"
	"github.com/stretchr/testify/require"
)

func TestHarvestNodeRanking(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	nodes := []mihomo.HarvestNode{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "gone-but-no-longer-listed"}}
	records := []CodexHarvestNodeRecord{{NodeID: "b", Successes: 1, LastSuccess: &past}, {NodeID: "c", Successes: 5, LastSuccess: &past, CooldownUntil: &future}}
	ranked := rankCodexHarvestNodes(nodes[:3], records, nil, 0, now)
	require.Equal(t, []mihomo.HarvestNode{{ID: "b"}, {ID: "a"}}, ranked)
	ranked = rankCodexHarvestNodes(nodes[:3], records, map[string]bool{"b": true}, 0, now)
	require.Equal(t, []mihomo.HarvestNode{{ID: "a"}}, ranked)
	require.Empty(t, rankCodexHarvestNodes(nil, records, nil, 1, now))
	require.Equal(t, "b", rankCodexHarvestNodes(nodes[:3], nil, nil, 1, now)[0].ID)
}

func TestPinnedHarvestNodeHonorsTriedAndMissingNodes(t *testing.T) {
	nodes := []mihomo.HarvestNode{{ID: "a"}, {ID: "b"}}
	node, ok := pinnedHarvestNode(nodes, &openAICodexTicket{HarvestNodeID: "b"}, nil)
	require.True(t, ok)
	require.Equal(t, "b", node.ID)
	_, ok = pinnedHarvestNode(nodes, &openAICodexTicket{HarvestNodeID: "b"}, map[string]bool{"b": true})
	require.False(t, ok)
	_, ok = pinnedHarvestNode(nodes, &openAICodexTicket{HarvestNodeID: "missing"}, nil)
	require.False(t, ok)
	_, ok = pinnedHarvestNode(nodes, nil, nil)
	require.False(t, ok)
}
