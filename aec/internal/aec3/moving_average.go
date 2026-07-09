// This file ports modules/audio_processing/aec3/moving_average.{h,cc}:
// MovingAverage, in the C++ source's nested webrtc::aec3 namespace
// (this whole package already occupies that scope, so it maps to a
// plain type here).
package aec3

// MovingAverage computes the moving average of the last memLen
// num-elem-wide vectors fed to Average, memLen inclusive of the
// current input. C: moving_average.{h,cc}'s MovingAverage.
type MovingAverage struct {
	numElem int
	memLen  int
	scaling float32
	memory  []float32
	memIdx  int
}

// NewMovingAverage constructs a MovingAverage over numElem-wide
// inputs, averaging over memLen vectors (including the current one).
// C: MovingAverage::MovingAverage.
func NewMovingAverage(numElem, memLen int) *MovingAverage {
	return &MovingAverage{
		numElem: numElem,
		memLen:  memLen - 1,
		scaling: 1.0 / float32(memLen),
		memory:  make([]float32, numElem*(memLen-1)),
	}
}

// Average computes the average over input and the numElem-wide
// vectors stored from the memLen-1 previous calls, writing the result
// into output. C: MovingAverage::Average.
func (m *MovingAverage) Average(input, output []float32) {
	copy(output, input[:m.numElem])

	for j := 0; j < m.memLen; j++ {
		base := j * m.numElem
		for k := 0; k < m.numElem; k++ {
			output[k] += m.memory[base+k]
		}
	}

	for k := 0; k < m.numElem; k++ {
		output[k] = mul32(output[k], m.scaling)
	}

	if m.memLen > 0 {
		copy(m.memory[m.memIdx*m.numElem:m.memIdx*m.numElem+m.numElem], input[:m.numElem])
		m.memIdx = (m.memIdx + 1) % m.memLen
	}
}
