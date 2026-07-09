// This file ports modules/audio_processing/aec3/decimator.{h,cc}: a
// signal decimator built from a cascaded-biquad anti-aliasing filter
// followed by a cascaded-biquad noise-reduction filter, followed by
// plain stride downsampling.
package aec3

// getLowPassFilterDS2 is the DS2 default anti-aliasing filter:
// signal.butter(2, 3400/8000.0, 'lowpass', analog=False), repeated 3x.
// C: decimator.cc's GetLowPassFilterDS2.
func getLowPassFilterDS2() []BiQuadParam {
	p := BiQuadParam{ZeroR: -1, ZeroI: 0, PoleR: 0.13833231, PoleI: 0.40743176, Gain: 0.22711796393486466}
	return []BiQuadParam{p, p, p}
}

// getLowPassFilterDS4 is the DS4 anti-aliasing filter:
// signal.ellip(6, 1, 40, 1800/8000, btype='lowpass', analog=False). C:
// decimator.cc's GetLowPassFilterDS4.
func getLowPassFilterDS4() []BiQuadParam {
	return []BiQuadParam{
		{ZeroR: -0.08873842, ZeroI: 0.99605496, PoleR: 0.75916227, PoleI: 0.23841065, Gain: 0.26250696827},
		{ZeroR: 0.62273832, ZeroI: 0.78243018, PoleR: 0.74892112, PoleI: 0.5410152, Gain: 0.26250696827},
		{ZeroR: 0.71107693, ZeroI: 0.70311421, PoleR: 0.74895534, PoleI: 0.63924616, Gain: 0.26250696827},
	}
}

// getBandPassFilterDS8 is the DS8 anti-aliasing filter:
// signal.cheby1(1, 6, [1000/8000, 2000/8000], btype='bandpass',
// analog=False). C: decimator.cc's GetBandPassFilterDS8.
func getBandPassFilterDS8() []BiQuadParam {
	p := BiQuadParam{ZeroR: 1, ZeroI: 0, PoleR: 0.7601815, PoleI: 0.46423542, Gain: 0.10330478266505948, MirrorZeroAlongIAxis: true}
	return []BiQuadParam{p, p, p, p, p}
}

// getHighPassFilter is the noise-reduction filter used for DS2/DS4:
// signal.butter(2, 1000/8000.0, 'highpass', analog=False). C:
// decimator.cc's GetHighPassFilter.
func getHighPassFilter() []BiQuadParam {
	return []BiQuadParam{
		{ZeroR: 1, ZeroI: 0, PoleR: 0.72712179, PoleI: 0.21296904, Gain: 0.7570763753338849},
	}
}

// Decimator downsamples a BlockSize signal by a factor of 2, 4, or 8,
// low/band-pass filtering to limit aliasing and optionally
// high-pass filtering to reduce near-end noise. Port of decimator.h/cc's
// Decimator.
type Decimator struct {
	downSamplingFactor   int
	antiAliasingFilter   *CascadedBiQuadFilter
	noiseReductionFilter *CascadedBiQuadFilter
}

// NewDecimator mirrors Decimator's C++ constructor; downSamplingFactor
// must be 2, 4, or 8.
func NewDecimator(downSamplingFactor int) *Decimator {
	var antiAliasing []BiQuadParam
	switch downSamplingFactor {
	case 4:
		antiAliasing = getLowPassFilterDS4()
	case 8:
		antiAliasing = getBandPassFilterDS8()
	default:
		antiAliasing = getLowPassFilterDS2()
	}

	var noiseReduction []BiQuadParam
	if downSamplingFactor != 8 {
		noiseReduction = getHighPassFilter()
	}

	return &Decimator{
		downSamplingFactor:   downSamplingFactor,
		antiAliasingFilter:   NewCascadedBiQuadFilter(antiAliasing),
		noiseReductionFilter: NewCascadedBiQuadFilter(noiseReduction),
	}
}

// Decimate downsamples in (length BlockSize) into out (length
// BlockSize/downSamplingFactor). C: Decimator::Decimate.
func (d *Decimator) Decimate(in []float32, out []float32) {
	var x [BlockSize]float32

	// Limit the frequency content of the signal to avoid aliasing.
	d.antiAliasingFilter.Process(in, x[:])

	// Reduce the impact of near-end noise.
	d.noiseReductionFilter.ProcessInPlace(x[:])

	// Downsample the signal.
	for j, k := 0, 0; j < len(out); j, k = j+1, k+d.downSamplingFactor {
		out[j] = x[k]
	}
}
