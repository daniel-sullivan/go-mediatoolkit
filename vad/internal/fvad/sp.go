package fvad

// This file ports src/vad/vad_sp.c — the specific signal-processing
// tools used by core.go: the by-2 downsampler that feeds the 16/32 kHz
// paths, and the smoothed feature-minimum tracker.

// Allpass filter coefficients, upper and lower, in Q13.
// Upper: 0.64, Lower: 0.17.
var kAllPassCoefsQ13 = [2]int16{5243, 1392}

const (
	kSmoothingDown = 6553  // 0.2 in Q15.
	kSmoothingUp   = 32439 // 0.99 in Q15.
)

// Downsampling ports WebRtcVad_Downsampling: downsample signalIn by a
// factor 2 (e.g. 32→16 or 16→8 kHz) into signalOut (inLength/2
// samples), carrying the two allpass filter states in filterState.
func Downsampling(signalIn, signalOut []int16, filterState []int32, inLength int) {
	tmp32_1 := filterState[0]
	tmp32_2 := filterState[1]
	halfLength := inLength >> 1

	// Filter coefficients in Q13, filter state in Q0.
	inIdx, outIdx := 0, 0
	for n := 0; n < halfLength; n++ {
		// All-pass filtering upper branch.
		tmp16_1 := int16((tmp32_1 >> 1) +
			((int32(kAllPassCoefsQ13[0]) * int32(signalIn[inIdx])) >> 14))
		signalOut[outIdx] = tmp16_1
		tmp32_1 = int32(signalIn[inIdx]) -
			((int32(kAllPassCoefsQ13[0]) * int32(tmp16_1)) >> 12)
		inIdx++

		// All-pass filtering lower branch.
		tmp16_2 := int16((tmp32_2 >> 1) +
			((int32(kAllPassCoefsQ13[1]) * int32(signalIn[inIdx])) >> 14))
		signalOut[outIdx] += tmp16_2 // int16 wrap matches the C int truncation
		outIdx++
		tmp32_2 = int32(signalIn[inIdx]) -
			((int32(kAllPassCoefsQ13[1]) * int32(tmp16_2)) >> 12)
		inIdx++
	}

	// Store the filter states.
	filterState[0] = tmp32_1
	filterState[1] = tmp32_2
}

// FindMinimum ports WebRtcVad_FindMinimum: insert featureValue into the
// per-channel 16-slot window of the smallest values seen over the last
// 100 frames, then return the smoothed median of the five smallest.
// While self.FrameCounter is zero (no "valid" data yet) the smoothing
// input is the default 1600 with alpha 0.
func FindMinimum(self *Inst, featureValue int16, channel int) int16 {
	position := -1
	currentMedian := int16(1600)
	alpha := int16(0)

	// The 16 minimum values and the age of each, for this channel.
	offset := channel << 4
	age := self.IndexVector[offset : offset+16]
	smallestValues := self.LowValueVector[offset : offset+16]

	// Each value in smallestValues gets 1 loop older. Update age, and
	// remove old values.
	for i := 0; i < 16; i++ {
		if age[i] != 100 {
			age[i]++
		} else {
			// Too old value. Remove from memory and shift larger values
			// downwards. NOTE: the C shift loop runs j = i..15 and on
			// its final iteration reads index 16 — one past the
			// channel's window — into slot 15, then unconditionally
			// overwrites slot 15 below, so the out-of-window read is
			// dead. The port stops the shift at j < 15; the resulting
			// state is bit-identical (and, for channel 5, avoids
			// reading past the array the C silently strays into).
			for j := i; j < 15; j++ {
				smallestValues[j] = smallestValues[j+1]
				age[j] = age[j+1]
			}
			age[15] = 101
			smallestValues[15] = 10000
		}
	}

	// Check if featureValue is smaller than any of the values in
	// smallestValues. If so, find the position to insert the new value.
	if featureValue < smallestValues[7] {
		if featureValue < smallestValues[3] {
			if featureValue < smallestValues[1] {
				if featureValue < smallestValues[0] {
					position = 0
				} else {
					position = 1
				}
			} else if featureValue < smallestValues[2] {
				position = 2
			} else {
				position = 3
			}
		} else if featureValue < smallestValues[5] {
			if featureValue < smallestValues[4] {
				position = 4
			} else {
				position = 5
			}
		} else if featureValue < smallestValues[6] {
			position = 6
		} else {
			position = 7
		}
	} else if featureValue < smallestValues[15] {
		if featureValue < smallestValues[11] {
			if featureValue < smallestValues[9] {
				if featureValue < smallestValues[8] {
					position = 8
				} else {
					position = 9
				}
			} else if featureValue < smallestValues[10] {
				position = 10
			} else {
				position = 11
			}
		} else if featureValue < smallestValues[13] {
			if featureValue < smallestValues[12] {
				position = 12
			} else {
				position = 13
			}
		} else if featureValue < smallestValues[14] {
			position = 14
		} else {
			position = 15
		}
	}

	// If we have detected a new small value, insert it at the correct
	// position and shift larger values up.
	if position > -1 {
		for i := 15; i > position; i-- {
			smallestValues[i] = smallestValues[i-1]
			age[i] = age[i-1]
		}
		smallestValues[position] = featureValue
		age[position] = 1
	}

	// Get currentMedian.
	if self.FrameCounter > 2 {
		currentMedian = smallestValues[2]
	} else if self.FrameCounter > 0 {
		currentMedian = smallestValues[0]
	}

	// Smooth the median value.
	if self.FrameCounter > 0 {
		if currentMedian < self.MeanValue[channel] {
			alpha = kSmoothingDown // 0.2 in Q15.
		} else {
			alpha = kSmoothingUp // 0.99 in Q15.
		}
	}
	tmp32 := (int32(alpha) + 1) * int32(self.MeanValue[channel])
	tmp32 += (splWord16Max - int32(alpha)) * int32(currentMedian)
	tmp32 += 16384
	self.MeanValue[channel] = int16(tmp32 >> 15)

	return self.MeanValue[channel]
}
