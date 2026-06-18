// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of libSBRenc/src/resampler.cpp — the 2:1 time-domain downsampler the
// non-PS HE-AAC v1 encoder runs on the AAC-LC core path (SBRENC_DS_TIME): a
// Chebyshev-II biquad-cascade (second-order-sections) low-pass evaluated per
// input pair, keeping every ratio-th output sample.
//   - FDKaacEnc_InitDownsampler (resampler.cpp:292-329)
//   - AdvanceFilter             (resampler.cpp:341-417)
//   - FDKaacEnc_Downsample      (resampler.cpp:426-444)
//
// INT_PCM == int16 (SAMPLE_BITS==16), FIXP_BQS == FIXP_DBL (int32 states),
// MAXNR_SECTIONS==15. The float coefficient tables are retained verbatim and
// quantised with BQC(x)==Fl2fxconstSGL(x/2) / the gain with Fl2fxconstDBL at
// init, exactly as the C ROM is materialised. Pure fixed-point — EXACT parity.
//
// HE-AAC v1 only: PS uses the QMF downsampler (SBRENC_DS_QMF), not this path.
package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

const (
	resamplerMaxnrSections = 15 // MAXNR_SECTIONS (resampler.h:116)
	biquadScale            = 12 // BIQUAD_SCALE (resampler.cpp:348)
	biquadCoefStep         = 4  // BIQUAD_COEFSTEP
	// SOS coefficient layout indices (resampler.cpp:117-120): B1,B2,A1,A2.
	bqB1 = 0
	bqB2 = 1
	bqA1 = 2
	bqA2 = 3
)

// filterParam is the 1:1 port of struct FILTER_PARAM (resampler.cpp:124-131).
type filterParam struct {
	coeffaFloat []float64 // raw SOS rows (BQC applies x/2 + Fl2fxconstSGL at init)
	gFloat      float64   // raw gain (Fl2fxconstDBL at init)
	gAdjust     int32     // the "- 0x8000" tweak some gains carry (else 0)
	wc          int       // normalized passband * 1000
	noCoeffs    int       // number of biquad sections
	delay       int       // delay in samples at input rate
}

// The five Chebyshev-II coefficient sets (resampler.cpp:141-276), verbatim
// floats. BQC(x) == Fl2fxconstSGL(x/2) is applied at init.
var (
	sos48f = []float64{
		1.98941075681938, 0.999999996890811, 0.863264527201963, 0.189553799960663,
		1.90733804822445, 1.00000001736189, 0.836321575841691, 0.203505809266564,
		1.75616665495325, 0.999999946079721, 0.784699225121588, 0.230471265506986,
		1.55727745512726, 1.00000011737815, 0.712515423588351, 0.268752723900498,
		1.33407591943643, 0.999999795953228, 0.625059117330989, 0.316194685288965,
		1.10689898412458, 1.00000035057114, 0.52803514366398, 0.370517843224669,
		0.89060371078454, 0.999999343962822, 0.426920462165257, 0.429608200207746,
		0.694438261209433, 1.0000008629792, 0.326530699561716, 0.491714450654174,
		0.523237800935322, 1.00000101349782, 0.230829556274851, 0.555559034843281,
		0.378631165929563, 0.99998986482665, 0.142906422036095, 0.620338874442411,
		0.260786911308437, 1.00003261460178, 0.0651008576256505, 0.685759923926262,
		0.168409429188098, 0.999933049695828, -0.000790067789975562, 0.751905896602325,
		0.100724533818628, 1.00009472669872, -0.0533772830257041, 0.81930744384525,
		0.0561434357867363, 0.999911636304276, -0.0913550299236405, 0.88883625875915,
		0.0341680678662057, 1.00003667508676, -0.113405185536697, 0.961756638268446,
	}
	sos45f = []float64{
		1.982962601444, 1.00000000007504, 0.646113303737836, 0.10851149979981,
		1.85334094281111, 0.999999999677192, 0.612073220102006, 0.130022141698044,
		1.62541051415425, 1.00000000080398, 0.547879702855959, 0.171165825133192,
		1.34554656923247, 0.9999999980169, 0.460373914508491, 0.228677463376354,
		1.05656568503116, 1.00000000569363, 0.357891894038287, 0.298676843912185,
		0.787967587877312, 0.999999984415017, 0.248826893211877, 0.377441803512978,
		0.555480971120497, 1.00000003583307, 0.140614263345315, 0.461979302213679,
		0.364986207070964, 0.999999932084303, 0.0392669446074516, 0.55033451180825,
		0.216827267631558, 1.00000010534682, -0.0506232228865103, 0.641691581560946,
		0.108951672277119, 0.999999871167516, -0.125584840183225, 0.736367748771803,
		0.0387988607229035, 1.00000011205574, -0.182814849097974, 0.835802108714964,
		0.0042866175809225, 0.999999954830813, -0.21965740617151, 0.942623047782363,
	}
	sos41f = []float64{
		1.96193625292, 0.999999999999964, 0.169266178786789, 0.0128823300475907,
		1.68913437662092, 1.00000000000053, 0.124751503206552, 0.0537472273950989,
		1.27274692366017, 0.999999999995674, 0.0433108625178357, 0.131015753236317,
		0.85214175088395, 1.00000000001813, -0.0625658152550408, 0.237763778993806,
		0.503841579939009, 0.999999999953223, -0.179176128722865, 0.367475236424474,
		0.249990711986162, 1.00000000007952, -0.294425165824676, 0.516594857170212,
		0.087971668680286, 0.999999999915528, -0.398956566777928, 0.686417767801123,
		0.00965373325350294, 1.00000000003744, -0.48579173764817, 0.884931534239068,
	}
	sos35f = []float64{
		1.93299325235762, 0.999999999999985, -0.140733187246596, 0.0124139497836062,
		1.4890416764109, 1.00000000000011, -0.198215402588504, 0.0746730616584138,
		0.918450161309795, 0.999999999999619, -0.30133912791941, 0.192276468839529,
		0.454877024246818, 1.00000000000086, -0.432337328809815, 0.356852933642815,
		0.158017147118507, 0.999999999998876, -0.574817494249777, 0.566380436970833,
		0.0171834649478749, 1.00000000000055, -0.718581178041165, 0.83367484487889,
	}
	sos25f = []float64{
		1.85334094301225, 1.0, -0.702127214212663, 0.132452403998767,
		1.056565682167, 0.999999999999997, -0.789503667880785, 0.236328693569128,
		0.364986307455489, 0.999999999999996, -0.955191189843375, 0.442966457936379,
		0.0387985751642125, 1.0, -1.19817786088084, 0.770493895456328,
	}
)

// filterParamSet mirrors filter_paramSet[] (resampler.cpp:279-280), in
// descending Wc order. The gAdjust column carries the "- (FIXP_DBL)0x8000" tweak
// on g48/g45 (resampler.cpp:174/205); the other gains have no adjustment.
var filterParamSet = []filterParam{
	{sos48f, 0.002712866530047, -0x8000, 480, 15, 4},
	{sos45f, 0.00242743980909524, -0x8000, 450, 12, 4},
	{sos41f, 0.00155956951169248, 0, 410, 8, 5},
	{sos35f, 0.00162580994125131, 0, 350, 6, 4},
	{sos25f, 0.000945182835294559, 0, 250, 4, 5},
}

// LpFilter is the 1:1 port of struct LP_FILTER (resampler.h:120-127).
type LpFilter struct {
	states   [resamplerMaxnrSections + 1][2]int32 // FIXP_BQS states[][2]
	ptr      int
	coeffa   []int16 // BQC-quantised SOS rows
	noCoeffs int
	gain     int32
	wc       int
}

// Downsampler is the 1:1 port of struct DOWNSAMPLER (resampler.h:130-136).
type Downsampler struct {
	downFilter LpFilter
	ratio      int
	delay      int
	pending    int
}

// Delay returns the downsampler's filter delay in samples at the input rate
// (DOWNSAMPLER.delay), used by the encoder's delay-balancing.
func (d *Downsampler) Delay() int { return d.delay }

// InitDownsampler is the 1:1 port of FDKaacEnc_InitDownsampler
// (resampler.cpp:292-329): selects the coefficient set for the requested cutoff
// and clears the delay lines. Returns 1.
func InitDownsampler(d *Downsampler, wc, ratio int) int {
	for i := range d.downFilter.states {
		d.downFilter.states[i][0] = 0
		d.downFilter.states[i][1] = 0
	}
	d.downFilter.ptr = 0

	currentSet := &filterParamSet[0]
	for i := 1; i < len(filterParamSet); i++ {
		if filterParamSet[i].wc <= wc {
			break
		}
		currentSet = &filterParamSet[i]
	}

	// Materialise the BQC-quantised coefficients (BQC(x) == Fl2fxconstSGL(x/2)).
	coeff := make([]int16, len(currentSet.coeffaFloat))
	for i, v := range currentSet.coeffaFloat {
		coeff[i] = nativeaac.Fl2fxconstSGL(v / 2)
	}
	d.downFilter.coeffa = coeff
	d.downFilter.gain = nativeaac.Fl2fxconstDBL(currentSet.gFloat) + currentSet.gAdjust
	d.downFilter.noCoeffs = currentSet.noCoeffs
	d.delay = currentSet.delay
	d.downFilter.wc = currentSet.wc

	d.ratio = ratio
	d.pending = ratio - 1
	return 1
}

// advanceFilter is the 1:1 port of AdvanceFilter (resampler.cpp:341-417): runs
// the biquad cascade over downRatio input samples and returns one int16 output.
func advanceFilter(downFilter *LpFilter, pInput []int16, base, downRatio int) int16 {
	var y int32

	for n := 0; n < downRatio; n++ {
		states := &downFilter.states
		coeff := downFilter.coeffa
		s1 := downFilter.ptr
		s2 := s1 ^ 1

		// SAMPLE_BITS == 16: input = (FIXP_DBL)pInput[n] << (32-16-12).
		input := int32(pInput[base+n]) << (dfractBits - 16 - biquadScale)

		state1 := states[0][s1]
		state2 := states[0][s2]

		ci := 0
		for i := 0; i < downFilter.noCoeffs; i++ {
			state1b := states[i+1][s1]
			state2b := states[i+1][s2]

			state0 := input + nativeaac.FMultDS(state1, coeff[ci+bqB1]) + nativeaac.FMultDS(state2, coeff[ci+bqB2])
			y = state0 - nativeaac.FMultDS(state1b, coeff[ci+bqA1]) - nativeaac.FMultDS(state2b, coeff[ci+bqA2])

			states[i+1][s2] = y << 1
			states[i][s2] = input << 1

			input = y
			state1 = state1b
			state2 = state2b
			ci += biquadCoefStep
		}
		downFilter.ptr ^= 1
	}

	// Apply global gain.
	y = nativeaac.FMultDD(y, downFilter.gain)

	// SAMPLE_BITS == 16 output: SATURATE_RIGHT_SHIFT(y + round, 4, 16).
	const shift = dfractBits - 16 - biquadScale // 4
	round := int32(1) << (shift - 1)
	v := (y + round) >> shift
	if v > 32767 {
		v = 32767
	} else if v < -32768 {
		v = -32768
	}
	return int16(v)
}

// Downsample is the 1:1 port of FDKaacEnc_Downsample (resampler.cpp:426-444):
// downsamples numInSamples by d.ratio into outSamples, returning numOutSamples.
func Downsample(d *Downsampler, inSamples []int16, numInSamples int, outSamples []int16) (numOutSamples int) {
	o := 0
	for i := 0; i < numInSamples; i += d.ratio {
		outSamples[o] = advanceFilter(&d.downFilter, inSamples, i, d.ratio)
		o++
	}
	return numInSamples / d.ratio
}
