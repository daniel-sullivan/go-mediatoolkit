//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_CeltPreemphasis(t *testing.T) {
	cm, _ := buildFullGoMode(t)
	preemph := cModePreemph(cm)
	for _, frame := range []int{120, 240, 480, 960} {
		pcm := make([]float32, frame)
		for i := range pcm {
			pcm[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/48000))
		}
		cOut := make([]float32, frame)
		gOut := make([]float32, frame)
		cMem := float32(0)
		gMem := float32(0)
		cCeltPreemphasis(pcm, cOut, frame, 1, 1, preemph, &cMem, 0)
		nativeopus.ExportTestCeltPreemphasis(pcm, gOut, frame, 1, 1, preemph, &gMem, 0)
		for i := 0; i < frame; i++ {
			if cOut[i] != gOut[i] {
				t.Errorf("frame=%d [%d]: C=%g Go=%g (%d ULP)",
					frame, i, cOut[i], gOut[i], ulpDiffF32(cOut[i], gOut[i]))
				break
			}
		}
		if cMem != gMem {
			t.Errorf("frame=%d: mem C=%g Go=%g", frame, cMem, gMem)
		}
	}
}
