// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psdecparse

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// TestPsParseInt8Parity is the HE-AAC v2 PS bitstream-parse parity gate. It
// drives BOTH the genuine vendored ReadPsData + DecodePs (psbitdec.cpp, via
// bridge.cpp) and the pure-Go sbr.PsParse over the IDENTICAL raw ps_data payload
// bytes, and asserts the dequantized + 34<->20-mapped IID/ICC index arrays and
// the resolved envelope borders are EXACTLY equal.
//
// The payloads are deterministic pseudo-random byte buffers. The PS parser is a
// fixed Huffman walk + SCHAR delta decode over whatever codewords the bits form;
// since both decoders consume the same bytes, this is a rigorous differential
// test of the parse/delta/mapping math. Both prevDecoded states and both frame
// sizes (960/1024 -> 30/32 subsamples) are exercised, and the previous-frame
// concealment path is reached when a payload's header bit is clear.
func TestPsParseInt8Parity(t *testing.T) {
	const bufBytes = 256 // power of two; comfortably larger than any ps_data
	r := rand.New(rand.NewSource(20260611))

	cases := []struct {
		name         string
		noSubSamples int
	}{
		{"frame1024", 32},
		{"frame960", 30},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const iters = 4000
			for it := 0; it < iters; it++ {
				payload := make([]byte, bufBytes)
				// Fill the first ~16 bytes with random bits (a ps_data element is at
				// most ~120 bits for the baseline 20-band coarse config, more for
				// 34-band fine; 16 bytes = 128 bits covers the parse comfortably,
				// the rest stays zero as fill).
				nRand := 8 + r.Intn(12)
				for i := 0; i < nRand; i++ {
					payload[i] = byte(r.Intn(256))
				}
				validBits := uint32(bufBytes * 8)

				prevDecoded := r.Intn(2)
				frameError := 0 // exercise the normal decode path

				cOut := cPsParse(payload, int(validBits), tc.noSubSamples, prevDecoded, frameError)
				goOut := sbr.PsParse(payload, uint32(bufBytes), validBits, tc.noSubSamples, prevDecoded, frameError)

				require.Equalf(t, cOut.psProcessFlag, goOut.PsProcessFlag, "it=%d psProcessFlag", it)
				require.Equalf(t, cOut.bitsRead, goOut.BitsRead, "it=%d bitsRead", it)
				require.Equalf(t, cOut.noEnv, goOut.NoEnv, "it=%d noEnv", it)
				require.Equalf(t, cOut.freqResIid, goOut.FreqResIid, "it=%d freqResIid", it)
				require.Equalf(t, cOut.freqResIcc, goOut.FreqResIcc, "it=%d freqResIcc", it)
				require.Equalf(t, cOut.bFineIidQ, goOut.BFineIidQ, "it=%d bFineIidQ", it)
				require.Equalf(t, cOut.envStartStop, goOut.EnvStartStop, "it=%d envStartStop", it)
				require.Equalf(t, cOut.iidMapped, goOut.IidMapped, "it=%d iidMapped", it)
				require.Equalf(t, cOut.iccMapped, goOut.IccMapped, "it=%d iccMapped", it)
			}
		})
	}
}
