//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestNSQDelDecSoA_vs_Scalar_Integration runs both
// silk_noise_shape_quantizer_del_dec (scalar) and
// silk_noise_shape_quantizer_del_dec_soa (SoA-fused, 4-lane SIMD-aware)
// on identical inputs via the silk_NSQ_del_dec_c entry point and asserts
// every mutated output is bit-exact.
//
// The SoA path only activates when nStatesDelayedDecision ==
// MAX_DEL_DEC_STATES (= 4). All trials fix ns=4 so the SoA path is the
// one exercised by ExportTestSilkNSQDelDec.
//
// Parity coverage:
//   - pulses[]
//   - NSQ.xq[0:frame_length] (as reported via SilkNSQIO.XQ)
//   - NSQ.sLTP_shp_Q14[0:frame_length]
//   - NSQ.sLPC_Q14[0:NSQ_LPC_BUF_LENGTH]
//   - NSQ.sAR2_Q14[0:MAX_SHAPE_LPC_ORDER]
//   - NSQ scalars: sLF_AR_shp_Q14, sDiff_shp_Q14, lagPrev, sLTP_buf_idx,
//     sLTP_shp_buf_idx, rand_seed, prev_gain_Q16, rewhite_flag.
//   - psIndices.Seed (via the second return value of ExportTestSilkNSQDelDec).
//
// The test uses the same randomNSQInputs helper the scalar-vs-C parity
// test consumes. That helper has been validated by TestParity_SilkNSQDelDec
// to produce inputs that the scalar del-dec encoder accepts without
// numerical divergence, so any remaining divergence between the scalar
// and SoA Go paths is attributable to the SoA wiring.
func TestNSQDelDecSoA_vs_Scalar_Integration(t *testing.T) {
	if !nativeopus.ExportTestNSQSIMDAvailable() {
		t.Skip("NSQ SoA SIMD path not linked (arm64 / !opus_strict / !opus_nosimd required) — dispatch would fall through to scalar, leaving nothing to compare.")
	}

	r := rand.New(rand.NewSource(7_777_777))
	fsSet := []int{8, 12, 16}
	nbSet := []int{2, 4}
	sigSet := []int{0, 2}
	// Only ns=4 so the dispatch picks the SoA path.
	const ns = 4

	trials := 0
	for _, fs := range fsSet {
		for _, nb := range nbSet {
			for _, sig := range sigSet {
				for trial := 0; trial < 4; trial++ {
					in := randomNSQInputs(r, fs, nb, sig, sig == 2)

					// Run via ExportTestSilkNSQDelDec. Because the
					// dispatch in silk_NSQ_del_dec_c routes on
					// (nsqSIMDAvailable && ns == 4) to the SoA variant,
					// and this build tag combination has SIMD available
					// (asserted via the t.Skip guard above), the first
					// invocation runs the SoA path.
					gotSoA, soaSeed := nativeopus.ExportTestSilkNSQDelDec(in, ns)

					// For the scalar reference, force the dispatch to
					// the non-SoA path by setting ns=3 — but that
					// changes the shape of the computation (three
					// delayed-decision lanes instead of four). To get a
					// true 4-lane scalar reference we need a per-call
					// scalar-forcing knob. We add one below via a new
					// export.
					gotScalar, scalarSeed := nativeopus.ExportTestSilkNSQDelDecForceScalar(in, ns)

					if !eqInt8Slice(gotSoA.Pulses, gotScalar.Pulses) {
						t.Fatalf("SoA vs Scalar pulses mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
					}
					if !eqInt16Slice(gotSoA.XQ, gotScalar.XQ) {
						t.Fatalf("SoA vs Scalar xq mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
					}
					if !eqInt32Slice(gotSoA.SLTP_shp_Q14, gotScalar.SLTP_shp_Q14) {
						t.Fatalf("SoA vs Scalar sLTP_shp mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
					}
					if !eqInt32Slice(gotSoA.SLPC_Q14, gotScalar.SLPC_Q14) {
						t.Fatalf("SoA vs Scalar sLPC mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
					}
					if !eqInt32Slice(gotSoA.SAR2_Q14, gotScalar.SAR2_Q14) {
						t.Fatalf("SoA vs Scalar sAR2 mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
					}
					if gotSoA.SLF_AR_shp_Q14 != gotScalar.SLF_AR_shp_Q14 ||
						gotSoA.SDiff_shp_Q14 != gotScalar.SDiff_shp_Q14 ||
						gotSoA.LagPrev != gotScalar.LagPrev ||
						gotSoA.SLTP_buf_idx != gotScalar.SLTP_buf_idx ||
						gotSoA.SLTP_shp_buf_idx != gotScalar.SLTP_shp_buf_idx ||
						gotSoA.RandSeed != gotScalar.RandSeed ||
						gotSoA.PrevGainQ16 != gotScalar.PrevGainQ16 ||
						gotSoA.RewhiteFlag != gotScalar.RewhiteFlag ||
						soaSeed != scalarSeed {
						t.Fatalf("SoA vs Scalar scalars mismatch fs=%d nb=%d sig=%d ns=%d trial=%d\n SoA=%+v/seed=%d\n Sca=%+v/seed=%d",
							fs, nb, sig, ns, trial, gotSoA, soaSeed, gotScalar, scalarSeed)
					}
					trials++
				}
			}
		}
	}

	if trials == 0 {
		t.Fatalf("no trials executed")
	}
	t.Logf("SoA vs Scalar bit-exact across %d trials (ns=%d)", trials, ns)
}
