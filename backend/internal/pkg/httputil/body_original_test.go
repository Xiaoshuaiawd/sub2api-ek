package httputil

import (
	"bytes"
	"compress/gzip"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOriginalRequestBodyPreservesCompressedBytes(t *testing.T) {
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	_, err := w.Write([]byte("{\"message\":\"hello\"}"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	req := httptest.NewRequest("POST", "/", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	decoded, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
	require.NoError(t, err)
	require.JSONEq(t, `{"message":"hello"}`, string(decoded))
	original, encoding, ok := OriginalRequestBody(req.Context())
	require.True(t, ok)
	require.Equal(t, "gzip", encoding)
	require.Equal(t, compressed.Bytes(), original)
	ReleaseOriginalRequestBody(req.Context())
	_, _, ok = OriginalRequestBody(req.Context())
	require.False(t, ok)
}

func TestOriginalRequestBodyPreservesLenientJSON(t *testing.T) {
	original := []byte("{\"message\":\"hello\nworld\"}")
	req := httptest.NewRequest("POST", "/", bytes.NewReader(original))
	decoded, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
	require.NoError(t, err)
	require.NotEqual(t, original, decoded)
	snapshot, _, ok := OriginalRequestBody(req.Context())
	require.True(t, ok)
	require.Equal(t, original, snapshot)
}
