//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_CeltEncode_Mono — encode a mono PCM burst with both C
// and Go celt_encode_with_ec; assert identical compressed output.
func TestParity_CeltEncode_Mono(t *testing.T) {
	cm, gm := buildFullGoMode(t)
	_ = cm

	for _, frame := range []int{120, 240, 480, 960} {
		for _, bitrate := range []int{32000, 64000, 128000} {
			// C encoder reference.
			cEnc := cCeltEncoderNew(48000, 1)
			if cEnc.p == nil {
				t.Fatalf("C encoder_new failed")
			}
			cEnc.SetStartBand(0)
			cEnc.SetEndBand(21)
			cEnc.SetBitrate(bitrate)
			cEnc.SetSignalling(0)
			cEnc.SetComplexity(5)

			// Go encoder mirror.
			gEnc, initRet := nativeopus.NewCeltEncoder(gm, 48000, 1)
			if initRet != nativeopus.OPUS_OK {
				t.Fatalf("Go encoder init: %d", initRet)
			}
			gEnc.SetStartEnd(0, 21)
			gEnc.SetSignalling(0)
			gEnc.SetBitrate(int32(bitrate))
			gEnc.SetComplexity(5)

			// Sinusoidal input.
			pcm := make([]float32, frame)
			for i := range pcm {
				pcm[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/48000))
			}

			bufSz := bitrate*frame/48000/8 + 32
			cBuf := make([]byte, bufSz)
			gBuf := make([]byte, bufSz)

			// C: encode with its own ec_enc so signalling=0 produces a
			// raw CELT bitstream. celt_encode_with_ec calls ec_enc_done
			// internally; do NOT call it again here — a double done
			// writes extra carry bytes into the middle of the buffer.
			cEc := cEcEncNew(cBuf)
			cN := cEnc.EncodeWithEc(pcm, frame, cBuf, cEc)
			cEc.Free()
			cEnc.Free()

			// Go: the Go encoder constructs its own ec_enc internally
			// when called without one.
			gN := nativeopus.ExportTestCeltEncodeWithEc(gEnc, pcm, frame, gBuf)
			if cN != gN {
				t.Errorf("frame=%d br=%d: nbCompressed C=%d Go=%d",
					frame, bitrate, cN, gN)
				continue
			}
			if !bytes.Equal(cBuf[:cN], gBuf[:gN]) {
				diff := -1
				for i := 0; i < cN; i++ {
					if cBuf[i] != gBuf[i] {
						diff = i
						break
					}
				}
				t.Errorf("frame=%d br=%d: %d bytes differ (first diff at %d)",
					frame, bitrate, cN, diff)
			}
		}
	}
}
