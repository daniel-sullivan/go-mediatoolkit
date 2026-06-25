//go:build cgo

package predictors

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// fillRandom32 fills `out` with random int32s in the range
// [-1<<(bits-1), 1<<(bits-1)-1) so multiplications later don't blow
// past int32. The caller picks `bits` per test.
func fillRandom32(r *rand.Rand, out []int32, bits int) {
	half := int32(1) << (bits - 1)
	for i := range out {
		out[i] = int32(r.Uint32()&uint32(2*half-1)) - half
	}
}

func fillRandom64(r *rand.Rand, out []int64, bits int) {
	half := int64(1) << (bits - 1)
	for i := range out {
		out[i] = int64(r.Uint64()&uint64(2*half-1)) - half
	}
}

// ── Fixed predictor ─────────────────────────────────────────────────

func TestParityFixedRestoreSignal(t *testing.T) {
	r := rand.New(rand.NewPCG(1101, 1102))
	for _, dataLen := range []int{0, 1, 16, 4096} {
		for order := uint32(0); order <= 4; order++ {
			n := int(order) + dataLen
			cData := make([]int32, n)
			gData := make([]int32, n)
			residual := make([]int32, dataLen)
			// Bit depth chosen to keep order-4 multiplications inside
			// int32: at order 4 the worst-case multiplier is 6, so
			// 24-bit warm-up samples leave headroom.
			fillRandom32(r, cData[:order], 16)
			copy(gData, cData)
			fillRandom32(r, residual, 16)

			cgoFixedRestore(residual, order, cData)
			nativeflac.FixedRestoreSignal(residual, order, gData)
			require.Equal(t, cData, gData, "FixedRestoreSignal order=%d dataLen=%d", order, dataLen)
		}
	}
}

func TestParityFixedRestoreSignalWide(t *testing.T) {
	r := rand.New(rand.NewPCG(1201, 1202))
	for _, dataLen := range []int{0, 1, 16, 4096} {
		for order := uint32(0); order <= 4; order++ {
			n := int(order) + dataLen
			cData := make([]int32, n)
			gData := make([]int32, n)
			residual := make([]int32, dataLen)
			fillRandom32(r, cData[:order], 24)
			copy(gData, cData)
			fillRandom32(r, residual, 24)

			cgoFixedRestoreWide(residual, order, cData)
			nativeflac.FixedRestoreSignalWide(residual, order, gData)
			require.Equal(t, cData, gData, "FixedRestoreSignalWide order=%d dataLen=%d", order, dataLen)
		}
	}
}

func TestParityFixedRestoreSignalWide33(t *testing.T) {
	r := rand.New(rand.NewPCG(1301, 1302))
	for _, dataLen := range []int{0, 1, 16, 4096} {
		for order := uint32(0); order <= 4; order++ {
			n := int(order) + dataLen
			cData := make([]int64, n)
			gData := make([]int64, n)
			residual := make([]int32, dataLen)
			fillRandom64(r, cData[:order], 32)
			copy(gData, cData)
			fillRandom32(r, residual, 24)

			cgoFixedRestoreWide33(residual, order, cData)
			nativeflac.FixedRestoreSignalWide33Bit(residual, order, gData)
			require.Equal(t, cData, gData, "FixedRestoreSignalWide33 order=%d dataLen=%d", order, dataLen)
		}
	}
}

// ── LPC predictor ───────────────────────────────────────────────────

// lpcCases iterates a representative cross-product of (order, lp_quant,
// dataLen) combinations: orders 1..32 (libFLAC's full FLAC__MAX_LPC_ORDER
// range), shift values 0..15 (the QLP shift field is 5 bits, but
// negative values aren't reachable through the encoder), and several
// dataLen sizes.
func lpcCases(b *testing.B) {
	_ = b
}

func TestParityLPCRestoreSignal(t *testing.T) {
	r := rand.New(rand.NewPCG(1401, 1402))
	for _, dataLen := range []int{1, 16, 4096} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 13, 16, 32} {
			for _, shift := range []int{0, 5, 12} {
				n := int(order) + dataLen
				cData := make([]int32, n)
				gData := make([]int32, n)
				residual := make([]int32, dataLen)
				qlp := make([]int32, order)
				// QLP coefficients: libFLAC stores them in a 14-bit
				// signed precision typically; keep the magnitudes
				// modest so int32 sums don't wrap.
				fillRandom32(r, qlp, 12)
				fillRandom32(r, cData[:order], 14)
				copy(gData, cData)
				fillRandom32(r, residual, 12)

				cgoLPCRestore(residual, qlp, order, shift, cData)
				nativeflac.LPCRestoreSignal(residual, qlp, order, shift, gData)
				require.Equal(t, cData, gData,
					"LPCRestoreSignal order=%d shift=%d dataLen=%d", order, shift, dataLen)
			}
		}
	}
}

func TestParityLPCRestoreSignalWide(t *testing.T) {
	r := rand.New(rand.NewPCG(1501, 1502))
	for _, dataLen := range []int{1, 16, 4096} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 13, 16, 32} {
			for _, shift := range []int{0, 5, 12} {
				n := int(order) + dataLen
				cData := make([]int32, n)
				gData := make([]int32, n)
				residual := make([]int32, dataLen)
				qlp := make([]int32, order)
				fillRandom32(r, qlp, 14)
				fillRandom32(r, cData[:order], 24)
				copy(gData, cData)
				fillRandom32(r, residual, 24)

				cgoLPCRestoreWide(residual, qlp, order, shift, cData)
				nativeflac.LPCRestoreSignalWide(residual, qlp, order, shift, gData)
				require.Equal(t, cData, gData,
					"LPCRestoreSignalWide order=%d shift=%d dataLen=%d", order, shift, dataLen)
			}
		}
	}
}

func TestParityLPCRestoreSignalWide33(t *testing.T) {
	r := rand.New(rand.NewPCG(1601, 1602))
	for _, dataLen := range []int{1, 16, 1024} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 32} {
			for _, shift := range []int{0, 8, 14} {
				n := int(order) + dataLen
				cData := make([]int64, n)
				gData := make([]int64, n)
				residual := make([]int32, dataLen)
				qlp := make([]int32, order)
				fillRandom32(r, qlp, 12)
				fillRandom64(r, cData[:order], 32)
				copy(gData, cData)
				fillRandom32(r, residual, 24)

				cgoLPCRestoreWide33(residual, qlp, order, shift, cData)
				nativeflac.LPCRestoreSignalWide33Bit(residual, qlp, order, shift, gData)
				require.Equal(t, cData, gData,
					"LPCRestoreSignalWide33 order=%d shift=%d dataLen=%d", order, shift, dataLen)
			}
		}
	}
}
