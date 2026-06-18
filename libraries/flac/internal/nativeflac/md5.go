package nativeflac

import (
	"crypto/md5"
	"encoding/binary"
	"hash"
)

// 1:1 port of libflac/src/libFLAC/md5.c — restricted to the surface
// the FLAC encoder/decoder actually use:
//
//   - FLAC__MD5Init / Update / Final → MD5Context.{Init,Update,Final}
//   - FLAC__MD5Accumulate            → MD5Context.Accumulate
//
// The MD5 algorithm itself is bit-identical between libFLAC's
// hand-rolled implementation and Go's crypto/md5; both implement RFC
// 1321 byte-for-byte. We delegate to crypto/md5 rather than re-port
// MD5Transform, then wrap with the FLAC-specific signal-to-byte-stream
// formatter.

// MD5Context is the native counterpart of libFLAC's FLAC__MD5Context.
type MD5Context struct {
	h hash.Hash

	// scratch is reused across Accumulate calls to avoid per-block
	// allocations — analogous to libFLAC's ctx->internal_buf.
	scratch []byte
}

// Init — port of FLAC__MD5Init (md5.c:223). Resets the hash to its
// initial state.
func (c *MD5Context) Init() {
	if c.h == nil {
		c.h = md5.New()
	} else {
		c.h.Reset()
	}
}

// Update — port of FLAC__MD5Update (md5.c:172). Folds bytes into the
// running hash.
func (c *MD5Context) Update(data []byte) {
	if c.h == nil {
		c.Init()
	}
	c.h.Write(data)
}

// Final — port of FLAC__MD5Final (md5.c:241). Returns the 16-byte
// digest and resets the context.
func (c *MD5Context) Final() [16]byte {
	if c.h == nil {
		c.Init()
	}
	var digest [16]byte
	sum := c.h.Sum(nil)
	copy(digest[:], sum)
	c.h.Reset()
	return digest
}

// Accumulate — port of FLAC__MD5Accumulate (md5.c:497). Converts
// `samples` per-channel samples-per-channel of the supplied signal
// into a little-endian byte stream and folds the bytes into the hash.
//
// `signal` is libFLAC's `const FLAC__int32 * const signal[]` —
// per-channel buffers, NOT interleaved. Samples are sign-extended
// from `bytesPerSample*8` bits; the encoder uses the in-stream bits-
// per-sample, so e.g. a 16-bit stream is fed with bytesPerSample=2.
//
// Returns false on parameter overflow (matching libFLAC), true
// otherwise.
func (c *MD5Context) Accumulate(signal [][]int32, channels, samples, bytesPerSample uint32) bool {
	if c.h == nil {
		c.Init()
	}
	if channels == 0 || samples == 0 || bytesPerSample == 0 {
		return true
	}
	// Overflow guards mirroring libFLAC's checks. Go's int is 64-bit
	// on every supported target so we use uint64 for the arithmetic.
	chMul := uint64(channels) * uint64(bytesPerSample)
	if uint64(channels) > ^uint64(0)/uint64(bytesPerSample) {
		return false
	}
	if chMul > ^uint64(0)/uint64(samples) {
		return false
	}
	bytesNeeded := chMul * uint64(samples)
	if uint64(cap(c.scratch)) < bytesNeeded {
		c.scratch = make([]byte, bytesNeeded)
	} else {
		c.scratch = c.scratch[:bytesNeeded]
	}
	formatInput(c.scratch, signal, channels, samples, bytesPerSample)
	c.h.Write(c.scratch)
	return true
}

// formatInput is the merged hot/cold path of libFLAC's format_input_
// (md5.c:280). The C version unrolls the most common
// (bytes_per_sample, channels) combinations as separate switch arms —
// that's a hand optimisation aimed at avoiding the inner channel
// loop's branch. The Go translation keeps only the general path
// (which produces the same bytes) since the modern compiler keeps the
// unrolled versions fast enough; if profiling later shows a
// regression on multi-channel high-bit-depth payloads we can re-add
// the unrolls.
//
// Storage in the output is little-endian, per the FLAC spec.
func formatInput(out []byte, signal [][]int32, channels, samples, bytesPerSample uint32) {
	switch bytesPerSample {
	case 1:
		off := 0
		for s := uint32(0); s < samples; s++ {
			for ch := uint32(0); ch < channels; ch++ {
				out[off] = byte(signal[ch][s])
				off++
			}
		}
	case 2:
		off := 0
		for s := uint32(0); s < samples; s++ {
			for ch := uint32(0); ch < channels; ch++ {
				binary.LittleEndian.PutUint16(out[off:off+2], uint16(int16(signal[ch][s])))
				off += 2
			}
		}
	case 3:
		off := 0
		for s := uint32(0); s < samples; s++ {
			for ch := uint32(0); ch < channels; ch++ {
				w := uint32(signal[ch][s])
				out[off] = byte(w)
				out[off+1] = byte(w >> 8)
				out[off+2] = byte(w >> 16)
				off += 3
			}
		}
	case 4:
		off := 0
		for s := uint32(0); s < samples; s++ {
			for ch := uint32(0); ch < channels; ch++ {
				binary.LittleEndian.PutUint32(out[off:off+4], uint32(signal[ch][s]))
				off += 4
			}
		}
	}
}
