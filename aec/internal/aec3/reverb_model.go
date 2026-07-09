// This file ports modules/audio_processing/aec3/reverb_model.{h,cc}:
// ReverbModel, an estimate of the reverberant power spectrum of the
// echo.
package aec3

// ReverbModel models the reverberant power spectrum of the echo. C:
// reverb_model.{h,cc}'s ReverbModel.
type ReverbModel struct {
	reverb [FFTLengthBy2Plus1]float32
}

// Reset resets the reverb model. C: ReverbModel::Reset.
func (r *ReverbModel) Reset() {
	r.reverb = [FFTLengthBy2Plus1]float32{}
}

// Reverb returns the estimated power spectrum reverb. C:
// ReverbModel::reverb.
func (r *ReverbModel) Reverb() [FFTLengthBy2Plus1]float32 {
	return r.reverb
}

// UpdateReverbNoFreqShaping updates the reverb estimate, with the
// power spectrum scaling applied without any frequency shaping (a
// single scalar). C: ReverbModel::UpdateReverbNoFreqShaping.
func (r *ReverbModel) UpdateReverbNoFreqShaping(powerSpectrum []float32, powerSpectrumScaling, reverbDecay float32) {
	if reverbDecay > 0 {
		for k := range powerSpectrum {
			r.reverb[k] = mul32(add32(r.reverb[k], mul32(powerSpectrum[k], powerSpectrumScaling)), reverbDecay)
		}
	}
}

// UpdateReverb updates the reverb estimate, with the power spectrum
// scaling applied per frequency bin. C: ReverbModel::UpdateReverb.
func (r *ReverbModel) UpdateReverb(powerSpectrum []float32, powerSpectrumScaling [FFTLengthBy2Plus1]float32, reverbDecay float32) {
	if reverbDecay > 0 {
		for k := range powerSpectrum {
			r.reverb[k] = mul32(add32(r.reverb[k], mul32(powerSpectrum[k], powerSpectrumScaling[k])), reverbDecay)
		}
	}
}
