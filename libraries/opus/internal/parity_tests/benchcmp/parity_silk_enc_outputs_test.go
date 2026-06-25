//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math/rand"
	"sort"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// monotoneNLSFSorted returns a sorted strictly-increasing NLSF vector.
func monotoneNLSFSorted(r *rand.Rand, d int) []int16 {
	raw := make([]int, d)
	for i := range raw {
		raw[i] = 1 + r.Intn(32766)
	}
	sort.Ints(raw)
	for i := 1; i < d; i++ {
		if raw[i] <= raw[i-1] {
			raw[i] = raw[i-1] + 1
		}
	}
	if raw[d-1] > 32766 {
		sh := raw[d-1] - 32766
		for i := range raw {
			raw[i] -= sh
		}
	}
	out := make([]int16, d)
	for i, v := range raw {
		out[i] = int16(v)
	}
	return out
}

func TestParity_SilkProcessNLSFs(t *testing.T) {
	r := rand.New(rand.NewSource(800))
	for _, wb := range []bool{false, true} {
		order := 16
		if !wb {
			order = 10
		}
		for _, nb_subfr := range []int{2, 4} {
			for _, useInterp := range []int{0, 1} {
				for _, sigType := range []int{0, 1, 2} {
					for trial := 0; trial < 10; trial++ {
						// Default NLSFInterpCoef_Q2 to 4 (no interp) for useInterp=0
						interp := 1 << 2
						if useInterp == 1 {
							interp = r.Intn(5)
						}
						nlsf := monotoneNLSFSorted(r, order)
						prev := monotoneNLSFSorted(r, order)
						saQ8 := r.Intn(257)
						surv := 1 + r.Intn(8)

						gPA, gPB, gN, gI := nativeopus.ExportTestProcessNLSFs(wb, saQ8, useInterp,
							interp, sigType, nb_subfr, surv, nlsf, prev)
						cPA, cPB, cN, cI := cProcessNLSFs(wb, saQ8, useInterp, interp,
							sigType, nb_subfr, surv, nlsf, prev)

						// silk_process_NLSFs writes exactly `order` entries of
						// PredCoef; positions [order..MAX_LPC_ORDER-1] are left
						// as whatever the caller provided. In C's test harness
						// those are stack-uninitialised, so compare only the
						// positions the function actually writes.
						if !eqInt16Slice(gPA[:order], cPA[:order]) ||
							!eqInt16Slice(gPB[:order], cPB[:order]) ||
							!eqInt16Slice(gN, cN) || !eqInt8Slice(gI, cI) {
							t.Fatalf("wb=%v nb=%d ui=%d interp=%d sig=%d trial=%d\nC  PA=%v PB=%v NLSF=%v idx=%v\nGo PA=%v PB=%v NLSF=%v idx=%v",
								wb, nb_subfr, useInterp, interp, sigType, trial,
								cPA, cPB, cN, cI, gPA, gPB, gN, gI)
						}
					}
				}
			}
		}
	}
}

func TestParity_SilkStereoLRToMS(t *testing.T) {
	r := rand.New(rand.NewSource(801))
	for _, fs_kHz := range []int{8, 12, 16} {
		for _, nb_subfr := range []int{2, 4} {
			frame_length := 5 * nb_subfr * fs_kHz
			for trial := 0; trial < 20; trial++ {
				bufSize := frame_length + 2
				x1 := make([]int16, bufSize)
				x2 := make([]int16, bufSize)
				amp := 1 + r.Intn(5000)
				for i := range x1 {
					x1[i] = int16(r.Intn(2*amp) - amp)
					x2[i] = int16(r.Intn(2*amp) - amp)
				}
				st := nativeopus.StereoEncStateMirror{
					SmthWidth: int16(r.Intn(1 << 14)),
					WidthPrev: int16(r.Intn(1 << 14)),
				}
				cSt := cStereoState{
					SmthWidth: st.SmthWidth,
					WidthPrev: st.WidthPrev,
				}

				totalRate := int32(5000 + r.Intn(40000))
				prevSAQ8 := r.Intn(257)
				toMono := 0

				gX1, gX2, gIx, gMO, gMR, gSR, gStOut := nativeopus.ExportTestStereoLRToMS(
					x1, x2, totalRate, prevSAQ8, toMono, fs_kHz, frame_length, st)
				cX1, cX2, cIx, cMO, cMR, cSR, cStOut := cStereoLRToMS(
					x1, x2, totalRate, prevSAQ8, toMono, fs_kHz, frame_length, cSt)

				if !eqInt16Slice(gX1, cX1) || !eqInt16Slice(gX2, cX2) ||
					gIx != cIx || gMO != cMO || gMR != cMR || gSR != cSR ||
					gStOut.PredPrev0 != cStOut.PredPrev0 ||
					gStOut.PredPrev1 != cStOut.PredPrev1 ||
					gStOut.SmthWidth != cStOut.SmthWidth ||
					gStOut.WidthPrev != cStOut.WidthPrev ||
					gStOut.SilentSideLen != cStOut.SilentSideLen ||
					gStOut.MidSideAmp != cStOut.MidSideAmp {
					t.Fatalf("fs=%d nb=%d trial=%d:\nGo x1=%v x2=%v ix=%v mo=%d rates=(%d,%d) state=%+v\nC  x1=%v x2=%v ix=%v mo=%d rates=(%d,%d) state=%+v",
						fs_kHz, nb_subfr, trial,
						gX1, gX2, gIx, gMO, gMR, gSR, gStOut,
						cX1, cX2, cIx, cMO, cMR, cSR, cStOut)
				}
			}
		}
	}
}

func TestParity_SilkEncodeIndices(t *testing.T) {
	r := rand.New(rand.NewSource(802))
	for _, wb := range []bool{false, true} {
		order := 16
		if !wb {
			order = 10
		}
		for _, nb_subfr := range []int{2, 4} {
			for _, fs_kHz := range []int{8, 12, 16} {
				for _, sigType := range []int{0, 1, 2} {
					for _, condCoding := range []int{0, 1, 2} {
						for trial := 0; trial < 10; trial++ {
							gainsIdx := make([]int8, 4)
							NLSFIdx := make([]int8, 17)
							LTPIdx := make([]int8, 4)
							// Use valid gain index range.
							// condCoding=2 => delta-coded gains[0], must be <41. Else independent.
							if condCoding == 2 {
								gainsIdx[0] = int8(r.Intn(41))
							} else {
								gainsIdx[0] = int8(r.Intn(64))
							}
							for i := 1; i < nb_subfr; i++ {
								gainsIdx[i] = int8(r.Intn(41))
							}
							NLSFIdx[0] = int8(r.Intn(32))
							for i := 1; i <= order; i++ {
								NLSFIdx[i] = int8(r.Intn(13) - 6)
							}
							PERIndex := r.Intn(3)
							for i := 0; i < nb_subfr; i++ {
								LTPIdx[i] = int8(r.Intn(8 << PERIndex))
							}
							// lagIndex valid range depends on fs_kHz; use safe range.
							// NLSF interp must be < 5.
							NLSFInterp := r.Intn(5)
							LTPScale := 0
							if condCoding == 0 {
								LTPScale = r.Intn(3)
							}
							// contour index range per assertion.
							var contour int
							if nb_subfr == 4 {
								if fs_kHz == 8 {
									contour = r.Intn(11)
								} else {
									contour = r.Intn(34)
								}
							} else {
								if fs_kHz == 8 {
									contour = r.Intn(3)
								} else {
									contour = r.Intn(12)
								}
							}
							lagIdx := r.Intn(100)
							seed := r.Intn(4)
							ecPrevST := r.Intn(3)
							ecPrevLag := r.Intn(100)
							quantOff := r.Intn(2)

							gOut := nativeopus.ExportTestEncodeIndices(wb, nb_subfr, fs_kHz,
								sigType, quantOff, gainsIdx, NLSFIdx, int16(lagIdx), contour,
								NLSFInterp, PERIndex, LTPIdx, LTPScale, seed,
								ecPrevST, int16(ecPrevLag), 0, condCoding, 256)
							cOut := cEncodeIndices(wb, nb_subfr, fs_kHz, sigType, quantOff,
								gainsIdx, NLSFIdx, lagIdx, contour, NLSFInterp, PERIndex,
								LTPIdx, LTPScale, seed, ecPrevST, ecPrevLag, 0, condCoding, 256)
							if !bytes.Equal(gOut, cOut) {
								t.Fatalf("wb=%v nb=%d fs=%d sig=%d cond=%d trial=%d:\nC  %x\nGo %x",
									wb, nb_subfr, fs_kHz, sigType, condCoding, trial, cOut, gOut)
							}
						}
					}
				}
			}
		}
	}
}

func TestParity_SilkEncodePulses(t *testing.T) {
	r := rand.New(rand.NewSource(803))
	for _, fs_kHz := range []int{8, 12, 16} {
		for _, nb_subfr := range []int{2, 4} {
			frame_length := 5 * nb_subfr * fs_kHz
			for _, sigType := range []int{0, 1, 2} {
				for trial := 0; trial < 30; trial++ {
					pulses := make([]int8, frame_length)
					// Generate sparse pulse pattern typical of SILK residual.
					maxAmp := 1 + r.Intn(8)
					for i := range pulses {
						if r.Intn(3) == 0 {
							pulses[i] = int8(r.Intn(2*maxAmp) - maxAmp)
						}
					}
					quantOff := r.Intn(2)
					gOut := nativeopus.ExportTestSilkEncodePulses(sigType, quantOff, pulses, frame_length, 512)
					cOut := cSilkEncodePulses(sigType, quantOff, pulses, frame_length, 512)
					if !bytes.Equal(gOut, cOut) {
						t.Fatalf("fs=%d nb=%d sig=%d trial=%d:\nC  %x\nGo %x",
							fs_kHz, nb_subfr, sigType, trial, cOut, gOut)
					}
				}
			}
		}
	}
}
