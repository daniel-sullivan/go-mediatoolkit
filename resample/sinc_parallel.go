package resample

import (
	"math"
	"runtime"
	"sync"
)

// parallelThreshold is the minimum number of output frames in a batch
// before we parallelize. Below this, goroutine overhead dominates.
const parallelThreshold = 256

// sincWorkItem holds the pre-computed parameters needed to generate one
// output frame. All fields are values — no shared mutable state.
type sincWorkItem struct {
	bCurrent         int
	inc              increment
	startFilterIndex increment
	scale            float64
	outOffset        int // index into DataOut
}

// processLoopParallel is the parallel variant of processLoop. It runs the
// sequential position computation and buffer management in the calling
// goroutine, then fans out the expensive calcOutput convolutions across
// worker goroutines.
func (s *sincConverter) processLoopParallel(d *Data) error {
	channels := s.channels
	inputFrames := len(d.DataIn) / channels
	outputFrames := len(d.DataOut) / channels

	s.inCount = inputFrames * channels
	s.outCount = outputFrames * channels
	s.inUsed = 0
	s.outGen = 0

	srcRatio := s.lastRatio
	if isBadRatio(srcRatio) {
		return ErrBadInternalState
	}

	count := float64(s.coeffHalfLen+2) / float64(s.indexInc)
	minRatio := math.Min(s.lastRatio, d.Ratio.Float64())
	if minRatio < 1.0 {
		count /= minRatio
	}
	halfFilterChanLen := channels * (lrint(count) + 1)

	inputIndex := s.lastPosition
	rem := fmodOne(inputIndex)
	s.bCurrent = (s.bCurrent + channels*lrint(inputIndex-rem)) % s.bLen
	inputIndex = rem

	terminate := 1.0/srcRatio + 1e-20

	outCount := s.outCount
	indexInc := s.indexInc

	numWorkers := runtime.GOMAXPROCS(0)
	batch := make([]sincWorkItem, 0, 1024)
	done := false

	// Constant-ratio fast path.
	constantRatio := math.Abs(s.lastRatio-d.Ratio.Float64()) <= 1e-10
	var constFloatInc float64
	var constInc increment
	var constScale float64
	var constInvRatio float64
	if constantRatio {
		r := srcRatio
		if r > 1.0 {
			r = 1.0
		}
		constFloatInc = float64(indexInc) * r
		constInc = doubleToFP(constFloatInc)
		constScale = constFloatInc / float64(indexInc)
		constInvRatio = 1.0 / srcRatio
	}

	for s.outGen < outCount && !done {
		// Ensure the buffer has enough data.
		samplesInHand := s.bEnd - s.bCurrent
		if samplesInHand < 0 {
			samplesInHand += s.bLen
		}
		if samplesInHand <= halfFilterChanLen {
			if err := s.prepareData(d, halfFilterChanLen); err != nil {
				return err
			}
			samplesInHand = s.bEnd - s.bCurrent
			if samplesInHand < 0 {
				samplesInHand += s.bLen
			}
			if samplesInHand <= halfFilterChanLen {
				break
			}
		}

		// Collect a batch of work items for all output samples we can
		// produce before the buffer needs reloading.
		batch = batch[:0]

		for s.outGen < outCount {
			samplesInHand = s.bEnd - s.bCurrent
			if samplesInHand < 0 {
				samplesInHand += s.bLen
			}
			if samplesInHand <= halfFilterChanLen {
				break
			}

			if s.bRealEnd >= 0 {
				if s.isMono {
					if float64(s.bCurrent)+inputIndex+terminate > float64(s.bRealEnd) {
						done = true
						break
					}
				} else {
					if float64(s.bCurrent)+inputIndex+terminate >= float64(s.bRealEnd) {
						done = true
						break
					}
				}
			}

			var inc increment
			var startFilterIndex increment
			var scale float64
			var invRatio float64

			if constantRatio {
				inc = constInc
				startFilterIndex = doubleToFP(inputIndex * constFloatInc)
				scale = constScale
				invRatio = constInvRatio
			} else {
				if outCount > 0 {
					srcRatio = s.lastRatio + float64(s.outGen)*(d.Ratio.Float64()-s.lastRatio)/float64(outCount)
				}
				r := srcRatio
				if r > 1.0 {
					r = 1.0
				}
				floatIncrement := float64(indexInc) * r
				inc = doubleToFP(floatIncrement)
				startFilterIndex = doubleToFP(inputIndex * floatIncrement)
				scale = floatIncrement / float64(indexInc)
				invRatio = 1.0 / srcRatio
			}

			batch = append(batch, sincWorkItem{
				bCurrent:         s.bCurrent,
				inc:              inc,
				startFilterIndex: startFilterIndex,
				scale:            scale,
				outOffset:        s.outGen,
			})
			s.outGen += channels

			// Advance input position via floor — matches
			// libsamplerate's `lrint(input - fmod_one(input))`.
			// RoundToEven would skip a sample every iteration whose
			// fractional part lands in (0.5, 1.0). See sinc.go for the
			// long-form rationale and the parity oracle in
			// parity_oracle_test.go for the regression net.
			inputIndex += invRatio
			floor := math.Floor(inputIndex)
			inputIndex -= floor
			s.bCurrent += channels * int(floor)
			if s.bCurrent >= s.bLen {
				s.bCurrent -= s.bLen
			}
		}

		if len(batch) == 0 {
			break
		}

		// Execute the batch.
		if len(batch) < parallelThreshold || numWorkers <= 1 {
			// Small batch — run sequentially.
			if s.isMono {
				for i := range batch {
					w := &batch[i]
					s.calcOutputMono(w.bCurrent, w.inc, w.startFilterIndex, w.scale, d.DataOut[w.outOffset:])
				}
			} else {
				for i := range batch {
					w := &batch[i]
					s.calcOutputMulti(w.bCurrent, w.inc, w.startFilterIndex, w.scale, d.DataOut[w.outOffset:], s.leftCalc, s.rightCalc)
				}
			}
		} else {
			// Large batch — fan out across goroutines.
			s.executeBatchParallel(batch, d.DataOut, numWorkers)
		}
	}

	s.lastPosition = inputIndex
	s.lastRatio = srcRatio

	d.InputFramesUsed = s.inUsed / channels
	d.OutputFramesGen = s.outGen / channels
	return nil
}

// executeBatchParallel distributes work items across goroutines.
func (s *sincConverter) executeBatchParallel(batch []sincWorkItem, dataOut []float64, numWorkers int) {
	chunkSize := (len(batch) + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup

	if s.isMono {
		for i := 0; i < numWorkers; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if end > len(batch) {
				end = len(batch)
			}
			if start >= end {
				break
			}
			wg.Add(1)
			go func(items []sincWorkItem) {
				defer wg.Done()
				for j := range items {
					w := &items[j]
					s.calcOutputMono(w.bCurrent, w.inc, w.startFilterIndex, w.scale, dataOut[w.outOffset:])
				}
			}(batch[start:end])
		}
	} else {
		channels := s.channels
		for i := 0; i < numWorkers; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if end > len(batch) {
				end = len(batch)
			}
			if start >= end {
				break
			}
			wg.Add(1)
			go func(items []sincWorkItem) {
				defer wg.Done()
				// Each goroutine gets its own scratch space.
				lc := make([]float64, channels)
				rc := make([]float64, channels)
				for j := range items {
					w := &items[j]
					s.calcOutputMulti(w.bCurrent, w.inc, w.startFilterIndex, w.scale, dataOut[w.outOffset:], lc, rc)
				}
			}(batch[start:end])
		}
	}

	wg.Wait()
}
