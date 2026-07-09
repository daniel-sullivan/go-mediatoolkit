//go:build cgo && aec_oracle

package adaptivefir

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// lcg is the same PRNG the other slices use; both sides consume
// identical Go-generated inputs, so quality is irrelevant.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

const (
	numRenderChannels        = 2
	maxSizePartitions        = 12
	initialSizePartitions    = 4
	sizeChangeDurationBlocks = 10
	sampleRateHz             = 16000
)

// goRenderHarness drives a Go RenderDelayBuffer the same minimal way
// the C++ shim drives its webrtc::RenderDelayBuffer (see shim.cc):
// Insert then PrepareCaptureProcessing every block, with no delay
// controller/estimation involved -- neither AdaptiveFirFilter nor
// ComputeErl depends on delay estimation quality.
type goRenderHarness struct {
	rb *aec3.RenderDelayBuffer
}

func newGoRenderHarness() *goRenderHarness {
	return &goRenderHarness{rb: aec3.NewRenderDelayBuffer(config.DefaultConfig(), sampleRateHz, numRenderChannels)}
}

func (h *goRenderHarness) insertBlock(samples []float32) {
	block := aec3.NewBlock(1, numRenderChannels)
	for ch := 0; ch < numRenderChannels; ch++ {
		copy(block.View(0, ch), samples[ch*aec3.BlockSize:(ch+1)*aec3.BlockSize])
	}
	h.rb.Insert(block)
	h.rb.PrepareCaptureProcessing()
}

func requireFloatSliceEqual(t *testing.T, iter int, name string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("iter %d: %s length mismatch: go %d, c %d", iter, name, len(want), len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("iter %d: %s[%d] mismatch: go %v, c %v", iter, name, i, want[i], got[i])
		}
	}
}

// TestAdaptiveFirFilterParity drives the oracle's AdaptiveFirFilter
// (forced scalar via Aec3Optimization::kNone) and this port's
// AdaptiveFirFilter with identical PRNG render blocks and PRNG
// adaptation gains over hundreds of blocks, carrying filter state
// across calls, requiring Filter's output, Adapt's updated impulse
// response, ComputeFrequencyResponse and ComputeErl to agree
// bit-for-bit throughout -- including HandleEchoPathChange,
// SetSizePartitions and ScaleFilter perturbations at intervals.
func TestAdaptiveFirFilterParity(t *testing.T) {
	goHarness := newGoRenderHarness()
	cHarness := newFilterC(maxSizePartitions, initialSizePartitions, sizeChangeDurationBlocks, numRenderChannels)
	defer cHarness.close()

	goFilter := aec3.NewAdaptiveFirFilter(maxSizePartitions, initialSizePartitions, sizeChangeDurationBlocks, numRenderChannels)

	if got, want := cHarness.maxFilterSizePartitions(), goFilter.MaxFilterSizePartitions(); got != want {
		t.Fatalf("MaxFilterSizePartitions mismatch: go %d, c %d", want, got)
	}
	if got, want := cHarness.sizePartitions(), goFilter.SizePartitions(); got != want {
		t.Fatalf("initial SizePartitions mismatch: go %d, c %d", want, got)
	}

	renderRng := lcg(0xA5A5A5A5)
	gainRng := lcg(0x5A5A5A5A)

	var goImpulseResponse []float32
	impulseResponseCap := aec3.GetTimeDomainLength(maxSizePartitions)

	const numBlocks = 600
	for iter := 0; iter < numBlocks; iter++ {
		samples := make([]float32, numRenderChannels*aec3.BlockSize)
		for i := range samples {
			samples[i] = renderRng.next() * 4000
		}
		goHarness.insertBlock(samples)
		cHarness.insertRenderBlock(samples)

		if got, want := cHarness.sizePartitions(), goFilter.SizePartitions(); got != want {
			t.Fatalf("iter %d: SizePartitions mismatch: go %d, c %d", iter, want, got)
		}

		var sGo aec3.FFTData
		goFilter.Filter(goHarness.rb.GetRenderBuffer(), &sGo)
		cRe, cIm := cHarness.filter()
		requireFloatSliceEqual(t, iter, "Filter.Re", sGo.Re[:], cRe[:])
		requireFloatSliceEqual(t, iter, "Filter.Im", sGo.Im[:], cIm[:])

		var gRe, gIm [65]float32
		for k := range gRe {
			gRe[k] = (gainRng.next() - 0.5) * 0.02
			gIm[k] = (gainRng.next() - 0.5) * 0.02
		}
		gIm[0] = 0
		gIm[64] = 0
		gGo := aec3.FFTData{Re: gRe, Im: gIm}

		if iter%3 == 0 {
			goFilter.AdaptAndUpdateImpulseResponse(goHarness.rb.GetRenderBuffer(), &gGo, &goImpulseResponse)
			cIR := cHarness.adapt(gRe, gIm, impulseResponseCap)
			requireFloatSliceEqual(t, iter, "ImpulseResponse", goImpulseResponse, cIR)
		} else {
			goFilter.Adapt(goHarness.rb.GetRenderBuffer(), &gGo)
			cHarness.adaptNoIR(gRe, gIm)
		}

		if iter%17 == 5 {
			var h2Go [][65]float32
			goFilter.ComputeFrequencyResponse(&h2Go)
			h2C := cHarness.computeFrequencyResponse(maxSizePartitions)
			if len(h2Go) != len(h2C) {
				t.Fatalf("iter %d: ComputeFrequencyResponse partition count mismatch: go %d, c %d", iter, len(h2Go), len(h2C))
			}
			for p := range h2Go {
				requireFloatSliceEqual(t, iter, "H2", h2Go[p][:], h2C[p][:])
			}

			erlGo := make([]float32, 65)
			aec3.ComputeErl(h2Go, erlGo)
			erlC := computeErlC(h2C)
			requireFloatSliceEqual(t, iter, "Erl", erlGo, erlC[:])
		}

		switch iter {
		case 150, 400:
			goFilter.HandleEchoPathChange()
			cHarness.handleEchoPathChange()
		case 200:
			goFilter.SetSizePartitions(8, false)
			cHarness.setSizePartitions(8, false)
		case 250:
			goFilter.SetSizePartitions(3, true)
			cHarness.setSizePartitions(3, true)
		case 500:
			goFilter.ScaleFilter(0.5)
			cHarness.scaleFilter(0.5)
		}
	}
}
