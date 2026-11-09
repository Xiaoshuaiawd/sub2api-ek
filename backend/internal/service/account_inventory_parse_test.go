//go:build unit

package service

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOpenAIInventoryLines(t *testing.T) {
	rows, err := ParseOpenAIInventoryLines(" A@Example.com----p@ss----JBSWY3DPEHPK3PXP\r\nb@example.com----second----MFA2\n")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "a@example.com", rows[0].Email)
	require.Equal(t, "p@ss", rows[0].Password)
	require.Equal(t, "JBSWY3DPEHPK3PXP", rows[0].MFASecret)
}

func TestParseOpenAIInventoryLinesRejectsWholeInvalidBatch(t *testing.T) {
	for _, raw := range []string{
		"",
		"a@example.com----pass----MFA\nA@example.com----pass2----MFA2",
		"a@example.com----pass----MFA\nbad-row",
		"a@example.com----pass----",
		"a@example.com----pass----MFA----extra",
	} {
		_, err := ParseOpenAIInventoryLines(raw)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "pass")
		require.NotContains(t, err.Error(), "MFA")
	}
}

func TestParseOpenAIInventoryLinesAcceptsTenThousand(t *testing.T) {
	var input strings.Builder
	for i := 0; i < 10000; i++ {
		input.WriteString("user" + strconv.Itoa(i) + "@example.com----password----MFA\n")
	}
	rows, err := ParseOpenAIInventoryLines(input.String())
	require.NoError(t, err)
	require.Len(t, rows, 10000)
	_, err = ParseOpenAIInventoryLines(input.String() + "overflow@example.com----pass----MFA\n")
	require.Error(t, err)
}
