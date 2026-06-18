//go:build cgo

package frame

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// goDecodeFrame drives the Go ReadFrame port over a fabricated frame
// body. The first two bytes are the sync warmup (already consumed during
// frame_sync_); the bitreader is fed body[2:] and the warmup is passed
// through ReadFrameHeaderInput, exactly as stream_decoder.c arranges it.
func goDecodeFrame(t *testing.T, body []byte, siSR, siBPS, siMinBS, siMaxBS uint32) (
	interleaved []int32, h nativeflac.FrameHeader, status nativeflac.FrameStatus) {

	br := nativeflac.NewBitReader()
	off := 0
	tail := body[2:]
	br.Init(func(buf []byte) (uint, bool) {
		avail := len(tail) - off
		if avail <= 0 {
			return 0, false
		}
		n := len(buf)
		if n > avail {
			n = avail
		}
		copy(buf, tail[off:off+n])
		off += n
		return uint(n), true
	})

	state := &nativeflac.FrameDecodeState{
		Output: make([][]int32, MaxChannels),
		Side:   make([]int64, MaxBlocksize),
	}
	for c := range state.Output {
		state.Output[c] = make([]int32, MaxBlocksize)
	}

	in := nativeflac.ReadFrameHeaderInput{
		HeaderWarmup:            [2]byte{body[0], body[1]},
		HasStreamInfo:           siSR != 0,
		StreamInfoSampleRate:    siSR,
		StreamInfoBitsPerSample: siBPS,
		StreamInfoMinBlockSize:  siMinBS,
		StreamInfoMaxBlockSize:  siMaxBS,
	}

	var nextFBS uint32
	h, nextFBS, status = nativeflac.ReadFrame(br, state, in, true)
	_ = nextFBS
	if status != nativeflac.FrameOK {
		return nil, h, status
	}

	// Interleave the decoded channels.
	interleaved = make([]int32, h.Blocksize*h.Channels)
	for i := uint32(0); i < h.Blocksize; i++ {
		for c := uint32(0); c < h.Channels; c++ {
			interleaved[i*h.Channels+c] = state.Output[c][i]
		}
	}
	return interleaved, h, nativeflac.FrameOK
}

// makeRiceableResidual builds a residual whose values are small enough
// that the given rice parameters produce tractable unary codes (and
// never hit the escape path). One rice parameter per partition.
func makeRiceableResidual(r *rand.Rand, n int) []int32 {
	res := make([]int32, n)
	for i := range res {
		res[i] = int32(r.IntN(256) - 128)
	}
	return res
}

func riceParams(r *rand.Rand, partitions uint32) []uint32 {
	p := make([]uint32, partitions)
	for i := range p {
		p[i] = uint32(r.IntN(8))
	}
	return p
}

// C oracle status codes mirroring the Go FrameStatus enum (frame_cgo_src.c).
const (
	cFrOK          = 0
	cFrOutOfBounds = 4
)

// assertParity decodes body with both the C oracle and the Go port and
// asserts the two sides AGREE: either both accept the frame (FrameOK /
// FR_OK), in which case the decoded interleaved samples and parsed header
// fields must be bit-exact; or both reject it as out-of-range
// (FrameOutOfBounds / FR_OUT_OF_BOUNDS, the bps-fit check at
// stream_decoder.c:2457-2473). The fixtures are never clamped — agreement
// on the rejection path is the coverage for the new OOB check.
func assertParity(t *testing.T, body []byte, siSR, siBPS, siMinBS, siMaxBS uint32, msg string) {
	t.Helper()

	cInter, cBS, cCh, cBps, cCa, cSN, cSt := CgoDecodeFrame(body, siSR, siBPS, siMinBS, siMaxBS)
	goInter, goH, goSt := goDecodeFrame(t, body, siSR, siBPS, siMinBS, siMaxBS)

	// Status must agree: both OK or both OUT_OF_BOUNDS.
	switch goSt {
	case nativeflac.FrameOK:
		require.Equal(t, cFrOK, cSt, "Go accepted frame but C rejected it (status %d): %s", cSt, msg)
	case nativeflac.FrameOutOfBounds:
		require.Equal(t, cFrOutOfBounds, cSt, "Go rejected frame OUT_OF_BOUNDS but C status was %d: %s", cSt, msg)
	default:
		require.FailNow(t, "unexpected Go status", "status %d: %s", goSt, msg)
	}

	if goSt != nativeflac.FrameOK {
		// Both sides rejected the frame; no samples to compare.
		return
	}

	// Header parity.
	require.Equal(t, cBS, goH.Blocksize, "blocksize: %s", msg)
	require.Equal(t, cCh, goH.Channels, "channels: %s", msg)
	require.Equal(t, cBps, goH.BitsPerSample, "bps: %s", msg)
	require.Equal(t, cCa, uint32(goH.ChannelAssignment), "channel_assignment: %s", msg)
	require.Equal(t, cSN, goH.Number, "sample_number: %s", msg)

	// Decoded interleaved samples parity.
	require.Equal(t, cInter, goInter, "decoded samples: %s", msg)
}

// ── CONSTANT subframes (mono + stereo independent) ──────────────────────

func TestParityFrameConstant(t *testing.T) {
	for _, bps := range []uint32{8, 16, 24} {
		for _, channels := range []uint32{1, 2} {
			ca := uint32(0) // INDEPENDENT
			subs := make([]SubframeDesc, channels)
			for c := range subs {
				subs[c] = SubframeDesc{
					Type:          0,
					SubframeBPS:   bps,
					ConstantValue: int64(1234 * (c + 1)),
				}
			}
			body := AssembleFrame(256, 44100, channels, bps, ca, 0, subs)
			require.NotEmpty(t, body)
			assertParity(t, body, 44100, bps, 256, 256, "constant")
		}
	}
}

// ── VERBATIM subframes ──────────────────────────────────────────────────

func TestParityFrameVerbatim(t *testing.T) {
	r := rand.New(rand.NewPCG(3001, 3002))
	for _, bps := range []uint32{8, 16, 24} {
		blocksize := uint32(64)
		channels := uint32(2)
		subs := make([]SubframeDesc, channels)
		max := int32(1)<<(bps-1) - 1
		min := -max - 1
		for c := range subs {
			samples := make([]int32, blocksize)
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
			subs[c] = SubframeDesc{Type: 1, SubframeBPS: bps, Verbatim: samples}
		}
		body := AssembleFrame(blocksize, 48000, channels, bps, 0, 100, subs)
		require.NotEmpty(t, body)
		assertParity(t, body, 48000, bps, blocksize, blocksize, "verbatim")
	}
}

// ── FIXED subframes ─────────────────────────────────────────────────────

func TestParityFrameFixed(t *testing.T) {
	r := rand.New(rand.NewPCG(3101, 3102))
	for _, bps := range []uint32{16, 24} {
		for order := uint32(0); order <= 4; order++ {
			blocksize := uint32(256)
			channels := uint32(2)
			partitionOrder := uint32(2)
			partitions := uint32(1) << partitionOrder
			subs := make([]SubframeDesc, channels)
			for c := range subs {
				warmup := make([]int64, order)
				for i := range warmup {
					warmup[i] = int64(r.IntN(1<<10) - 1<<9)
				}
				subs[c] = SubframeDesc{
					Type:           2,
					SubframeBPS:    bps,
					Order:          order,
					Warmup:         warmup,
					PartitionOrder: partitionOrder,
					Residual:       makeRiceableResidual(r, int(blocksize-order)),
					RiceParams:     riceParams(r, partitions),
				}
			}
			body := AssembleFrame(blocksize, 44100, channels, bps, 0, 200, subs)
			require.NotEmpty(t, body)
			assertParity(t, body, 44100, bps, blocksize, blocksize, "fixed")
		}
	}
}

// ── LPC subframes ───────────────────────────────────────────────────────

func TestParityFrameLPC(t *testing.T) {
	r := rand.New(rand.NewPCG(3201, 3202))
	for _, bps := range []uint32{16, 24} {
		for _, order := range []uint32{1, 4, 8, 12, 32} {
			blocksize := uint32(512)
			channels := uint32(2)
			prec := uint32(12)
			shift := 8
			subs := make([]SubframeDesc, channels)
			for c := range subs {
				warmup := make([]int64, order)
				for i := range warmup {
					warmup[i] = int64(r.IntN(1<<10) - 1<<9)
				}
				qlp := make([]int32, order)
				for i := range qlp {
					qlp[i] = int32(r.IntN(1<<(prec-1)) - 1<<(prec-2))
				}
				subs[c] = SubframeDesc{
					Type:              3,
					SubframeBPS:       bps,
					Order:             order,
					Warmup:            warmup,
					QLPCoeffPrecision: prec,
					QuantizationLevel: shift,
					QLPCoeff:          qlp,
					PartitionOrder:    0,
					Residual:          makeRiceableResidual(r, int(blocksize-order)),
					RiceParams:        riceParams(r, 1),
				}
			}
			body := AssembleFrame(blocksize, 44100, channels, bps, 0, 300, subs)
			require.NotEmpty(t, body)
			assertParity(t, body, 44100, bps, blocksize, blocksize, "lpc")
		}
	}
}

// ── Stereo decorrelation (LEFT_SIDE / RIGHT_SIDE / MID_SIDE) ─────────────

// For decorrelated stereo, the side channel carries bps+1 bits. We use
// FIXED subframes for both channels; the side subframe is given the +1
// bps so AssembleFrame writes the wider warmup/residual correctly.
func TestParityFrameDecorrelation(t *testing.T) {
	r := rand.New(rand.NewPCG(3301, 3302))
	// channel_assignment: 1=LEFT_SIDE, 2=RIGHT_SIDE, 3=MID_SIDE.
	for _, ca := range []uint32{1, 2, 3} {
		for _, bps := range []uint32{16, 24} {
			blocksize := uint32(256)
			order := uint32(2)
			partitionOrder := uint32(1)
			partitions := uint32(1) << partitionOrder

			// Which channel is the side channel (+1 bps)?
			sideCh := uint32(1) // LEFT_SIDE/MID_SIDE: ch1 is side
			if ca == 2 {        // RIGHT_SIDE: ch0 is side
				sideCh = 0
			}

			subs := make([]SubframeDesc, 2)
			for c := uint32(0); c < 2; c++ {
				cbps := bps
				if c == sideCh {
					cbps = bps + 1
				}
				warmup := make([]int64, order)
				for i := range warmup {
					warmup[i] = int64(r.IntN(1<<10) - 1<<9)
				}
				subs[c] = SubframeDesc{
					Type:           2,
					SubframeBPS:    cbps,
					Order:          order,
					Warmup:         warmup,
					PartitionOrder: partitionOrder,
					Residual:       makeRiceableResidual(r, int(blocksize-order)),
					RiceParams:     riceParams(r, partitions),
				}
			}
			body := AssembleFrame(blocksize, 44100, 2, bps, ca, 400, subs)
			require.NotEmpty(t, body)
			assertParity(t, body, 44100, bps, blocksize, blocksize, "decorrelation")
		}
	}
}

// ── Wasted bits ─────────────────────────────────────────────────────────

// A subframe with wasted_bits applies a left shift after decode. We use a
// CONSTANT subframe so the post-shift value is easy to reason about.
func TestParityFrameWastedBits(t *testing.T) {
	for _, bps := range []uint32{16, 24} {
		for _, wasted := range []uint32{1, 3, 5} {
			channels := uint32(2)
			subs := make([]SubframeDesc, channels)
			for c := range subs {
				// The constant value is stored in (bps - wasted) bits;
				// keep it small so it fits.
				subs[c] = SubframeDesc{
					Type:          0,
					SubframeBPS:   bps,
					WastedBits:    wasted,
					ConstantValue: int64(11 * (c + 1)),
				}
			}
			body := AssembleFrame(128, 44100, channels, bps, 0, 500, subs)
			require.NotEmpty(t, body)
			assertParity(t, body, 44100, bps, 128, 128, "wasted-bits")
		}
	}
}

// ── 32-bit (33-bit side) path ───────────────────────────────────────────

// For 32-bit streams with stereo decorrelation the side channel is 33-bit
// and routes through the int64 side buffer. The main channel (bps == 32)
// uses a VERBATIM int32 subframe; the side channel (bps == 33) uses a
// CONSTANT subframe — FLAC__subframe_add_verbatim asserts subframe_bps < 33
// on the int32 path, whereas CONSTANT writes via write_raw_int64 and so
// handles the 33-bit width. This exercises side_subframe_in_use + the
// 33-bit undo_channel_coding branch.
func TestParityFrame32BitSide(t *testing.T) {
	r := rand.New(rand.NewPCG(3401, 3402))
	bps := uint32(32)
	blocksize := uint32(64)
	for _, ca := range []uint32{1, 2, 3} {
		sideCh := uint32(1)
		if ca == 2 {
			sideCh = 0
		}
		subs := make([]SubframeDesc, 2)
		for c := uint32(0); c < 2; c++ {
			if c == sideCh {
				// 33-bit side channel via CONSTANT.
				subs[c] = SubframeDesc{
					Type:          0,
					SubframeBPS:   bps + 1,
					ConstantValue: int64(r.Uint64()&((1<<33)-1)) - (1 << 32),
				}
			} else {
				// 32-bit main channel via VERBATIM int32.
				samples := make([]int32, blocksize)
				for i := range samples {
					samples[i] = int32(r.Uint32())
				}
				subs[c] = SubframeDesc{Type: 1, SubframeBPS: bps, Verbatim: samples}
			}
		}
		body := AssembleFrame(blocksize, 44100, 2, bps, ca, 600, subs)
		require.NotEmpty(t, body)
		assertParity(t, body, 44100, bps, blocksize, blocksize, "32bit-side")
	}
}
