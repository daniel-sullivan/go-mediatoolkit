// This file ports modules/audio_processing/aec3/
// adaptive_fir_filter.{h,cc}: a partitioned-block frequency-domain FIR
// filter (the "H_" partitions), adapted via a supplied frequency-domain
// gain G and evaluated against the render buffer's circular FFT
// history.
//
// Only the scalar kernels (aec3::ComputeFrequencyResponse,
// aec3::AdaptPartitions, aec3::ApplyFilter) are ported — see
// common.go's doc comment: this whole package is scalar-only, so the
// Neon/Sse2/Avx2 variants and the Aec3Optimization dispatch in
// AdaptiveFirFilter's constructor/Filter/Adapt/ComputeFrequencyResponse
// are dropped entirely, matching the established convention (e.g.
// MatchedFilter, EchoPathDelayEstimator) of also dropping the
// ApmDataDumper logging parameter.
package aec3

// computeFrequencyResponse computes and stores the frequency response
// of the filter (the per-bin max power across partitions/channels). C:
// aec3::ComputeFrequencyResponse (scalar).
func computeFrequencyResponse(numPartitions int, h [][]FFTData, h2 [][FFTLengthBy2Plus1]float32) {
	for i := range h2 {
		h2[i] = [FFTLengthBy2Plus1]float32{}
	}

	numRenderChannels := len(h[0])
	for p := 0; p < numPartitions; p++ {
		for ch := 0; ch < numRenderChannels; ch++ {
			for j := 0; j < FFTLengthBy2Plus1; j++ {
				tmp := muladd(h[p][ch].Re[j], h[p][ch].Re[j], h[p][ch].Im[j], h[p][ch].Im[j])
				if tmp > h2[p][j] {
					h2[p][j] = tmp
				}
			}
		}
	}
}

// adaptPartitions adapts the filter partitions as H(t+1)=H(t)+G(t)*conj(X(t)).
// C: aec3::AdaptPartitions (scalar).
func adaptPartitions(renderBuffer *RenderBuffer, g *FFTData, numPartitions int, h [][]FFTData) {
	renderBufferData := renderBuffer.GetFftBuffer()
	index := renderBuffer.Position()
	numRenderChannels := len(renderBufferData[index])
	for p := 0; p < numPartitions; p++ {
		for ch := 0; ch < numRenderChannels; ch++ {
			xPCh := renderBufferData[index][ch]
			hPCh := &h[p][ch]
			for k := 0; k < FFTLengthBy2Plus1; k++ {
				hPCh.Re[k] += muladd(xPCh.Re[k], g.Re[k], xPCh.Im[k], g.Im[k])
				hPCh.Im[k] += mulsub(xPCh.Re[k], g.Im[k], xPCh.Im[k], g.Re[k])
			}
		}
		if index < len(renderBufferData)-1 {
			index++
		} else {
			index = 0
		}
	}
}

// applyFilter produces the filter output. C: aec3::ApplyFilter (scalar).
func applyFilter(renderBuffer *RenderBuffer, numPartitions int, h [][]FFTData, s *FFTData) {
	s.Re = [FFTLengthBy2Plus1]float32{}
	s.Im = [FFTLengthBy2Plus1]float32{}

	renderBufferData := renderBuffer.GetFftBuffer()
	index := renderBuffer.Position()
	numRenderChannels := len(renderBufferData[index])
	for p := 0; p < numPartitions; p++ {
		for ch := 0; ch < numRenderChannels; ch++ {
			xPCh := renderBufferData[index][ch]
			hPCh := &h[p][ch]
			for k := 0; k < FFTLengthBy2Plus1; k++ {
				s.Re[k] += mulsub(xPCh.Re[k], hPCh.Re[k], xPCh.Im[k], hPCh.Im[k])
				s.Im[k] += muladd(xPCh.Re[k], hPCh.Im[k], xPCh.Im[k], hPCh.Re[k])
			}
		}
		if index < len(renderBufferData)-1 {
			index++
		} else {
			index = 0
		}
	}
}

// zeroFilter clears H's partitions in [oldSize, newSize). C: (anonymous
// namespace) ZeroFilter.
func zeroFilter(oldSize, newSize int, h [][]FFTData) {
	for p := oldSize; p < newSize; p++ {
		for ch := range h[p] {
			h[p][ch].Clear()
		}
	}
}

// AdaptiveFirFilter provides a frequency domain adaptive filter
// functionality. Port of AdaptiveFirFilter (adaptive_fir_filter.{h,cc}).
type AdaptiveFirFilter struct {
	fft                           *Aec3FFT
	numRenderChannels             int
	maxSizePartitions             int
	sizeChangeDurationBlocks      int
	oneBySizeChangeDurationBlocks float32
	currentSizePartitions         int
	targetSizePartitions          int
	oldTargetSizePartitions       int
	sizeChangeCounter             int
	h                             [][]FFTData // [partition][channel]
	partitionToConstrain          int
}

// NewAdaptiveFirFilter mirrors AdaptiveFirFilter's C++ constructor
// (minus the optimization/data_dumper parameters — see this file's
// doc comment).
func NewAdaptiveFirFilter(maxSizePartitions, initialSizePartitions, sizeChangeDurationBlocks, numRenderChannels int) *AdaptiveFirFilter {
	f := &AdaptiveFirFilter{
		fft:                      NewAec3FFT(),
		numRenderChannels:        numRenderChannels,
		maxSizePartitions:        maxSizePartitions,
		sizeChangeDurationBlocks: sizeChangeDurationBlocks,
		currentSizePartitions:    initialSizePartitions,
		targetSizePartitions:     initialSizePartitions,
		oldTargetSizePartitions:  initialSizePartitions,
	}
	f.oneBySizeChangeDurationBlocks = 1.0 / float32(sizeChangeDurationBlocks)

	f.h = make([][]FFTData, maxSizePartitions)
	for p := range f.h {
		f.h[p] = make([]FFTData, numRenderChannels)
	}
	zeroFilter(0, maxSizePartitions, f.h)

	f.SetSizePartitions(f.currentSizePartitions, true)
	return f
}

// HandleEchoPathChange receives reports that known echo path changes
// have occurred and adjusts the filter adaptation accordingly. C:
// AdaptiveFirFilter::HandleEchoPathChange.
func (f *AdaptiveFirFilter) HandleEchoPathChange() {
	zeroFilter(f.currentSizePartitions, f.maxSizePartitions, f.h)
}

// SizePartitions returns the filter size. C:
// AdaptiveFirFilter::SizePartitions.
func (f *AdaptiveFirFilter) SizePartitions() int { return f.currentSizePartitions }

// SetSizePartitions sets the filter size. C:
// AdaptiveFirFilter::SetSizePartitions.
func (f *AdaptiveFirFilter) SetSizePartitions(size int, immediateEffect bool) {
	f.targetSizePartitions = size
	if f.targetSizePartitions > f.maxSizePartitions {
		f.targetSizePartitions = f.maxSizePartitions
	}
	if immediateEffect {
		oldSizePartitions := f.currentSizePartitions
		f.currentSizePartitions = f.targetSizePartitions
		f.oldTargetSizePartitions = f.targetSizePartitions
		zeroFilter(oldSizePartitions, f.currentSizePartitions, f.h)

		if f.partitionToConstrain > f.currentSizePartitions-1 {
			f.partitionToConstrain = f.currentSizePartitions - 1
		}
		f.sizeChangeCounter = 0
	} else {
		f.sizeChangeCounter = f.sizeChangeDurationBlocks
	}
}

// max_filter_size_partitions returns the maximum number of partitions
// for the filter. C: AdaptiveFirFilter::max_filter_size_partitions.
func (f *AdaptiveFirFilter) MaxFilterSizePartitions() int { return f.maxSizePartitions }

// updateSize gradually updates the current filter size towards the
// target size. C: AdaptiveFirFilter::UpdateSize.
func (f *AdaptiveFirFilter) updateSize() {
	oldSizePartitions := f.currentSizePartitions
	if f.sizeChangeCounter > 0 {
		f.sizeChangeCounter--

		average := func(from, to, fromWeight float32) float32 {
			return mla(mul32(to, 1-fromWeight), from, fromWeight)
		}

		changeFactor := float32(f.sizeChangeCounter) * f.oneBySizeChangeDurationBlocks

		f.currentSizePartitions = int(average(float32(f.oldTargetSizePartitions), float32(f.targetSizePartitions), changeFactor))

		if f.partitionToConstrain > f.currentSizePartitions-1 {
			f.partitionToConstrain = f.currentSizePartitions - 1
		}
	} else {
		f.currentSizePartitions = f.targetSizePartitions
		f.oldTargetSizePartitions = f.targetSizePartitions
	}
	zeroFilter(oldSizePartitions, f.currentSizePartitions, f.h)
}

// Filter produces the output of the filter. C: AdaptiveFirFilter::Filter.
func (f *AdaptiveFirFilter) Filter(renderBuffer *RenderBuffer, s *FFTData) {
	applyFilter(renderBuffer, f.currentSizePartitions, f.h, s)
}

// adaptAndUpdateSize updates the filter size if needed and adapts the
// filter. C: AdaptiveFirFilter::AdaptAndUpdateSize.
func (f *AdaptiveFirFilter) adaptAndUpdateSize(renderBuffer *RenderBuffer, g *FFTData) {
	f.updateSize()
	adaptPartitions(renderBuffer, g, f.currentSizePartitions, f.h)
}

// Adapt adapts the filter and constrains the filter partitions in a
// cyclic manner (the non-impulse-response-reporting overload). C:
// AdaptiveFirFilter::Adapt (2-argument overload).
func (f *AdaptiveFirFilter) Adapt(renderBuffer *RenderBuffer, g *FFTData) {
	f.adaptAndUpdateSize(renderBuffer, g)
	f.constrain()
}

// AdaptAndUpdateImpulseResponse adapts the filter, constraining the
// filter partitions in a cyclic manner and updating the corresponding
// values in the supplied impulse response. C:
// AdaptiveFirFilter::Adapt (3-argument overload).
func (f *AdaptiveFirFilter) AdaptAndUpdateImpulseResponse(renderBuffer *RenderBuffer, g *FFTData, impulseResponse *[]float32) {
	f.adaptAndUpdateSize(renderBuffer, g)
	f.constrainAndUpdateImpulseResponse(impulseResponse)
}

// ComputeFrequencyResponse computes the frequency responses for the
// filter partitions. h2 is resized to the current size (C:
// AdaptiveFirFilter::ComputeFrequencyResponse).
func (f *AdaptiveFirFilter) ComputeFrequencyResponse(h2 *[][FFTLengthBy2Plus1]float32) {
	if len(*h2) != f.currentSizePartitions {
		*h2 = make([][FFTLengthBy2Plus1]float32, f.currentSizePartitions)
	}
	computeFrequencyResponse(f.currentSizePartitions, f.h, *h2)
}

// constrainAndUpdateImpulseResponse constrains the partition of the
// frequency domain filter to be limited in time via setting the
// relevant time-domain coefficients to zero and updates the
// corresponding values in an externally stored impulse response
// estimate. C: AdaptiveFirFilter::ConstrainAndUpdateImpulseResponse.
func (f *AdaptiveFirFilter) constrainAndUpdateImpulseResponse(impulseResponse *[]float32) {
	newLen := GetTimeDomainLength(f.currentSizePartitions)
	*impulseResponse = resizeF32Zero(*impulseResponse, newLen)
	var h [FFTLength]float32
	ir := *impulseResponse
	for i := f.partitionToConstrain * FFTLengthBy2; i < (f.partitionToConstrain+1)*FFTLengthBy2; i++ {
		ir[i] = 0
	}

	for ch := 0; ch < f.numRenderChannels; ch++ {
		f.fft.Ifft(&f.h[f.partitionToConstrain][ch], &h)

		const scale = float32(1.0) / FFTLengthBy2
		for i := 0; i < FFTLengthBy2; i++ {
			h[i] *= scale
		}
		for i := FFTLengthBy2; i < FFTLength; i++ {
			h[i] = 0
		}

		if ch == 0 {
			copy(ir[f.partitionToConstrain*FFTLengthBy2:], h[:FFTLengthBy2])
		} else {
			for k, j := 0, f.partitionToConstrain*FFTLengthBy2; k < FFTLengthBy2; k, j = k+1, j+1 {
				if abs32(ir[j]) < abs32(h[k]) {
					ir[j] = h[k]
				}
			}
		}

		f.fft.Fft(&h, &f.h[f.partitionToConstrain][ch])
	}

	if f.partitionToConstrain < f.currentSizePartitions-1 {
		f.partitionToConstrain++
	} else {
		f.partitionToConstrain = 0
	}
}

// constrain constrains a partition of the frequency domain filter to
// be limited in time via setting the relevant time-domain coefficients
// to zero. C: AdaptiveFirFilter::Constrain.
func (f *AdaptiveFirFilter) constrain() {
	var h [FFTLength]float32
	for ch := 0; ch < f.numRenderChannels; ch++ {
		f.fft.Ifft(&f.h[f.partitionToConstrain][ch], &h)

		const scale = float32(1.0) / FFTLengthBy2
		for i := 0; i < FFTLengthBy2; i++ {
			h[i] *= scale
		}
		for i := FFTLengthBy2; i < FFTLength; i++ {
			h[i] = 0
		}

		f.fft.Fft(&h, &f.h[f.partitionToConstrain][ch])
	}

	if f.partitionToConstrain < f.currentSizePartitions-1 {
		f.partitionToConstrain++
	} else {
		f.partitionToConstrain = 0
	}
}

// ScaleFilter scales the filter impulse response and spectrum by a
// factor. C: AdaptiveFirFilter::ScaleFilter.
func (f *AdaptiveFirFilter) ScaleFilter(factor float32) {
	for _, hP := range f.h {
		for ch := range hP {
			for k := range hP[ch].Re {
				hP[ch].Re[k] *= factor
			}
			for k := range hP[ch].Im {
				hP[ch].Im[k] *= factor
			}
		}
	}
}

// SetFilter sets the filter coefficients. C: AdaptiveFirFilter::SetFilter.
func (f *AdaptiveFirFilter) SetFilter(numPartitions int, h [][]FFTData) {
	minNumPartitions := f.currentSizePartitions
	if numPartitions < minNumPartitions {
		minNumPartitions = numPartitions
	}
	for p := 0; p < minNumPartitions; p++ {
		for ch := 0; ch < f.numRenderChannels; ch++ {
			f.h[p][ch].Re = h[p][ch].Re
			f.h[p][ch].Im = h[p][ch].Im
		}
	}
}

// GetFilter returns the filter coefficients. C: AdaptiveFirFilter::GetFilter.
func (f *AdaptiveFirFilter) GetFilter() [][]FFTData { return f.h }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// resizeF32Zero mirrors std::vector<float>::resize(n): if n < len(s),
// truncates (retaining spare capacity); if n > len(s), grows and
// value-initializes (zeroes) the newly appended elements — the C++
// standard mandates this on every growing resize() call regardless of
// whether the underlying storage is reused, so a plain Go re-slice
// (which would leave stale data in the tail) is not equivalent.
func resizeF32Zero(s []float32, n int) []float32 {
	old := len(s)
	if n <= cap(s) {
		s = s[:n]
	} else {
		grown := make([]float32, n)
		copy(grown, s)
		s = grown
	}
	if n > old {
		for i := old; i < n; i++ {
			s[i] = 0
		}
	}
	return s
}
