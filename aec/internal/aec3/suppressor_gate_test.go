package aec3

import "testing"

func unityGain() ([FFTLengthBy2Plus1]float32, float32) {
	var g [FFTLengthBy2Plus1]float32
	for k := range g {
		g[k] = 1
	}
	return g, 1
}

func halfGain() ([FFTLengthBy2Plus1]float32, float32) {
	var g [FFTLengthBy2Plus1]float32
	for k := range g {
		g[k] = 0.5
	}
	return g, 0.5
}

func TestSuppressorGate_DisabledIsANoOp(t *testing.T) {
	g := newSuppressorGate()
	g.ramp = 1 // as if a previous run had bypassed
	g.Update(nil, nil)
	if g.ramp != 0 {
		t.Fatalf("ramp = %v with gating disabled, want 0", g.ramp)
	}
	if got := g.State(); got != SuppressorGateEngaged {
		t.Fatalf("State() = %v with gating disabled, want SuppressorGateEngaged", got)
	}

	low, high := halfGain()
	g.Apply(&low, &high)
	if low[0] != 0.5 || high != 0.5 {
		t.Errorf("Apply modified the gains with gating disabled: low[0]=%v high=%v", low[0], high)
	}
}

func TestSuppressorGate_ApplyReachesUnity(t *testing.T) {
	g := newSuppressorGate()
	g.ramp = 1
	low, high := halfGain()
	g.Apply(&low, &high)
	for k, v := range low {
		if v != 1 {
			t.Fatalf("lowBandGain[%d] = %v at full bypass, want 1", k, v)
		}
	}
	if high != 1 {
		t.Errorf("highBandsGain = %v at full bypass, want 1", high)
	}

	// Halfway through the ramp the gains sit halfway to unity.
	g.ramp = 0.5
	low, high = halfGain()
	g.Apply(&low, &high)
	if low[0] != 0.75 || high != 0.75 {
		t.Errorf("mid-ramp gains = (%v, %v), want (0.75, 0.75)", low[0], high)
	}
}

func TestSuppressorGate_UnityGainsAreUntouched(t *testing.T) {
	g := newSuppressorGate()
	g.ramp = 1
	low, high := unityGain()
	g.Apply(&low, &high)
	for k, v := range low {
		if v != 1 {
			t.Fatalf("lowBandGain[%d] = %v, want the gate to leave unity gains alone", k, v)
		}
	}
	if high != 1 {
		t.Errorf("highBandsGain = %v, want 1", high)
	}
}

func TestDelayLocked(t *testing.T) {
	coarse := NewDelayEstimate(DelayEstimateQualityCoarse, 40)
	refined := NewDelayEstimate(DelayEstimateQualityRefined, 40)
	cases := []struct {
		name string
		in   *DelayEstimate
		want bool
	}{
		{"no estimate", nil, false},
		{"coarse estimate", &coarse, false},
		{"refined estimate", &refined, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := delayLocked(tc.in); got != tc.want {
				t.Errorf("delayLocked() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSuppressorGate_StateTracksRamp(t *testing.T) {
	g := newSuppressorGate()
	cases := []struct {
		ramp float32
		want SuppressorGateState
	}{
		{0, SuppressorGateEngaged},
		{0.5, SuppressorGateTransitioning},
		{1, SuppressorGateBypassed},
	}
	for _, tc := range cases {
		g.ramp = tc.ramp
		if got := g.State(); got != tc.want {
			t.Errorf("State() at ramp %v = %v, want %v", tc.ramp, got, tc.want)
		}
	}
}
