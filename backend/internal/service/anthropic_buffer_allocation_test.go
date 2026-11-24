package service

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func appendToolJSONFragments(arguments string, fragmentBytes int) json.RawMessage {
	input := json.RawMessage(`{}`)
	for offset := 0; offset < len(arguments); offset += fragmentBytes {
		input = appendRawJSON(input, arguments[offset:min(offset+fragmentBytes, len(arguments))])
	}
	return input
}

func TestAppendRawJSONFragmentAllocationIsLinear(t *testing.T) {
	arguments := `{"source":"` + strings.Repeat("x", 256<<10) + `"}`
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	input := appendToolJSONFragments(arguments, 256)
	runtime.ReadMemStats(&after)
	require.Equal(t, arguments, string(input))
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20), "tool JSON buffering must not decode and copy every accumulated prefix")
	runtime.KeepAlive(input)
}

func TestAppendRawJSONKeepsObservablePrefixesAndPlaceholderSemantics(t *testing.T) {
	for _, placeholder := range []string{"", `{}`, " \n{ \t\r } \n"} {
		require.Equal(t, `{"x":1}`, string(appendRawJSON(json.RawMessage(placeholder), `{"x":1}`)))
	}
	for _, nonPlaceholder := range []string{`{"x":1}`, "\v{}", "{\v}"} {
		require.Equal(t, nonPlaceholder+`{"y":2}`, string(appendRawJSON(json.RawMessage(nonPlaceholder), `{"y":2}`)))
	}
	input := make(json.RawMessage, len(`{"x":"`), 4096)
	copy(input, `{"x":"`)
	prefix := input[:len(input)]
	prefixBefore := bytes.Clone(prefix)
	combined := appendRawJSON(input, `extended"}`)
	require.Equal(t, prefixBefore, []byte(prefix), "existing holders keep their visible immutable prefix")
	require.Equal(t, `{"x":"extended"}`, string(combined))
}

var benchmarkAnthropicToolJSONSink json.RawMessage

func BenchmarkAppendRawJSONFragments(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{{"170KiB", 170 << 10}, {"800KiB", 800 << 10}} {
		b.Run(tc.name, func(b *testing.B) {
			arguments := `{"source":"` + strings.Repeat("x", tc.size) + `"}`
			b.SetBytes(int64(len(arguments)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkAnthropicToolJSONSink = appendToolJSONFragments(arguments, 1024)
			}
		})
	}
}
