//go:build cgo

package benchcmp

/*
#include "config.h"
#include "bands.h"
#include "entenc.h"
#include "entdec.h"

static short c_bitexact_cos(short x) { return bitexact_cos((opus_int16)x); }
static int c_bitexact_log2tan(int isin, int icos) { return bitexact_log2tan(isin, icos); }
static unsigned c_celt_lcg_rand(unsigned seed) { return celt_lcg_rand(seed); }

static int c_hysteresis_decision(float val, const float *thr, const float *hys,
    int N, int prev) {
    return hysteresis_decision(val, thr, hys, N, prev);
}

static void c_compute_band_energies(const CELTMode *m, const float *X, float *bandE,
    int end, int C, int LM) {
    compute_band_energies(m, X, bandE, end, C, LM, 0);
}

static void c_normalise_bands(const CELTMode *m, const float *freq, float *X,
    const float *bandE, int end, int C, int M) {
    normalise_bands(m, freq, X, bandE, end, C, M);
}

static void c_denormalise_bands(const CELTMode *m, const float *X, float *freq,
    const float *bandLogE, int start, int end, int M, int downsample, int silence) {
    denormalise_bands(m, X, freq, bandLogE, start, end, M, downsample, silence);
}

static void c_anti_collapse(const CELTMode *m, float *X, unsigned char *cm,
    int LM, int C, int size, int start, int end,
    const float *logE, const float *prev1, const float *prev2,
    const int *pulses, unsigned seed, int encode) {
    anti_collapse(m, X, cm, LM, C, size, start, end, logE, prev1, prev2, pulses, seed, encode, 0);
}

static void c_intensity_stereo(const CELTMode *m, float *X, const float *Y,
    const float *bandE, int bandID, int N) {
    // shim needs to access the file-static; call the bands.c wrapper by
    // reinvoking through a local copy of its logic: libopus exposes
    // intensity_stereo as file-static in bands.c, so this test goes
    // through quant_band_stereo. We replicate the body here for a
    // direct test.
    int i = bandID;
    float left = bandE[i];
    float right = bandE[i + m->nbEBands];
    float norm = 1e-15f + sqrtf(1e-15f + left*left + right*right);
    float a1 = (left*32768.f)/norm;
    float a2 = (right*32768.f)/norm;
    for (int j = 0; j < N; j++) X[j] = (a1 * X[j] + a2 * Y[j]) * (1.f/32768);
    (void)m; // silence unused warning when shim evolves
}

// Replicate C stereo_split / stereo_merge / haar1 / deinterleave /
// interleave / compute_qn since they are static in bands.c.
static void c_stereo_split(float *X, float *Y, int N) {
    for (int j = 0; j < N; j++) {
        float l = 0.70710678f * X[j];
        float r = 0.70710678f * Y[j];
        X[j] = l + r;
        Y[j] = r - l;
    }
}

static void c_haar1(float *X, int N0, int stride) { haar1(X, N0, stride); }

static void c_compute_channel_weights(float Ex, float Ey, float w[2]) {
    float minE = Ex < Ey ? Ex : Ey;
    Ex += minE/3.f;
    Ey += minE/3.f;
    w[0] = Ex;
    w[1] = Ey;
}

static int c_spreading_decision(const CELTMode *m, const float *X, int *average,
    int last_decision, int *hf_average, int *tapset_decision, int update_hf,
    int end, int C, int M, const int *spread_weight) {
    return spreading_decision(m, X, average, last_decision, hf_average,
        tapset_decision, update_hf, end, C, M, spread_weight);
}

static void c_quant_all_bands(int encode, const CELTMode *m, int start, int end,
    float *X, float *Y, unsigned char *collapse_masks,
    const float *bandE, int *pulses, int shortBlocks, int spread,
    int dual_stereo, int intensity, int *tf_res, opus_int32 total_bits,
    opus_int32 balance, ec_ctx *ec, int LM, int codedBands, unsigned *seed,
    int complexity, int disable_inv) {
    quant_all_bands(encode, m, start, end, X, Y, collapse_masks, bandE,
        pulses, shortBlocks, spread, dual_stereo, intensity, tf_res,
        total_bits, balance, ec, LM, codedBands, seed, complexity, 0, disable_inv);
}

#include <math.h>
*/
import "C"
import "unsafe"

func cBitexactCos(x int16) int16 { return int16(C.c_bitexact_cos(C.short(x))) }
func cBitexactLog2Tan(isin, icos int) int {
	return int(C.c_bitexact_log2tan(C.int(isin), C.int(icos)))
}
func cCeltLcgRand(seed uint32) uint32 { return uint32(C.c_celt_lcg_rand(C.uint(seed))) }

func cHysteresisDecision(val float32, thresholds, hysteresis []float32, N, prev int) int {
	return int(C.c_hysteresis_decision(C.float(val),
		(*C.float)(unsafe.Pointer(&thresholds[0])),
		(*C.float)(unsafe.Pointer(&hysteresis[0])),
		C.int(N), C.int(prev)))
}

func cComputeBandEnergies(m cMode, X, bandE []float32, end, C_, LM int) {
	C.c_compute_band_energies(m.p,
		(*C.float)(unsafe.Pointer(&X[0])),
		(*C.float)(unsafe.Pointer(&bandE[0])),
		C.int(end), C.int(C_), C.int(LM))
}

func cNormaliseBands(m cMode, freq, X, bandE []float32, end, C_, M int) {
	C.c_normalise_bands(m.p,
		(*C.float)(unsafe.Pointer(&freq[0])),
		(*C.float)(unsafe.Pointer(&X[0])),
		(*C.float)(unsafe.Pointer(&bandE[0])),
		C.int(end), C.int(C_), C.int(M))
}

func cDenormaliseBands(m cMode, X, freq, bandLogE []float32, start, end, M, downsample, silence int) {
	C.c_denormalise_bands(m.p,
		(*C.float)(unsafe.Pointer(&X[0])),
		(*C.float)(unsafe.Pointer(&freq[0])),
		(*C.float)(unsafe.Pointer(&bandLogE[0])),
		C.int(start), C.int(end), C.int(M), C.int(downsample), C.int(silence))
}

func cAntiCollapse(m cMode, X []float32, cm []byte, LM, C_, size, start, end int,
	logE, prev1, prev2 []float32, pulses []int, seed uint32, encode int) {
	cPulses := make([]C.int, len(pulses))
	for i, v := range pulses {
		cPulses[i] = C.int(v)
	}
	C.c_anti_collapse(m.p,
		(*C.float)(unsafe.Pointer(&X[0])),
		(*C.uchar)(unsafe.Pointer(&cm[0])),
		C.int(LM), C.int(C_), C.int(size), C.int(start), C.int(end),
		(*C.float)(unsafe.Pointer(&logE[0])),
		(*C.float)(unsafe.Pointer(&prev1[0])),
		(*C.float)(unsafe.Pointer(&prev2[0])),
		(*C.int)(unsafe.Pointer(&cPulses[0])),
		C.uint(seed), C.int(encode))
}

func cStereoSplit(X, Y []float32, N int) {
	C.c_stereo_split((*C.float)(unsafe.Pointer(&X[0])),
		(*C.float)(unsafe.Pointer(&Y[0])), C.int(N))
}

func cHaar1(X []float32, N0, stride int) {
	C.c_haar1((*C.float)(unsafe.Pointer(&X[0])), C.int(N0), C.int(stride))
}

func cSpreadingDecision(m cMode, X []float32, average *int, lastDecision int,
	hfAverage, tapsetDecision *int, updateHf, end, C_, M int, spreadWeight []int) int {
	cAvg := C.int(*average)
	cHf := C.int(*hfAverage)
	cTap := C.int(*tapsetDecision)
	cSw := make([]C.int, len(spreadWeight))
	for i, v := range spreadWeight {
		cSw[i] = C.int(v)
	}
	r := int(C.c_spreading_decision(m.p,
		(*C.float)(unsafe.Pointer(&X[0])),
		&cAvg, C.int(lastDecision), &cHf, &cTap, C.int(updateHf),
		C.int(end), C.int(C_), C.int(M),
		(*C.int)(unsafe.Pointer(&cSw[0]))))
	*average = int(cAvg)
	*hfAverage = int(cHf)
	*tapsetDecision = int(cTap)
	return r
}

func cQuantAllBands(encode int, m cMode, start, end int, X, Y []float32,
	collapseMasks []byte, bandE []float32, pulses []int,
	shortBlocks, spread, dualStereo, intensity int, tfRes []int,
	totalBits, balance int32, ec cEc, LM, codedBands int, seed *uint32,
	complexity, disableInv int) {
	cPulses := make([]C.int, len(pulses))
	for i, v := range pulses {
		cPulses[i] = C.int(v)
	}
	cTf := make([]C.int, len(tfRes))
	for i, v := range tfRes {
		cTf[i] = C.int(v)
	}
	var yPtr *C.float
	if Y != nil {
		yPtr = (*C.float)(unsafe.Pointer(&Y[0]))
	}
	s := C.uint(*seed)
	C.c_quant_all_bands(C.int(encode), m.p, C.int(start), C.int(end),
		(*C.float)(unsafe.Pointer(&X[0])), yPtr,
		(*C.uchar)(unsafe.Pointer(&collapseMasks[0])),
		(*C.float)(unsafe.Pointer(&bandE[0])),
		(*C.int)(unsafe.Pointer(&cPulses[0])),
		C.int(shortBlocks), C.int(spread), C.int(dualStereo), C.int(intensity),
		(*C.int)(unsafe.Pointer(&cTf[0])),
		C.opus_int32(totalBits), C.opus_int32(balance),
		ec.p, C.int(LM), C.int(codedBands), &s,
		C.int(complexity), C.int(disableInv))
	*seed = uint32(s)
	for i := range pulses {
		pulses[i] = int(cPulses[i])
	}
}
