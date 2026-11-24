package service

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func replayPaddedInputFrame(paddingBytes int, input string) []byte {
	payload := make([]byte, 0, paddingBytes+len(input)+64)
	payload = append(payload, `{"metadata":{"padding":"`...)
	payload = append(payload, strings.Repeat("x", paddingBytes)...)
	payload = append(payload, `"},"input":`...)
	payload = append(payload, input...)
	return append(payload, '}')
}

func TestWSReplaySmallInputOwnsBackingWithoutPinningPaddedFrame(t *testing.T) {
	const input = `[{"type":"input_text","text":"one"},{"type":"input_text","text":"two"}]`
	frame := replayPaddedInputFrame(800<<10, input)
	items, exists, err := openAIWSExtractNormalizedInputSequence(frame)
	require.NoError(t, err)
	require.True(t, exists)
	require.Len(t, items, 2)
	for _, item := range items {
		offset := bytes.Index(frame, item)
		require.GreaterOrEqual(t, offset, 0)
		require.NotSame(t, &frame[offset], &item[0], "small replay input must not retain the full padded frame")
		require.Equal(t, len(item), cap(item), "an append cannot overwrite another item in the shared input region")
	}
	first := uintptr(unsafe.Pointer(&items[0][0]))
	second := uintptr(unsafe.Pointer(&items[1][0]))
	wantDistance := bytes.Index([]byte(input), items[1]) - bytes.Index([]byte(input), items[0])
	require.Equal(t, uintptr(wantDistance), second-first, "all items must share one owned input region")
	secondBefore := bytes.Clone(items[1])
	appended := append(items[0], bytes.Repeat([]byte{'z'}, len(items[1])+4)...)
	require.Equal(t, secondBefore, []byte(items[1]))
	runtime.KeepAlive(appended)
}

func TestWSReplaySmallInputRetainedHeapIsBounded(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var history []json.RawMessage
	for turn := 0; turn < 32; turn++ {
		frame := replayPaddedInputFrame(512<<10, `[{"type":"input_text","text":"small delta"}]`)
		items, exists, err := openAIWSExtractNormalizedInputSequence(frame)
		require.NoError(t, err)
		require.True(t, exists)
		history = combineOpenAIWSReplayItems(history, items)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := uint64(0)
	if after.HeapAlloc > before.HeapAlloc {
		retained = after.HeapAlloc - before.HeapAlloc
	}
	require.Len(t, history, 32)
	require.Less(t, retained, uint64(4<<20), "tiny history items must not retain 16 MiB of unrelated frame metadata")
	runtime.KeepAlive(history)
}

func TestWSReplayOwnedInputCopiesOnlyInputBytes(t *testing.T) {
	frame := replayPaddedInputFrame(800<<10, `[{"type":"input_text","text":"small delta"}]`)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var history []json.RawMessage
	for turn := 0; turn < 128; turn++ {
		items, _, err := openAIWSExtractNormalizedInputSequence(frame)
		require.NoError(t, err)
		history = combineOpenAIWSReplayItems(history, items)
	}
	runtime.ReadMemStats(&after)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(2<<20), "detaching input must not copy each entire frame")
	runtime.KeepAlive(history)
}

var benchmarkWSReplayRetentionSink []json.RawMessage

func BenchmarkWSReplayPaddedInput(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{{"170KiB", 170 << 10}, {"800KiB", 800 << 10}} {
		b.Run(tc.name, func(b *testing.B) {
			frame := replayPaddedInputFrame(tc.size, `[{"type":"input_text","text":"small delta"}]`)
			b.SetBytes(int64(len(frame)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				items, _, err := openAIWSExtractNormalizedInputSequence(frame)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkWSReplayRetentionSink = items
			}
		})
	}
}
