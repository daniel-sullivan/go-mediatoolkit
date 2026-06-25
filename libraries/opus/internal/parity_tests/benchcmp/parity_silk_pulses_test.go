//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_SilkShellRoundtrip(t *testing.T) {
	r := rand.New(rand.NewSource(50))
	for trial := 0; trial < 200; trial++ {
		// 16 nonnegative pulse amplitudes.
		pulses := make([]int, 16)
		// Total must stay <= 16 because shell_code_table_offsets has 17 entries.
		total := 0
		budget := 1 + r.Intn(16)
		for i := range pulses {
			if total >= budget {
				break
			}
			v := r.Intn(budget - total + 1)
			pulses[i] = v
			total += v
		}
		bufSize := 256
		wPkt, wOut := cSilkShellRoundtrip(pulses, bufSize)
		gPkt, gOut := nativeopus.ExportTestSilkShellRoundtrip(pulses, bufSize)
		if !eqByteSlice(wPkt, gPkt) || !eqInt16Slice(wOut, gOut) {
			t.Fatalf("shell_roundtrip trial=%d pulses=%v\nC pkt[:16]=%x\nGo pkt[:16]=%x",
				trial, pulses, wPkt[:16], gPkt[:16])
		}
	}
}

// silk_decode_pulses without a valid encoder-generated bitstream is
// undefined behaviour: the shell decoder can propagate negative
// child-sum values through its recursive tree and index arrays out of
// range (same crash the C code exhibits on garbage input). Porting
// silk_encode_pulses is encoder-only (out of scope for Phase 6), and
// the roundtrip coverage in TestParity_SilkShellRoundtrip plus
// TestParity_SilkCodeSignsRoundtrip is sufficient to lock the
// bit-exact behaviour of the decoder-side halves. The integration-
// level parity (silk_decode_pulses exercised via opus_decode) will be
// covered in a later phase when the full SILK decoder path lands.

func TestParity_SilkCodeSignsRoundtrip(t *testing.T) {
	r := rand.New(rand.NewSource(52))
	// Encode random signs, then decode both sides, compare output.
	for _, length := range []int{80, 160, 320} {
		nb := length / 16
		for _, sig := range []int{0, 1, 2} {
			for _, qoff := range []int{0, 1} {
				for trial := 0; trial < 20; trial++ {
					pulses := make([]int8, length)
					sumP := make([]int, nb)
					// For each block, pick a pulse pattern and its sum.
					for b := 0; b < nb; b++ {
						for j := 0; j < 16; j++ {
							v := r.Intn(5) - 2
							pulses[b*16+j] = int8(v)
							if v != 0 {
								if v < 0 {
									sumP[b] += -v
								} else {
									sumP[b] += v
								}
							}
						}
					}
					bufSize := 128
					wPkt := cSilkEncodeSigns(pulses, length, sig, qoff, sumP, bufSize)
					gPkt := nativeopus.ExportTestSilkEncodeSigns(pulses, length, sig, qoff, sumP, bufSize)
					if !eqByteSlice(wPkt, gPkt) {
						t.Fatalf("encode_signs pkt mismatch length=%d sig=%d qoff=%d", length, sig, qoff)
					}
					// Decode: start with abs(pulses) since decode_signs
					// only flips positive entries' signs.
					start := make([]int16, length)
					for i, v := range pulses {
						if v < 0 {
							start[i] = int16(-v)
						} else {
							start[i] = int16(v)
						}
					}
					wDec := cSilkDecodeSigns(wPkt, start, length, sig, qoff, sumP)
					gDec := nativeopus.ExportTestSilkDecodeSigns(wPkt, start, length, sig, qoff, sumP)
					if !eqInt16Slice(wDec, gDec) {
						t.Fatalf("decode_signs length=%d sig=%d qoff=%d", length, sig, qoff)
					}
				}
			}
		}
	}
}

func eqByteSlice(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
