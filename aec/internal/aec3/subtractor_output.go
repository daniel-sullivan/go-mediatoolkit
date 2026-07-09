// This file ports modules/audio_processing/aec3/
// subtractor_output.{h,cc}: the values returned from the echo
// subtractor for a single capture channel.
package aec3

// SubtractorOutput stores the values being returned from the echo
// subtractor for a single capture channel. Port of
// subtractor_output.{h,cc}'s SubtractorOutput.
type SubtractorOutput struct {
	SRefined        [BlockSize]float32
	SCoarse         [BlockSize]float32
	ERefinedTime    [BlockSize]float32
	ECoarseTime     [BlockSize]float32
	ERefined        FFTData
	E2Refined       [FFTLengthBy2Plus1]float32
	E2Coarse        [FFTLengthBy2Plus1]float32
	S2Refined       float32
	S2Coarse        float32
	E2RefinedScalar float32
	E2CoarseScalar  float32
	Y2              float32
	SRefinedMaxAbs  float32
	SCoarseMaxAbs   float32
}

// Reset resets the struct content. C: SubtractorOutput::Reset.
func (o *SubtractorOutput) Reset() {
	o.SRefined = [BlockSize]float32{}
	o.SCoarse = [BlockSize]float32{}
	o.ERefinedTime = [BlockSize]float32{}
	o.ECoarseTime = [BlockSize]float32{}
	o.ERefined.Re = [FFTLengthBy2Plus1]float32{}
	o.ERefined.Im = [FFTLengthBy2Plus1]float32{}
	o.E2Refined = [FFTLengthBy2Plus1]float32{}
	o.E2Coarse = [FFTLengthBy2Plus1]float32{}
	o.E2RefinedScalar = 0
	o.E2CoarseScalar = 0
	o.S2Refined = 0
	o.S2Coarse = 0
	o.Y2 = 0
}

// ComputeMetrics updates the powers of the signals. C:
// SubtractorOutput::ComputeMetrics.
func (o *SubtractorOutput) ComputeMetrics(y []float32) {
	sumOfSquares := func(s []float32) float32 {
		var sum float32
		for _, v := range s {
			sum = mla(sum, v, v)
		}
		return sum
	}

	o.Y2 = sumOfSquares(y)
	o.E2RefinedScalar = sumOfSquares(o.ERefinedTime[:])
	o.E2CoarseScalar = sumOfSquares(o.ECoarseTime[:])
	o.S2Refined = sumOfSquares(o.SRefined[:])
	o.S2Coarse = sumOfSquares(o.SCoarse[:])

	minMaxAbs := func(s []float32) float32 {
		maxV, minV := s[0], s[0]
		for _, v := range s {
			if v > maxV {
				maxV = v
			}
			if v < minV {
				minV = v
			}
		}
		return max32(maxV, -minV)
	}
	o.SRefinedMaxAbs = minMaxAbs(o.SRefined[:])
	o.SCoarseMaxAbs = minMaxAbs(o.SCoarse[:])
}
