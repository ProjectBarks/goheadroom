package transforms

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/projectbarks/goheadroom/core/transforms/adaptivesizer"
	"github.com/projectbarks/goheadroom/core/transforms/anchorselector"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
)

func TestAllPackagesImport(t *testing.T) {
	// Verify all sub-packages compile and key types are accessible
	_ = contentdetector.DetectContentType("test")
	_ = adaptivesizer.Simhash("test")
	_ = anchorselector.DefaultAnchorConfig()

	// Verify transforms package types
	_ = ToolPairIndices([]Message{})
	_, _ = ProtectTags("test")
	_ = IsDiff("test")
	_ = Detect("test", "")

	assert.True(t, true, "all packages compile and types accessible")
}
