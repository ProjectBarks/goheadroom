package compressionpolicy

import (
	"math"
	"testing"

	"github.com/uber/goheadroom/authmode"
)

func TestPaygIsAggressive(t *testing.T) {
	p := ForMode(authmode.Payg)
	if p.LiveZoneOnly {
		t.Error("PAYG can touch outside live zone")
	}
	if !p.CacheAlignerEnabled {
		t.Error("PAYG runs cache aligner")
	}
	if !p.LiveZoneCompressionEnabled() {
		t.Error("PAYG should have live zone compression enabled")
	}
}

func TestPaygTuningFieldsAggressive(t *testing.T) {
	p := ForMode(authmode.Payg)
	if p.VolatileTokenThreshold != 128 {
		t.Errorf("volatile threshold = %d, want 128", p.VolatileTokenThreshold)
	}
	if math.Abs(float64(p.MaxLossyRatio-0.45)) > 1e-6 {
		t.Errorf("max_lossy_ratio = %f, want 0.45", p.MaxLossyRatio)
	}
	if p.ToinReadOnly {
		t.Error("PAYG should not be TOIN read-only")
	}
}

func TestOAuthMatchesPaygToday(t *testing.T) {
	oauth := ForMode(authmode.OAuth)
	payg := ForMode(authmode.Payg)
	if oauth != payg {
		t.Errorf("OAuth should match PAYG: oauth=%+v payg=%+v", oauth, payg)
	}
}

func TestSubscriptionDisablesCacheAligner(t *testing.T) {
	p := ForMode(authmode.Subscription)
	if !p.LiveZoneOnly {
		t.Error("Subscription should be live-zone-only")
	}
	if p.CacheAlignerEnabled {
		t.Error("Subscription MUST skip cache aligner")
	}
	if !p.LiveZoneCompressionEnabled() {
		t.Error("Subscription still gets live-zone compression")
	}
}

func TestSubscriptionTuningFieldsConservative(t *testing.T) {
	p := ForMode(authmode.Subscription)
	if p.VolatileTokenThreshold != 32 {
		t.Errorf("volatile threshold = %d, want 32", p.VolatileTokenThreshold)
	}
	if math.Abs(float64(p.MaxLossyRatio-0.25)) > 1e-6 {
		t.Errorf("max_lossy_ratio = %f, want 0.25", p.MaxLossyRatio)
	}
	if !p.ToinReadOnly {
		t.Error("Subscription MUST be TOIN read-only")
	}
}

func TestMaxLossyRatioInUnitInterval(t *testing.T) {
	for _, mode := range []authmode.AuthMode{authmode.Payg, authmode.OAuth, authmode.Subscription} {
		r := ForMode(mode).MaxLossyRatio
		if r < 0.0 || r > 1.0 {
			t.Errorf("max_lossy_ratio for %v = %f, outside [0.0, 1.0]", mode, r)
		}
	}
}

func TestNetGainSmallShaveDeepSuffixIsLoss(t *testing.T) {
	// 2000*(1.25 + 0.1*9) - 1.0*1.15*50000 = 4300 - 57500 = -53200
	p := ForMode(authmode.Payg)
	gain := p.NetMutationGain(2000, 50000, 10.0, 1.0)
	if math.Abs(float64(gain-(-53200.0))) >= 1.0 {
		t.Errorf("gain = %f, want -53200", gain)
	}
	if p.ShouldMutateDeep(2000, 50000, 10.0, 1.0) {
		t.Error("should not mutate: small shave under deep suffix")
	}
}

func TestNetGainBigShaveShallowSuffixIsWin(t *testing.T) {
	// 50000*(1.25 + 0.1*2) - 1.0*1.15*10000 = 72500 - 11500 = 61000
	p := ForMode(authmode.Payg)
	gain := p.NetMutationGain(50000, 10000, 3.0, 1.0)
	if math.Abs(float64(gain-61000.0)) >= 1.0 {
		t.Errorf("gain = %f, want 61000", gain)
	}
	if !p.ShouldMutateDeep(50000, 10000, 3.0, 1.0) {
		t.Error("should mutate: big shave under shallow suffix")
	}
}

func TestNetGainLiveZoneEditAlwaysProfitable(t *testing.T) {
	p := ForMode(authmode.Subscription)
	if !p.ShouldMutateDeep(1, 0, 0.0, 1.0) {
		t.Error("live zone edit (S=0) should always be profitable")
	}
	if !p.ShouldMutateDeep(2000, 0, 0.0, 1.0) {
		t.Error("live zone edit (S=0) should always be profitable")
	}
}

func TestNetGainColdCacheIgnoresSuffix(t *testing.T) {
	p := ForMode(authmode.Payg)
	if !p.ShouldMutateDeep(2000, 50000, 0.0, 0.0) {
		t.Error("cold cache (P_alive=0) should always be profitable")
	}
}

func TestNetGainClampsOutOfRangeInputs(t *testing.T) {
	p := ForMode(authmode.Payg)
	clamped := p.NetMutationGain(2000, 50000, -5.0, 7.0)
	reference := p.NetMutationGain(2000, 50000, 0.0, 1.0)
	if math.Abs(float64(clamped-reference)) > 1e-4 {
		t.Errorf("clamped=%f reference=%f", clamped, reference)
	}
}

func TestNetGainGuardsNanInputs(t *testing.T) {
	p := ForMode(authmode.Payg)
	guarded := p.NetMutationGain(2000, 50000, float32(math.NaN()), float32(math.NaN()))
	if math.IsNaN(float64(guarded)) || math.IsInf(float64(guarded), 0) {
		t.Error("NaN inputs should produce finite output")
	}
	reference := p.NetMutationGain(2000, 50000, 0.0, 1.0)
	if math.Abs(float64(guarded-reference)) > 1e-4 {
		t.Errorf("guarded=%f reference=%f", guarded, reference)
	}
}

func TestBreakEvenReadsMatchesResearchAnchor(t *testing.T) {
	// R = 11.5*(S/dT - 1): 2K/50K -> 11.5*24 = 276
	p := ForMode(authmode.Payg)
	r := p.BreakEvenReads(2000, 50000)
	if math.Abs(float64(r-276.0)) >= 0.5 {
		t.Errorf("break-even = %f, want ~276", r)
	}
	if p.BreakEvenReads(50000, 10000) >= 0.0 {
		t.Error("50K shave / 10K suffix should be profitable from first read (negative break-even)")
	}
	if p.BreakEvenReads(0, 10000) != 0.0 {
		t.Error("delta_t=0 should return 0")
	}
}
