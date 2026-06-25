//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// Mode / bandwidth constants mirror the C enums (opus_defines.h,
// opus_decoder.c) for in-test use.
const (
	testMODE_SILK_ONLY = 1000
	testMODE_HYBRID    = 1001
	testMODE_CELT_ONLY = 1002

	testBW_NARROWBAND    = 1101
	testBW_MEDIUMBAND    = 1102
	testBW_WIDEBAND      = 1103
	testBW_SUPERWIDEBAND = 1104
	testBW_FULLBAND      = 1105
)

// TestParity_ThresholdTables verifies all threshold tables match the C
// file-scope values byte-for-byte.
func TestParity_ThresholdTables(t *testing.T) {
	gMV, gMM, gSV, gSM := nativeopus.ExportBandwidthThresholds()
	cMV, cMM, cSV, cSM := cGetBandwidthThresholds()
	if gMV != cMV {
		t.Errorf("mono_voice_bandwidth_thresholds: Go=%v C=%v", gMV, cMV)
	}
	if gMM != cMM {
		t.Errorf("mono_music_bandwidth_thresholds: Go=%v C=%v", gMM, cMM)
	}
	if gSV != cSV {
		t.Errorf("stereo_voice_bandwidth_thresholds: Go=%v C=%v", gSV, cSV)
	}
	if gSM != cSM {
		t.Errorf("stereo_music_bandwidth_thresholds: Go=%v C=%v", gSM, cSM)
	}
	gSVth, gSMth := nativeopus.ExportStereoThresholds()
	cSVth, cSMth := cGetStereoThresholds()
	if gSVth != cSVth || gSMth != cSMth {
		t.Errorf("stereo thresholds: Go=(%d,%d) C=(%d,%d)", gSVth, gSMth, cSVth, cSMth)
	}
	if nativeopus.ExportModeThresholds() != cGetModeThresholds() {
		t.Errorf("mode_thresholds mismatch: Go=%v C=%v",
			nativeopus.ExportModeThresholds(), cGetModeThresholds())
	}
	if nativeopus.ExportFecThresholds() != cGetFecThresholds() {
		t.Errorf("fec_thresholds mismatch: Go=%v C=%v",
			nativeopus.ExportFecThresholds(), cGetFecThresholds())
	}
}

// TestParity_DecideFec — random inputs in the realistic domain:
//
//   - useInBandFEC ∈ {0, 1}
//   - loss ∈ [0, 100]
//   - last_fec ∈ {0, 1}
//   - mode ∈ {SILK, HYBRID, CELT}
//   - bandwidth ∈ [NB, FB]
//   - rate ∈ [1000, 100000]
func TestParity_DecideFec(t *testing.T) {
	r := rand.New(rand.NewSource(0x9fd01))
	const trials = 2000
	bandwidths := []int{testBW_NARROWBAND, testBW_MEDIUMBAND, testBW_WIDEBAND, testBW_SUPERWIDEBAND, testBW_FULLBAND}
	modes := []int{testMODE_SILK_ONLY, testMODE_HYBRID, testMODE_CELT_ONLY}
	for i := 0; i < trials; i++ {
		useFec := r.Intn(2)
		loss := r.Intn(101)
		lastFec := r.Intn(2)
		mode := modes[r.Intn(len(modes))]
		bw := bandwidths[r.Intn(len(bandwidths))]
		rate := int32(r.Intn(100000) + 1000)

		gRet, gBw := nativeopus.ExportDecideFec(useFec, loss, lastFec, mode, bw, rate)
		cRet, cBw := cDecideFec(useFec, loss, lastFec, mode, bw, rate)
		if gRet != cRet || gBw != cBw {
			t.Fatalf("trial %d useFec=%d loss=%d lastFec=%d mode=%d bw=%d rate=%d: Go=(%d,%d) C=(%d,%d)",
				i, useFec, loss, lastFec, mode, bw, rate, gRet, gBw, cRet, cBw)
		}
	}
}

// TestParity_ComputeSilkRateForHybrid — random realistic inputs.
func TestParity_ComputeSilkRateForHybrid(t *testing.T) {
	r := rand.New(rand.NewSource(0x9fd02))
	const trials = 3000
	bandwidths := []int{testBW_WIDEBAND, testBW_SUPERWIDEBAND, testBW_FULLBAND}
	for i := 0; i < trials; i++ {
		rate := r.Intn(128000) + 2000
		bw := bandwidths[r.Intn(len(bandwidths))]
		frame20 := r.Intn(2)
		vbr := r.Intn(2)
		fec := r.Intn(2)
		channels := 1 + r.Intn(2)
		g := nativeopus.ExportComputeSilkRateForHybrid(rate, bw, frame20, vbr, fec, channels)
		c := cComputeSilkRateForHybrid(rate, bw, frame20, vbr, fec, channels)
		if g != c {
			t.Fatalf("trial %d rate=%d bw=%d frame20=%d vbr=%d fec=%d ch=%d: Go=%d C=%d",
				i, rate, bw, frame20, vbr, fec, channels, g, c)
		}
	}
}

// TestParity_ComputeEquivRate — random realistic inputs.
func TestParity_ComputeEquivRate(t *testing.T) {
	r := rand.New(rand.NewSource(0x9fd03))
	const trials = 3000
	modes := []int{testMODE_SILK_ONLY, testMODE_HYBRID, testMODE_CELT_ONLY, -1 /* unknown */}
	frameRates := []int{10, 20, 25, 50, 100, 200, 400}
	for i := 0; i < trials; i++ {
		bitrate := int32(r.Intn(500000) + 500)
		channels := 1 + r.Intn(2)
		fr := frameRates[r.Intn(len(frameRates))]
		vbr := r.Intn(2)
		mode := modes[r.Intn(len(modes))]
		complexity := r.Intn(11)
		loss := r.Intn(101)
		g := nativeopus.ExportComputeEquivRate(bitrate, channels, fr, vbr, mode, complexity, loss)
		c := cComputeEquivRate(bitrate, channels, fr, vbr, mode, complexity, loss)
		if g != c {
			t.Fatalf("trial %d br=%d ch=%d fr=%d vbr=%d mode=%d cx=%d loss=%d: Go=%d C=%d",
				i, bitrate, channels, fr, vbr, mode, complexity, loss, g, c)
		}
	}
}

// TestParity_ComputeFrameEnergy — random PCM (mono + stereo).
// Float input range is the realistic [-1, +1] decoded-sample domain.
func TestParity_ComputeFrameEnergy(t *testing.T) {
	r := rand.New(rand.NewSource(0x9fd04))
	const trials = 200
	frameSizes := []int{120, 240, 480, 960}
	for i := 0; i < trials; i++ {
		channels := 1 + r.Intn(2)
		frameSize := frameSizes[r.Intn(len(frameSizes))]
		n := frameSize * channels
		pcm := make([]float32, n)
		for j := 0; j < n; j++ {
			pcm[j] = (r.Float32()*2 - 1) * 0.8
		}
		g := nativeopus.ExportComputeFrameEnergy(pcm, frameSize, channels, 0)
		c := cComputeFrameEnergy(pcm, frameSize, channels, 0)
		if math.Float32bits(g) != math.Float32bits(c) {
			t.Fatalf("trial %d ch=%d fs=%d: Go=%g (0x%08x) C=%g (0x%08x)",
				i, channels, frameSize, g, math.Float32bits(g), c, math.Float32bits(c))
		}
	}
}

// TestParity_DecideDtxMode — table-driven across the activity/no-
// activity state-machine transitions.
func TestParity_DecideDtxMode(t *testing.T) {
	// Explore a range that crosses both thresholds:
	//   NB_SPEECH_FRAMES_BEFORE_DTX*20*2 = 10*40 = 400
	//   (NB_SPEECH_FRAMES_BEFORE_DTX + MAX_CONSECUTIVE_DTX)*20*2 = 30*40 = 1200
	type tc struct {
		activity, nbQ1, fsQ1 int
	}
	var cases []tc
	activities := []int{0, 1}
	nbs := []int{0, 100, 399, 400, 401, 800, 1200, 1201, 2000}
	fss := []int{10, 20, 40, 80, 100, 240}
	for _, a := range activities {
		for _, nb := range nbs {
			for _, fs := range fss {
				cases = append(cases, tc{a, nb, fs})
			}
		}
	}
	for i, tc := range cases {
		g, gNb := nativeopus.ExportDecideDtxMode(tc.activity, tc.nbQ1, tc.fsQ1)
		c, cNb := cDecideDtxMode(tc.activity, tc.nbQ1, tc.fsQ1)
		if g != c || gNb != cNb {
			t.Fatalf("case %d activity=%d nb=%d fs=%d: Go=(%d,%d) C=(%d,%d)",
				i, tc.activity, tc.nbQ1, tc.fsQ1, g, gNb, c, cNb)
		}
	}
}

// TestParity_ComputeRedundancyBytes — table-driven realistic inputs.
func TestParity_ComputeRedundancyBytes(t *testing.T) {
	type tc struct {
		maxDataBytes, bitrateBps int32
		frameRate, channels      int
	}
	var cases []tc
	maxBytesList := []int32{60, 120, 250, 500, 1000, 1500}
	bitrateList := []int32{6000, 12000, 24000, 48000, 96000, 192000}
	frameRateList := []int{25, 50, 100, 200, 400}
	for _, mb := range maxBytesList {
		for _, br := range bitrateList {
			for _, fr := range frameRateList {
				for _, ch := range []int{1, 2} {
					cases = append(cases, tc{mb, br, fr, ch})
				}
			}
		}
	}
	for i, tc := range cases {
		g := nativeopus.ExportComputeRedundancyBytes(tc.maxDataBytes, tc.bitrateBps, tc.frameRate, tc.channels)
		c := cComputeRedundancyBytes(tc.maxDataBytes, tc.bitrateBps, tc.frameRate, tc.channels)
		if g != c {
			t.Fatalf("case %d maxDb=%d br=%d fr=%d ch=%d: Go=%d C=%d",
				i, tc.maxDataBytes, tc.bitrateBps, tc.frameRate, tc.channels, g, c)
		}
	}
}
