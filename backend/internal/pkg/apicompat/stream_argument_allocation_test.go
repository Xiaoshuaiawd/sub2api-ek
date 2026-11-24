package apicompat

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func streamChatArgumentsForAllocation(arguments string, fragmentBytes int) *ChatCompletionsToResponsesStreamState {
	state := NewChatCompletionsToResponsesStreamState("fixture-model")
	index := 0
	chunk := &ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{{
		Index: &index, ID: "call_fixture", Type: "function", Function: ChatFunctionCall{Name: "exec"},
	}}}}}}
	for offset := 0; offset < len(arguments); offset += fragmentBytes {
		chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments = arguments[offset:min(offset+fragmentBytes, len(arguments))]
		ChatCompletionsChunkToResponsesEvents(chunk, state)
	}
	return state
}

func streamReadArgumentsForAllocation(arguments string, fragmentBytes int) *ResponsesEventToAnthropicState {
	state := NewResponsesEventToAnthropicState()
	state.MessageStartSent, state.ContentBlockOpen = true, true
	state.CurrentBlockType, state.CurrentToolName = "tool_use", "Read"
	state.OutputIndexToBlockIdx[0] = 0
	event := &ResponsesStreamEvent{Type: "response.function_call_arguments.delta", OutputIndex: 0}
	for offset := 0; offset < len(arguments); offset += fragmentBytes {
		event.Delta = arguments[offset:min(offset+fragmentBytes, len(arguments))]
		ResponsesEventToAnthropicEvents(event, state)
	}
	return state
}

func TestStreamChatArgumentsAllocationIsLinear(t *testing.T) {
	arguments := `{"source":"` + strings.Repeat("x", 256<<10) + `"}`
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	state := streamChatArgumentsForAllocation(arguments, 256)
	runtime.ReadMemStats(&after)
	require.Equal(t, arguments, state.ToolCalls[0].Function.Arguments)
	require.NoError(t, state.ValidateToolCallArguments())
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20), "small deltas must not repeatedly copy the accumulated tool arguments")
	runtime.KeepAlive(state)
}

func TestStreamReadArgumentsAllocationIsLinear(t *testing.T) {
	arguments := `{"file_path":"` + strings.Repeat("x", 256<<10) + `","pages":""}`
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	state := streamReadArgumentsForAllocation(arguments, 256)
	runtime.ReadMemStats(&after)
	require.Equal(t, arguments, state.CurrentToolArgs)
	require.True(t, state.CurrentToolHadDelta)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20), "Read argument buffering must allocate with output size rather than the sum of all prefixes")
	runtime.KeepAlive(state)
}

func TestStreamChatArgumentsPublicPrefixesRemainObservable(t *testing.T) {
	index := 0
	state := NewChatCompletionsToResponsesStreamState("fixture-model")
	chunk := &ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{{
		Index: &index, ID: "call_fixture", Type: "function", Function: ChatFunctionCall{Name: "exec"},
	}}}}}}
	var snapshots []string
	var expected string
	for _, delta := range []string{`{"source":"`, strings.Repeat("x", 8192), `"}`} {
		expected += delta
		chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments = delta
		ChatCompletionsChunkToResponsesEvents(chunk, state)
		require.Equal(t, expected, state.ToolCalls[0].Function.Arguments)
		snapshots = append(snapshots, state.ToolCalls[0].Function.Arguments)
	}
	require.Equal(t, `{"source":"`, snapshots[0], "published prefixes remain immutable after later appends")
	require.Equal(t, expected[:len(expected)-2], snapshots[1])
	require.NoError(t, state.ValidateToolCallArguments())
	for _, event := range FinalizeChatCompletionsResponsesStream(state) {
		if event.Type == "response.function_call_arguments.done" {
			require.Equal(t, expected, event.Arguments)
		}
	}
}

func TestStreamChatArgumentsRespectsExternallyUpdatedPublicState(t *testing.T) {
	state := streamChatArgumentsForAllocation(`{"source":"old"}`, 4)
	previous := state.ToolCalls[0].Function.Arguments
	state.ToolCalls[0].Function.Arguments = `{"external":"`
	index := 0
	chunk := &ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{{
		Index: &index, Function: ChatFunctionCall{Arguments: `replacement"}`},
	}}}}}}
	ChatCompletionsChunkToResponsesEvents(chunk, state)
	require.Equal(t, `{"external":"replacement"}`, state.ToolCalls[0].Function.Arguments)
	require.Equal(t, `{"source":"old"}`, previous)
}

func TestStreamReadArgumentsMatchesFirstCompleteJSONAtEverySplit(t *testing.T) {
	for _, arguments := range []string{
		`{"file_path":"/tmp/quoted\\\"}path-é","pages":""}`,
		`{"file_path":"x","nested":[{"k":"[}\\"}]}`, `{"file_path":"\u00e9"}`,
		`[{"k":"x"}]`, `"quoted\\\"é"`, `true`, `false`, `null`, `123.45e+6`,
		`{"invalid":]}`, `{"incomplete":"x`, `{"invalid":"\u0X00"}`, ` { } `,
	} {
		for split := 1; split < len(arguments); split++ {
			state := NewResponsesEventToAnthropicState()
			state.MessageStartSent, state.ContentBlockOpen = true, true
			state.CurrentBlockType, state.CurrentToolName = "tool_use", "Read"
			state.OutputIndexToBlockIdx[0] = 0
			expectedEmitted := false
			var prefix string
			for _, fragment := range []string{arguments[:split], arguments[split:]} {
				prefix += fragment
				events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.function_call_arguments.delta", Delta: fragment}, state)
				wantEmission := !expectedEmitted && json.Valid([]byte(prefix))
				require.Equal(t, wantEmission, len(events) > 0, "arguments=%q split=%d prefix=%q", arguments, split, prefix)
				require.Equal(t, prefix, state.CurrentToolArgs)
				if wantEmission {
					require.Len(t, events, 1)
					require.Equal(t, string(sanitizeAnthropicToolUseInput("Read", prefix)), events[0].Delta.PartialJSON)
					expectedEmitted = true
				}
			}
		}
	}
}

func TestStreamReadArgumentsResetsAcrossToolChanges(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	state.MessageStartSent = true
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.output_item.added", Item: &ResponsesOutput{Type: "function_call", CallID: "first", Name: "Read"}}, state)
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.function_call_arguments.delta", Delta: `{"file_path":"first`}, state)
	firstPrefix := state.CurrentToolArgs
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.output_item.added", OutputIndex: 1, Item: &ResponsesOutput{Type: "function_call", CallID: "second", Name: "Read"}}, state)
	require.Empty(t, state.CurrentToolArgs)
	arguments := `{"file_path":"second","pages":""}`
	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.function_call_arguments.delta", OutputIndex: 1, Delta: arguments}, state)
	require.Len(t, events, 1)
	require.JSONEq(t, `{"file_path":"second"}`, events[0].Delta.PartialJSON)
	require.Equal(t, arguments, state.CurrentToolArgs)
	require.Equal(t, `{"file_path":"first`, firstPrefix, "later tools cannot mutate an earlier public prefix")
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.function_call_arguments.done", OutputIndex: 1, Arguments: arguments}, state)
	require.Empty(t, state.CurrentToolArgs)
}

func TestStreamReadArgumentsRespectsExternallyUpdatedPublicState(t *testing.T) {
	state := streamReadArgumentsForAllocation(`{"file_path":"old`, 4)
	previous := state.CurrentToolArgs
	state.CurrentToolArgs = `{"file_path":"replacement`
	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.function_call_arguments.delta", Delta: `"}`}, state)
	require.Len(t, events, 1)
	require.Equal(t, `{"file_path":"replacement"}`, events[0].Delta.PartialJSON)
	require.Equal(t, `{"file_path":"old`, previous)
}

var benchmarkStreamArgumentSink string

func BenchmarkStreamChatArguments(b *testing.B) {
	for _, size := range []int{170 << 10, 800 << 10} {
		name := map[int]string{170 << 10: "170KiB", 800 << 10: "800KiB"}[size]
		b.Run(name, func(b *testing.B) {
			arguments := `{"source":"` + strings.Repeat("x", size) + `"}`
			b.ReportAllocs()
			b.SetBytes(int64(len(arguments)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := streamChatArgumentsForAllocation(arguments, 1024)
				benchmarkStreamArgumentSink = state.ToolCalls[0].Function.Arguments
			}
		})
	}
}

func BenchmarkStreamReadArguments(b *testing.B) {
	for _, size := range []int{170 << 10, 800 << 10} {
		name := map[int]string{170 << 10: "170KiB", 800 << 10: "800KiB"}[size]
		b.Run(name, func(b *testing.B) {
			arguments := `{"file_path":"` + strings.Repeat("x", size) + `","pages":""}`
			b.ReportAllocs()
			b.SetBytes(int64(len(arguments)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := streamReadArgumentsForAllocation(arguments, 1024)
				benchmarkStreamArgumentSink = state.CurrentToolArgs
			}
		})
	}
}
