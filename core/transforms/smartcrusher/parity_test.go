package smartcrusher_test

import (
	"path/filepath"
	"testing"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/parity/comparators"
)

func TestSmartCrusherParity(t *testing.T) {
	parity.RunFixtures(t,
		filepath.Join("testdata", "fixtures"),
		comparators.SmartCrusher{},
		parity.WithPreprocessor(parity.StripCCRMarkers),
		parity.WithMinFixtures(17))
}

func TestSmartCrusherParityCentral(t *testing.T) {
	parity.RunFixtures(t,
		filepath.Join("..", "..", "testdata", "parity", "smart_crusher"),
		comparators.SmartCrusher{},
		parity.WithPreprocessor(parity.StripCCRMarkers),
		parity.WithMinFixtures(17))
}
