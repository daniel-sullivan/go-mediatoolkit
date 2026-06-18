//go:build cgo

package channel

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// runWithSource constructs a Go BitReader backed by a slice and invokes
// f. Mirrors the helper used by the subframe parity package.
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

// clampSigned clamps v into the signed range representable in `bps`
// bits, matching the per-subframe value domain the decoder produces.
func clampSigned(v int64, bps uint32) int64 {
	max := int64(1)<<(bps-1) - 1
	min := -(int64(1) << (bps - 1))
	if v > max {
		return max
	}
	if v < min {
		return min
	}
	return v
}

// ── undo_channel_coding ─────────────────────────────────────────────

func TestParityUndoChannelCoding(t *testing.T) {
	assignments := []struct {
		name string
		code int
		ca   nativeflac.ChannelAssignment
	}{
		{"independent", 0, nativeflac.ChannelAssignmentIndependent},
		{"left_side", 1, nativeflac.ChannelAssignmentLeftSide},
		{"right_side", 2, nativeflac.ChannelAssignmentRightSide},
		{"mid_side", 3, nativeflac.ChannelAssignmentMidSide},
	}

	r := rand.New(rand.NewPCG(7777, 8888))
	blocksizes := []uint32{1, 2, 7, 16, 256, 4096}

	for _, a := range assignments {
		for _, bps := range []uint32{8, 16, 24, 32} {
			// For LEFT/RIGHT/MID_SIDE the side channel uses bps+1 bits
			// (33 when bps==32). The 33-bit side path is selected when
			// bps == 32: side lives in the int64 side buffer.
			sideInUse := bps == 32 && a.code != 0
			for _, blocksize := range blocksizes {
				// channel 0 occupies bps bits; channel 1 (or the side)
				// occupies bps+1 bits for the decorrelated modes.
				ch0 := make([]int32, blocksize)
				ch1 := make([]int32, blocksize)
				side := make([]int64, blocksize)

				ch0Bps := bps
				ch1Bps := bps
				switch a.code {
				case 1, 3: // LEFT_SIDE, MID_SIDE: channel 1 is the side
					ch1Bps = bps + 1
				case 2: // RIGHT_SIDE: channel 0 is the side
					ch0Bps = bps + 1
				}

				for i := uint32(0); i < blocksize; i++ {
					ch0[i] = int32(clampSigned(int64(int32(r.Uint32())), ch0Bps))
					ch1[i] = int32(clampSigned(int64(int32(r.Uint32())), ch1Bps))
					// 33-bit side: full int64 range clamped to 33 bits.
					sv := int64(r.Uint64())
					side[i] = clampSigned(sv, 33)
				}

				var sideArg []int64
				if sideInUse {
					sideArg = side
				}

				cOut0, cOut1 := CgoUndoChannelCoding(a.code, ch0, ch1, sideArg, sideInUse, blocksize)

				goOut0 := append([]int32(nil), ch0...)
				goOut1 := append([]int32(nil), ch1...)
				nativeflac.UndoChannelCoding(a.ca, goOut0, goOut1, sideArg, sideInUse, blocksize)

				require.Equal(t, cOut0, goOut0, "%s bps=%d bs=%d sideInUse=%v ch0 mismatch", a.name, bps, blocksize, sideInUse)
				require.Equal(t, cOut1, goOut1, "%s bps=%d bs=%d sideInUse=%v ch1 mismatch", a.name, bps, blocksize, sideInUse)
			}
		}
	}
}

// ── frame footer CRC-16 ─────────────────────────────────────────────

func TestParityFrameFooterCRC(t *testing.T) {
	r := rand.New(rand.NewPCG(13, 37))

	for _, payloadLen := range []int{0, 1, 2, 3, 7, 8, 9, 16, 64, 1000} {
		for _, corrupt := range []bool{false, true} {
			payload := make([]byte, payloadLen)
			for i := range payload {
				payload[i] = byte(r.Uint32())
			}
			w0 := byte(r.Uint32())
			w1 := byte(r.Uint32())

			body := BuildFooter(payload, w0, w1, corrupt)

			cMatch, cSt := CgoVerifyFooter(body, w0, w1, payloadLen)
			require.Equal(t, 0, cSt, "C-side read failed payloadLen=%d corrupt=%v", payloadLen, corrupt)
			require.Equal(t, !corrupt, cMatch, "C-side match wrong payloadLen=%d corrupt=%v", payloadLen, corrupt)

			runWithSource(body, func(br *nativeflac.BitReader) {
				// Seed the CRC the way read_frame_ does: fold the two
				// warmup bytes through the running CRC, then arm
				// tracking on the byte-aligned reader.
				seed := nativeflac.CRC16Seed(w0, w1)
				br.ResetReadCRC16(seed)
				// Consume the payload bytes.
				for k := 0; k < payloadLen; k++ {
					_, ok := br.ReadRawUint32(8)
					require.True(t, ok, "Go payload read failed")
				}
				match, ok := nativeflac.ReadFrameFooterCRC(br)
				require.True(t, ok, "Go footer read failed payloadLen=%d", payloadLen)
				require.Equal(t, !corrupt, match, "Go match wrong payloadLen=%d corrupt=%v", payloadLen, corrupt)
				require.Equal(t, cMatch, match, "Go/C match disagree payloadLen=%d corrupt=%v", payloadLen, corrupt)
			})
		}
	}
}
