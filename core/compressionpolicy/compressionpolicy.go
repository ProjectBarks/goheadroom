package compressionpolicy

import (
	"math"

	"github.com/projectbarks/goheadroom/core/authmode"
)

// Mode controls whether live-zone compression is attempted at all.
// The proxy sets this from the request-level or global config.
type Mode int

const (
	// Auto enables compression with provider-appropriate defaults.
	Auto Mode = iota
	// Off disables compression entirely; the body passes through unchanged.
	Off
)

// String returns a stable string tag for the mode.
func (m Mode) String() string {
	switch m {
	case Auto:
		return "auto"
	case Off:
		return "off"
	default:
		return "unknown"
	}
}

const (
	VolatileTokenThresholdPayg         uint32  = 128
	VolatileTokenThresholdSubscription uint32  = 32
	MaxLossyRatioPayg                  float32 = 0.45
	MaxLossyRatioSubscription          float32 = 0.25
	CacheWriteMultiplier               float32 = 1.25
	CacheReadMultiplier                float32 = 0.1
)

type CompressionPolicy struct {
	LiveZoneOnly           bool
	CacheAlignerEnabled    bool
	VolatileTokenThreshold uint32
	MaxLossyRatio          float32
	ToinReadOnly           bool
}

func ForMode(mode authmode.AuthMode) CompressionPolicy {
	switch mode {
	case authmode.Subscription:
		return CompressionPolicy{
			LiveZoneOnly:           true,
			CacheAlignerEnabled:    false,
			VolatileTokenThreshold: VolatileTokenThresholdSubscription,
			MaxLossyRatio:          MaxLossyRatioSubscription,
			ToinReadOnly:           true,
		}
	default: // Payg and OAuth are identical
		return CompressionPolicy{
			LiveZoneOnly:           false,
			CacheAlignerEnabled:    true,
			VolatileTokenThreshold: VolatileTokenThresholdPayg,
			MaxLossyRatio:          MaxLossyRatioPayg,
			ToinReadOnly:           false,
		}
	}
}

func (p CompressionPolicy) LiveZoneCompressionEnabled() bool {
	return true
}

func (p CompressionPolicy) NetMutationGain(deltaT, suffixTokens uint32, expectedReads, pAlive float32) float32 {
	w := CacheWriteMultiplier
	r := CacheReadMultiplier
	reads := float32(math.Max(float64(expectedReads), 0.0))
	if math.IsNaN(float64(expectedReads)) {
		reads = 0.0
	}
	alive := pAlive
	if math.IsNaN(float64(pAlive)) {
		alive = 1.0
	} else if alive < 0.0 {
		alive = 0.0
	} else if alive > 1.0 {
		alive = 1.0
	}
	return float32(deltaT)*(w+r*(reads-1.0)) - alive*(w-r)*float32(suffixTokens)
}

func (p CompressionPolicy) ShouldMutateDeep(deltaT, suffixTokens uint32, expectedReads, pAlive float32) bool {
	return p.NetMutationGain(deltaT, suffixTokens, expectedReads, pAlive) > 0.0
}

func (p CompressionPolicy) BreakEvenReads(deltaT, suffixTokens uint32) float32 {
	if deltaT == 0 {
		return 0.0
	}
	w := CacheWriteMultiplier
	r := CacheReadMultiplier
	return ((w - r) / r) * (float32(suffixTokens)/float32(deltaT) - 1.0)
}
