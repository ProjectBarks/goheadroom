package parity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripCCRMarkers_SingleMarker(t *testing.T) {
	input := `[{"a":1},{"_ccr_dropped":"<<ccr:foo>>"}]`
	expected := `[{"a":1}]`
	require.Equal(t, expected, string(StripCCRMarkers([]byte(input))))
}

func TestStripCCRMarkers_NoMarkers(t *testing.T) {
	input := `[{"a":1},{"b":2}]`
	require.Equal(t, input, string(StripCCRMarkers([]byte(input))))
}

func TestStripCCRMarkers_MultipleMarkers(t *testing.T) {
	input := `[{"a":1},{"_ccr_dropped":"<<ccr:foo>>"},{"b":2},{"_ccr_dropped":"<<ccr:bar>>"}]`
	expected := `[{"a":1},{"b":2}]`
	require.Equal(t, expected, string(StripCCRMarkers([]byte(input))))
}

func TestStripCCRMarkers_EmptyInput(t *testing.T) {
	require.Equal(t, "", string(StripCCRMarkers([]byte(""))))
}
