//go:build cgo

package subframe

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// runWithSource constructs a Go BitReader backed by a slice and
// invokes f. Used to drive each Go port over the same byte buffer
// the cgo decoder consumed.
func runWithSource(body []byte, f func(*nativeflac.BitReader)) {
	br := nativeflac.NewBitReader()
	off := 0
	br.Init(func(buf []byte) (uint, bool) {
		avail := len(body) - off
		if avail <= 0 {
			return 0, false
		}
		n := len(buf)
		if n > avail {
			n = avail
		}
		copy(buf, body[off:off+n])
		off += n
		return uint(n), true
	})
	f(br)
}

// ── CONSTANT ────────────────────────────────────────────────────────

func TestParityConstantSubframe(t *testing.T) {
	for _, bps := range []uint32{4, 8, 12, 16, 20, 24, 32} {
		max := int64(1)<<(bps-1) - 1
		min := -(int64(1) << (bps - 1))
		for _, value := range []int64{0, 1, -1, max, min, max / 3, min / 3} {
			body := EncodeConstant(value, bps)
			cVal, cSt := CgoDecodeConstant(body, bps)
			require.Equal(t, 0, cSt, "C-side decode failed bps=%d value=%d", bps, value)
			require.Equal(t, value, cVal, "C-side decoded wrong value")

			runWithSource(body, func(br *nativeflac.BitReader) {
				var sub nativeflac.Subframe
				st := nativeflac.ReadSubframeConstant(br, &sub, bps)
				require.Equal(t, nativeflac.SubframeOK, st, "Go decode failed bps=%d value=%d", bps, value)
				require.Equal(t, value, sub.Constant.Value, "Go decoded wrong value bps=%d", bps)
				require.Equal(t, nativeflac.SubframeConstant, sub.Type)
			})
		}
	}
}

// ── VERBATIM (32-bit) ───────────────────────────────────────────────

func TestParityVerbatimSubframe32(t *testing.T) {
	r := rand.New(rand.NewPCG(2001, 2002))
	for _, bps := range []uint32{8, 16, 24, 32} {
		for _, blocksize := range []uint32{1, 16, 1024} {
			samples := make([]int32, blocksize)
			max := int32(1)<<(bps-1) - 1
			min := int32(-(int64(1) << (bps - 1)))
			for i := range samples {
				v := int32(r.Uint32())
				if v > max {
					v = max
				}
				if v < min {
					v = min
				}
				samples[i] = v
			}
			body := EncodeVerbatim(samples, blocksize, bps)

			cOut, cSt := CgoDecodeVerbatim32(body, blocksize, bps)
			require.Equal(t, 0, cSt)
			require.Equal(t, samples, cOut)

			runWithSource(body, func(br *nativeflac.BitReader) {
				var sub nativeflac.Subframe
				sub.Verbatim.Data32 = make([]int32, blocksize)
				st := nativeflac.ReadSubframeVerbatim(br, &sub, blocksize, bps)
				require.Equal(t, nativeflac.SubframeOK, st)
				require.Equal(t, samples, sub.Verbatim.Data32, "verbatim mismatch bps=%d blocksize=%d", bps, blocksize)
				require.Equal(t, nativeflac.SubframeVerbatim, sub.Type)
			})
		}
	}
}

// ── PARTITIONED_RICE residual ───────────────────────────────────────

func TestParityResidualPartitionedRice(t *testing.T) {
	r := rand.New(rand.NewPCG(2101, 2102))
	for _, blocksize := range []uint32{16, 64, 4096} {
		for partitionOrder := uint32(0); partitionOrder <= 4; partitionOrder++ {
			partitionSamples := blocksize >> partitionOrder
			if partitionSamples == 0 {
				continue
			}
			for _, predictorOrder := range []uint32{0, 1, 4, 8} {
				if partitionSamples <= predictorOrder {
					continue
				}
				partitions := uint32(1) << partitionOrder
				riceParams := make([]uint32, partitions)
				for i := range riceParams {
					riceParams[i] = uint32(r.IntN(14)) // 0..13, well below escape param 15
				}

				// Generate residual in [-2^11+1, 2^11-1] so even
				// rice param 0 produces tractable unary codes.
				residual := make([]int32, blocksize-predictorOrder)
				for i := range residual {
					residual[i] = int32(r.IntN(2048) - 1024)
				}

				body := EncodeResidual(predictorOrder, partitionOrder, blocksize, false, residual, riceParams)

				_, cParams, _, cSt := CgoDecodeResidual(body, predictorOrder, partitionOrder, blocksize, false)
				require.Equal(t, 0, cSt, "C decode failed po=%d po_ord=%d bs=%d",
					predictorOrder, partitionOrder, blocksize)

				runWithSource(body, func(br *nativeflac.BitReader) {
					goRes := make([]int32, blocksize-predictorOrder)
					contents := nativeflac.PartitionedRiceContents{}
					st := nativeflac.ReadResidualPartitionedRice(br,
						predictorOrder, partitionOrder, blocksize,
						&contents, goRes, false)
					require.Equal(t, nativeflac.SubframeOK, st,
						"Go decode failed po=%d po_ord=%d bs=%d",
						predictorOrder, partitionOrder, blocksize)
					require.Equal(t, residual, goRes,
						"residual mismatch po=%d po_ord=%d bs=%d",
						predictorOrder, partitionOrder, blocksize)
					// Parameters captured in contents must match the C oracle.
					for i := uint32(0); i < partitions; i++ {
						require.Equal(t, cParams[i], contents.Parameters[i],
							"rice parameter mismatch partition=%d", i)
					}
				})
			}
		}
	}
}

// PARTITIONED_RICE2 — same algorithm with 5-bit parameter widths.
func TestParityResidualPartitionedRice2(t *testing.T) {
	r := rand.New(rand.NewPCG(2201, 2202))
	for _, blocksize := range []uint32{16, 256, 4096} {
		for partitionOrder := uint32(0); partitionOrder <= 4; partitionOrder++ {
			partitionSamples := blocksize >> partitionOrder
			if partitionSamples == 0 {
				continue
			}
			predictorOrder := uint32(4)
			if partitionSamples <= predictorOrder {
				continue
			}
			partitions := uint32(1) << partitionOrder
			riceParams := make([]uint32, partitions)
			for i := range riceParams {
				riceParams[i] = uint32(r.IntN(20)) // 0..19, below escape 31
			}
			residual := make([]int32, blocksize-predictorOrder)
			for i := range residual {
				residual[i] = int32(r.IntN(2048) - 1024)
			}
			body := EncodeResidual(predictorOrder, partitionOrder, blocksize, true, residual, riceParams)

			_, _, _, cSt := CgoDecodeResidual(body, predictorOrder, partitionOrder, blocksize, true)
			require.Equal(t, 0, cSt)

			runWithSource(body, func(br *nativeflac.BitReader) {
				goRes := make([]int32, blocksize-predictorOrder)
				contents := nativeflac.PartitionedRiceContents{}
				st := nativeflac.ReadResidualPartitionedRice(br,
					predictorOrder, partitionOrder, blocksize,
					&contents, goRes, true)
				require.Equal(t, nativeflac.SubframeOK, st)
				require.Equal(t, residual, goRes)
			})
		}
	}
}

// ── FIXED subframe (decoded samples) ────────────────────────────────

func TestParityFixedSubframeRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(2301, 2302))
	for _, blocksize := range []uint32{16, 64, 4096} {
		for order := uint32(0); order <= 4; order++ {
			for _, bps := range []uint32{16, 24} {
				if blocksize <= order {
					continue
				}
				for partitionOrder := uint32(0); partitionOrder <= 2; partitionOrder++ {
					partitionSamples := blocksize >> partitionOrder
					if partitionSamples <= order {
						continue
					}
					// Generate warmup + residual. Keep magnitudes
					// modest so the predictor inverse stays in
					// int32 range.
					warmup := make([]int64, order)
					for i := range warmup {
						warmup[i] = int64(r.IntN(1<<10) - 1<<9)
					}
					partitions := uint32(1) << partitionOrder
					riceParams := make([]uint32, partitions)
					for i := range riceParams {
						riceParams[i] = uint32(r.IntN(8))
					}
					residual := make([]int32, blocksize-order)
					for i := range residual {
						residual[i] = int32(r.IntN(256) - 128)
					}

					body := EncodeSubframeFixed(blocksize, bps, order, warmup,
						partitionOrder, false, residual, riceParams)

					// Run the Go port with full decode and compare
					// the materialised samples.
					runWithSource(body, func(br *nativeflac.BitReader) {
						var sub nativeflac.Subframe
						sub.Fixed.Residual = make([]int32, blocksize-order)
						out := make([]int32, blocksize)
						st := nativeflac.ReadSubframeFixed(br, &sub,
							blocksize, bps, order, out, nil, true)
						require.Equal(t, nativeflac.SubframeOK, st,
							"go subframe_fixed bs=%d order=%d po=%d bps=%d",
							blocksize, order, partitionOrder, bps)
						require.Equal(t, residual, sub.Fixed.Residual,
							"residual mismatch")
						// Compute the expected output ourselves to
						// double-check: warmup at front, predictor
						// inverse over residual.
						expected := make([]int32, blocksize)
						for i := uint32(0); i < order; i++ {
							expected[i] = int32(warmup[i])
						}
						if bps+order <= 32 {
							nativeflac.FixedRestoreSignal(residual, order, expected)
						} else {
							nativeflac.FixedRestoreSignalWide(residual, order, expected)
						}
						require.Equal(t, expected, out)
					})
				}
			}
		}
	}
}

// ── LPC subframe (decoded samples) ──────────────────────────────────

func TestParityLPCSubframeRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(2401, 2402))
	for _, blocksize := range []uint32{16, 64, 4096} {
		for _, order := range []uint32{1, 4, 8, 12, 32} {
			for _, bps := range []uint32{16, 24} {
				if blocksize <= order {
					continue
				}
				partitionOrder := uint32(0)
				partitionSamples := blocksize >> partitionOrder
				if partitionSamples <= order {
					continue
				}
				prec := uint32(12) // 12-bit qlp precision
				shift := 8         // typical libFLAC shift

				warmup := make([]int64, order)
				for i := range warmup {
					warmup[i] = int64(r.IntN(1<<10) - 1<<9)
				}
				qlp := make([]int32, order)
				for i := range qlp {
					qlp[i] = int32(r.IntN(1<<(prec-1)) - 1<<(prec-2))
				}
				partitions := uint32(1) << partitionOrder
				riceParams := make([]uint32, partitions)
				for i := range riceParams {
					riceParams[i] = uint32(r.IntN(8))
				}
				residual := make([]int32, blocksize-order)
				for i := range residual {
					residual[i] = int32(r.IntN(256) - 128)
				}

				body := EncodeSubframeLPC(blocksize, bps, order, warmup,
					prec, shift, qlp,
					partitionOrder, false, residual, riceParams)

				runWithSource(body, func(br *nativeflac.BitReader) {
					var sub nativeflac.Subframe
					sub.LPC.Residual = make([]int32, blocksize-order)
					out := make([]int32, blocksize)
					st := nativeflac.ReadSubframeLPC(br, &sub,
						blocksize, bps, order, out, nil, true)
					require.Equal(t, nativeflac.SubframeOK, st,
						"go subframe_lpc bs=%d order=%d bps=%d", blocksize, order, bps)
					require.Equal(t, residual, sub.LPC.Residual)
					require.Equal(t, prec, sub.LPC.QLPCoeffPrecision)
					require.Equal(t, shift, sub.LPC.QuantizationLevel)
					for i := uint32(0); i < order; i++ {
						require.Equal(t, qlp[i], sub.LPC.QLPCoeff[i],
							"qlp_coeff[%d]", i)
						require.Equal(t, warmup[i], sub.LPC.Warmup[i],
							"warmup[%d]", i)
					}

					// Cross-check: hand-run the predictor inverse
					// and assert the materialised output matches.
					expected := make([]int32, blocksize)
					for i := uint32(0); i < order; i++ {
						expected[i] = int32(warmup[i])
					}
					coeff := qlp
					if nativeflac.LPCMaxResidualBPS(bps, coeff, order, shift) <= 32 &&
						nativeflac.LPCMaxPredictionBeforeShiftBPS(bps, coeff, order) <= 32 {
						nativeflac.LPCRestoreSignal(residual, coeff, order, shift, expected)
					} else {
						nativeflac.LPCRestoreSignalWide(residual, coeff, order, shift, expected)
					}
					require.Equal(t, expected, out)
				})
			}
		}
	}
}

// ── ZeroPadding ─────────────────────────────────────────────────────

func TestZeroPaddingByteAligned(t *testing.T) {
	body := []byte{0x00}
	runWithSource(body, func(br *nativeflac.BitReader) {
		st := nativeflac.ReadZeroPadding(br)
		require.Equal(t, nativeflac.SubframeOK, st)
	})
}

func TestZeroPaddingMidByte(t *testing.T) {
	// Read 5 bits to land mid-byte (consumed_bits=5), leaving 3
	// trailing zero bits → ReadZeroPadding should consume them.
	body := []byte{0xF8} // 11111 000 — 5 ones, 3 zeros
	runWithSource(body, func(br *nativeflac.BitReader) {
		v, ok := br.ReadRawUint32(5)
		require.True(t, ok)
		require.Equal(t, uint32(0x1F), v)
		st := nativeflac.ReadZeroPadding(br)
		require.Equal(t, nativeflac.SubframeOK, st)
	})
}

func TestZeroPaddingNonZero(t *testing.T) {
	// Read 5 bits, leaving 3 trailing bits with at least one 1.
	body := []byte{0xF9} // 11111 001
	runWithSource(body, func(br *nativeflac.BitReader) {
		_, ok := br.ReadRawUint32(5)
		require.True(t, ok)
		st := nativeflac.ReadZeroPadding(br)
		require.Equal(t, nativeflac.SubframeBadFrame, st)
	})
}
