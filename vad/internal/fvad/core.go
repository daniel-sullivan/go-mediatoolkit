package fvad

// This file ports src/vad/vad_core.c — the core VAD state, the GMM
// hypothesis test, model adaptation, and the per-rate CalcVad entry
// points.

// Structure dimensions, mirroring vad_core.h's enums.
const (
	NumChannels  = 6 // Number of frequency bands (named channels).
	NumGaussians = 2 // Number of Gaussians per channel in the GMM.
	TableSize    = NumChannels * NumGaussians
	MinEnergy    = 10 // Minimum energy required to trigger audio signal.
)

// Inst mirrors VadInstT. Fields are exported so the parity slices can
// snapshot and compare the complete state against the C oracle after
// every processed frame.
type Inst struct {
	Vad                      int
	DownsamplingFilterStates [4]int32
	State48To8               State48khzTo8khz
	NoiseMeans               [TableSize]int16
	SpeechMeans              [TableSize]int16
	NoiseStds                [TableSize]int16
	SpeechStds               [TableSize]int16
	FrameCounter             int32
	OverHang                 int16
	NumOfSpeech              int16
	IndexVector              [16 * NumChannels]int16
	LowValueVector           [16 * NumChannels]int16
	MeanValue                [NumChannels]int16
	UpperState               [5]int16
	LowerState               [5]int16
	HpFilterState            [4]int16
	OverHangMax1             [3]int16
	OverHangMax2             [3]int16
	Individual               [3]int16
	Total                    [3]int16

	FeatureVector [NumChannels]int16
	TotalPower    int16

	InitFlag int
}

// Spectrum Weighting
var kSpectrumWeight = [NumChannels]int16{6, 8, 10, 12, 14, 16}

const (
	kNoiseUpdateConst  = int16(655)  // Q15
	kSpeechUpdateConst = int16(6554) // Q15
	kBackEta           = int16(154)  // Q8
)

// Minimum difference between the two models, Q5
var kMinimumDifference = [NumChannels]int16{544, 544, 576, 576, 576, 576}

// Upper limit of mean value for speech model, Q7
var kMaximumSpeech = [NumChannels]int16{11392, 11392, 11520, 11520, 11520, 11520}

// Minimum value for mean value
var kMinimumMean = [NumGaussians]int16{640, 768}

// Upper limit of mean value for noise model, Q7
var kMaximumNoise = [NumChannels]int16{9216, 9088, 8960, 8832, 8704, 8576}

// Start values for the Gaussian models, Q7:
var (
	// Weights for the two Gaussians for the six channels (noise)
	kNoiseDataWeights = [TableSize]int16{34, 62, 72, 66, 53, 25, 94, 66, 56, 62, 75, 103}
	// Weights for the two Gaussians for the six channels (speech)
	kSpeechDataWeights = [TableSize]int16{48, 82, 45, 87, 50, 47, 80, 46, 83, 41, 78, 81}
	// Means for the two Gaussians for the six channels (noise)
	kNoiseDataMeans = [TableSize]int16{6738, 4892, 7065, 6715, 6771, 3369, 7646, 3863, 7820, 7266, 5020, 4362}
	// Means for the two Gaussians for the six channels (speech)
	kSpeechDataMeans = [TableSize]int16{8306, 10085, 10078, 11823, 11843, 6309, 9473, 9571, 10879, 7581, 8180, 7483}
	// Stds for the two Gaussians for the six channels (noise)
	kNoiseDataStds = [TableSize]int16{378, 1064, 493, 582, 688, 593, 474, 697, 475, 688, 421, 455}
	// Stds for the two Gaussians for the six channels (speech)
	kSpeechDataStds = [TableSize]int16{555, 505, 567, 524, 585, 1231, 509, 828, 492, 1540, 1079, 850}
)

// Constants used in gmmProbability().
const (
	// Maximum number of counted speech (VAD = 1) frames in a row.
	kMaxSpeechFrames = int16(6)
	// Minimum standard deviation for both speech and noise.
	kMinStd = int16(384)
)

// Constants in InitCore().
const (
	kDefaultMode = 0
	kInitCheck   = 42
)

// Mode thresholds for the three frame lengths (10, 20 and 30 ms).
var (
	// Mode 0, Quality.
	kOverHangMax1Q    = [3]int16{8, 4, 3}
	kOverHangMax2Q    = [3]int16{14, 7, 5}
	kLocalThresholdQ  = [3]int16{24, 21, 24}
	kGlobalThresholdQ = [3]int16{57, 48, 57}
	// Mode 1, Low bitrate.
	kOverHangMax1LBR    = [3]int16{8, 4, 3}
	kOverHangMax2LBR    = [3]int16{14, 7, 5}
	kLocalThresholdLBR  = [3]int16{37, 32, 37}
	kGlobalThresholdLBR = [3]int16{100, 80, 100}
	// Mode 2, Aggressive.
	kOverHangMax1AGG    = [3]int16{6, 3, 2}
	kOverHangMax2AGG    = [3]int16{9, 5, 3}
	kLocalThresholdAGG  = [3]int16{82, 78, 82}
	kGlobalThresholdAGG = [3]int16{285, 260, 285}
	// Mode 3, Very aggressive.
	kOverHangMax1VAG    = [3]int16{6, 3, 2}
	kOverHangMax2VAG    = [3]int16{9, 5, 3}
	kLocalThresholdVAG  = [3]int16{94, 94, 94}
	kGlobalThresholdVAG = [3]int16{1100, 1050, 1100}
)

// weightedAverage ports WeightedAverage: the weighted (w.r.t. the
// Gaussians) average of one channel's two model means, with data
// updated by offset before averaging. The C receives &data[channel]
// and indexes k*kNumChannels; the port passes the full table plus the
// channel.
func weightedAverage(data []int16, channel int, offset int16, weights *[TableSize]int16) int32 {
	var avg int32
	for k := 0; k < NumGaussians; k++ {
		data[channel+k*NumChannels] += offset
		avg += int32(data[channel+k*NumChannels]) * int32(weights[channel+k*NumChannels])
	}
	return avg
}

// gmmProbability ports GmmProbability: calculate speech/noise
// probabilities with the two GMMs, run the local+global LRT hypothesis
// test, update the models, and return the raw VAD decision (0 = noise,
// >0 = speech).
func gmmProbability(self *Inst, features []int16, totalPower int16, frameLength int) int16 {
	vadflag := int16(0)
	var overhead1, overhead2, individualTest, totalTest int16

	// Set various thresholds based on frame length (80/160/240 samples).
	if frameLength == 80 {
		overhead1 = self.OverHangMax1[0]
		overhead2 = self.OverHangMax2[0]
		individualTest = self.Individual[0]
		totalTest = self.Total[0]
	} else if frameLength == 160 {
		overhead1 = self.OverHangMax1[1]
		overhead2 = self.OverHangMax2[1]
		individualTest = self.Individual[1]
		totalTest = self.Total[1]
	} else {
		overhead1 = self.OverHangMax1[2]
		overhead2 = self.OverHangMax2[2]
		individualTest = self.Individual[2]
		totalTest = self.Total[2]
	}

	if totalPower > MinEnergy {
		// The signal power of the current frame is large enough for
		// processing: 1) calculate the likelihoods and a VAD decision,
		// 2) update the underlying model w.r.t. the decision.
		//
		// The detection scheme is an LRT with hypotheses H0 (noise) and
		// H1 (speech), combining a global LRT with local per-band tests.
		var deltaN, deltaS [TableSize]int16
		var ngprvec, sgprvec [TableSize]int16 // Conditional probability = 0.
		var noiseProbability, speechProbability [NumGaussians]int32
		var sumLogLikelihoodRatios int32

		for channel := 0; channel < NumChannels; channel++ {
			h0Test := int32(0)
			h1Test := int32(0)
			for k := 0; k < NumGaussians; k++ {
				gaussian := channel + k*NumChannels

				// Probability under H0 (noise). Q27 = Q7 * Q20.
				prob, d := GaussianProbability(features[channel],
					self.NoiseMeans[gaussian], self.NoiseStds[gaussian])
				deltaN[gaussian] = d
				noiseProbability[k] = int32(kNoiseDataWeights[gaussian]) * prob
				h0Test += noiseProbability[k] // Q27

				// Probability under H1 (speech). Q27 = Q7 * Q20.
				prob, d = GaussianProbability(features[channel],
					self.SpeechMeans[gaussian], self.SpeechStds[gaussian])
				deltaS[gaussian] = d
				speechProbability[k] = int32(kSpeechDataWeights[gaussian]) * prob
				h1Test += speechProbability[k] // Q27
			}

			// Approximate the log likelihood ratio by
			// shifts_h0 - shifts_h1 (see the C source's derivation).
			shiftsH0 := NormW32(h0Test)
			shiftsH1 := NormW32(h1Test)
			if h0Test == 0 {
				shiftsH0 = 31
			}
			if h1Test == 0 {
				shiftsH1 = 31
			}
			logLikelihoodRatio := shiftsH0 - shiftsH1

			// Update the sum with spectrum weighting, for the global
			// decision.
			sumLogLikelihoodRatios += int32(logLikelihoodRatio) * int32(kSpectrumWeight[channel])

			// Local VAD decision.
			if int32(logLikelihoodRatio)*4 > int32(individualTest) {
				vadflag = 1
			}

			// Calculate local noise probabilities used later when
			// updating the GMM.
			h0 := int16(h0Test >> 12) // Q15
			if h0 > 0 {
				// High probability of noise: assign conditional
				// probabilities for each Gaussian.
				tmp1 := int32((uint32(noiseProbability[0]) & 0xFFFFF000) << 2) // Q29
				ngprvec[channel] = int16(DivW32W16(tmp1, h0))                  // Q14
				ngprvec[channel+NumChannels] = 16384 - ngprvec[channel]
			} else {
				// Low noise probability: conditional probability 1 for
				// the first Gaussian, 0 for the rest (initialized value).
				ngprvec[channel] = 16384
			}

			// Calculate local speech probabilities used later when
			// updating the GMM.
			h1 := int16(h1Test >> 12) // Q15
			if h1 > 0 {
				tmp1 := int32((uint32(speechProbability[0]) & 0xFFFFF000) << 2) // Q29
				sgprvec[channel] = int16(DivW32W16(tmp1, h1))                   // Q14
				sgprvec[channel+NumChannels] = 16384 - sgprvec[channel]
			}
		}

		// Make a global VAD decision.
		if sumLogLikelihoodRatios >= int32(totalTest) {
			vadflag |= 1
		}

		// Update the model parameters.
		maxspe := int16(12800)
		for channel := 0; channel < NumChannels; channel++ {

			// Get minimum value in past, for long term correction, Q4.
			featureMinimum := FindMinimum(self, features[channel], channel)

			// Compute the "global" mean (the sum of the two weighted
			// means).
			noiseGlobalMean := weightedAverage(self.NoiseMeans[:], channel, 0, &kNoiseDataWeights)
			tmp1S16 := int16(noiseGlobalMean >> 6) // Q8

			for k := 0; k < NumGaussians; k++ {
				gaussian := channel + k*NumChannels

				nmk := self.NoiseMeans[gaussian]
				smk := self.SpeechMeans[gaussian]
				nsk := self.NoiseStds[gaussian]
				ssk := self.SpeechStds[gaussian]

				// Update noise mean vector if the frame is noise only.
				nmk2 := nmk
				if vadflag == 0 {
					// deltaN = (x-mu)/sigma^2
					// (Q14 * Q11 >> 11) = Q14.
					delt := int16((int32(ngprvec[gaussian]) * int32(deltaN[gaussian])) >> 11)
					// Q7 + (Q14 * Q15 >> 22) = Q7.
					nmk2 = nmk + int16((int32(delt)*int32(kNoiseUpdateConst))>>22)
				}

				// Long term correction of the noise mean.
				// Q8 - Q8 = Q8.
				ndelt := int16((int32(featureMinimum) << 4) - int32(tmp1S16))
				// Q7 + (Q8 * Q8) >> 9 = Q7.
				nmk3 := nmk2 + int16((int32(ndelt)*int32(kBackEta))>>9)

				// Control that the noise mean does not drift too much.
				tmpS16 := int16((k + 5) << 7)
				if nmk3 < tmpS16 {
					nmk3 = tmpS16
				}
				tmpS16 = int16((72 + k - channel) << 7)
				if nmk3 > tmpS16 {
					nmk3 = tmpS16
				}
				self.NoiseMeans[gaussian] = nmk3

				if vadflag != 0 {
					// Update speech mean vector:
					// deltaS = (x-mu)/sigma^2
					// (Q14 * Q11) >> 11 = Q14.
					delt := int16((int32(sgprvec[gaussian]) * int32(deltaS[gaussian])) >> 11)
					// Q14 * Q15 >> 21 = Q8.
					tmpS16 = int16((int32(delt) * int32(kSpeechUpdateConst)) >> 21)
					// Q7 + (Q8 >> 1) = Q7. With rounding.
					smk2 := int16(int32(smk) + ((int32(tmpS16) + 1) >> 1))

					// Control that the speech mean does not drift too
					// much.
					maxmu := maxspe + 640
					if smk2 < kMinimumMean[k] {
						smk2 = kMinimumMean[k]
					}
					if smk2 > maxmu {
						smk2 = maxmu
					}
					self.SpeechMeans[gaussian] = smk2 // Q7.

					// (Q7 >> 3) = Q4. With rounding.
					tmpS16 = int16((int32(smk) + 4) >> 3)

					tmpS16 = int16(int32(features[channel]) - int32(tmpS16)) // Q4
					// (Q11 * Q4 >> 3) = Q12.
					tmp1S32 := (int32(deltaS[gaussian]) * int32(tmpS16)) >> 3
					tmp2S32 := tmp1S32 - 4096
					tmpS16 = sgprvec[gaussian] >> 2
					// (Q14 >> 2) * Q12 = Q24. (May wrap; so does the C.)
					tmp1S32 = int32(tmpS16) * tmp2S32

					tmp2S32 = tmp1S32 >> 4 // Q20

					// 0.1 * Q20 / Q7 = Q13. Note the C passes ssk * 10
					// through an int16_t parameter, truncating the
					// promoted product mod 2^16 — mirrored here.
					if tmp2S32 > 0 {
						tmpS16 = int16(DivW32W16(tmp2S32, int16(int32(ssk)*10)))
					} else {
						tmpS16 = int16(DivW32W16(-tmp2S32, int16(int32(ssk)*10)))
						tmpS16 = -tmpS16
					}
					// Divide by 4 giving an update factor of 0.025
					// (= 0.1 / 4). (Q13 >> 8) = (Q13 >> 6) / 4 = Q7.
					tmpS16 += 128 // Rounding.
					ssk += tmpS16 >> 8
					if ssk < kMinStd {
						ssk = kMinStd
					}
					self.SpeechStds[gaussian] = ssk
				} else {
					// Update GMM variance vectors.
					// deltaN * (features[channel] - nmk) - 1
					// Q4 - (Q7 >> 3) = Q4.
					tmpS16 = int16(int32(features[channel]) - int32(nmk>>3))
					// (Q11 * Q4 >> 3) = Q12.
					tmp1S32 := (int32(deltaN[gaussian]) * int32(tmpS16)) >> 3
					tmp1S32 -= 4096

					// (Q14 >> 2) * Q12 = Q24.
					tmpS16 = (ngprvec[gaussian] + 2) >> 2
					// C routes this via OverflowingMulS16ByS32ToS32 —
					// the product may wrap; Go's int32 wraps identically.
					tmp2S32 := int32(tmpS16) * tmp1S32
					// Q20 * approx 0.001 (2^-10=0.0009766):
					// (Q24 >> 14) = (Q24 >> 4) / 2^10 = Q20.
					tmp1S32 = tmp2S32 >> 14

					// Q20 / Q7 = Q13.
					if tmp1S32 > 0 {
						tmpS16 = int16(DivW32W16(tmp1S32, nsk))
					} else {
						tmpS16 = int16(DivW32W16(-tmp1S32, nsk))
						tmpS16 = -tmpS16
					}
					tmpS16 += 32       // Rounding
					nsk += tmpS16 >> 6 // Q13 >> 6 = Q7.
					if nsk < kMinStd {
						nsk = kMinStd
					}
					self.NoiseStds[gaussian] = nsk
				}
			}

			// Separate models if they are too close.
			// noiseGlobalMean in Q14 (= Q7 * Q7).
			noiseGlobalMean = weightedAverage(self.NoiseMeans[:], channel, 0, &kNoiseDataWeights)

			// speechGlobalMean in Q14 (= Q7 * Q7).
			speechGlobalMean := weightedAverage(self.SpeechMeans[:], channel, 0, &kSpeechDataWeights)

			// diff = "global" speech mean - "global" noise mean.
			// (Q14 >> 9) - (Q14 >> 9) = Q5.
			diff := int16(speechGlobalMean>>9) - int16(noiseGlobalMean>>9)
			if diff < kMinimumDifference[channel] {
				tmpS16 := kMinimumDifference[channel] - diff

				// tmp1S16 = ~0.8 * (kMinimumDifference - diff) in Q7.
				// tmp2S16 = ~0.2 * (kMinimumDifference - diff) in Q7.
				tmp1S16 := int16((13 * int32(tmpS16)) >> 2)
				tmp2S16 := int16((3 * int32(tmpS16)) >> 2)

				// Move Gaussian means for speech model by tmp1S16 and
				// update speechGlobalMean; the means themselves are
				// changed by weightedAverage.
				speechGlobalMean = weightedAverage(self.SpeechMeans[:], channel,
					tmp1S16, &kSpeechDataWeights)

				// Move Gaussian means for noise model by -tmp2S16 and
				// update noiseGlobalMean.
				noiseGlobalMean = weightedAverage(self.NoiseMeans[:], channel,
					-tmp2S16, &kNoiseDataWeights)
			}

			// Control that the speech & noise means do not drift too
			// much.
			maxspe = kMaximumSpeech[channel]
			tmp2S16 := int16(speechGlobalMean >> 7)
			if tmp2S16 > maxspe {
				// Upper limit of speech model.
				tmp2S16 -= maxspe
				for k := 0; k < NumGaussians; k++ {
					self.SpeechMeans[channel+k*NumChannels] -= tmp2S16
				}
			}

			tmp2S16 = int16(noiseGlobalMean >> 7)
			if tmp2S16 > kMaximumNoise[channel] {
				tmp2S16 -= kMaximumNoise[channel]
				for k := 0; k < NumGaussians; k++ {
					self.NoiseMeans[channel+k*NumChannels] -= tmp2S16
				}
			}
		}
		self.FrameCounter++
	}

	// Smooth with respect to transition hysteresis.
	if vadflag == 0 {
		if self.OverHang > 0 {
			vadflag = 2 + self.OverHang
			self.OverHang--
		}
		self.NumOfSpeech = 0
	} else {
		self.NumOfSpeech++
		if self.NumOfSpeech > kMaxSpeechFrames {
			self.NumOfSpeech = kMaxSpeechFrames
			self.OverHang = overhead2
		} else {
			self.OverHang = overhead1
		}
	}
	return vadflag
}

// InitCore ports WebRtcVad_InitCore: reset all state and set the
// aggressiveness mode to the default (0, quality).
func (self *Inst) InitCore() {
	// General struct variables.
	self.Vad = 1 // Speech active (=1).
	self.FrameCounter = 0
	self.OverHang = 0
	self.NumOfSpeech = 0

	// Downsampling filter states.
	self.DownsamplingFilterStates = [4]int32{}

	// 48-to-8 kHz downsampling state.
	self.State48To8.Reset()

	// Read initial PDF parameters.
	self.NoiseMeans = kNoiseDataMeans
	self.SpeechMeans = kSpeechDataMeans
	self.NoiseStds = kNoiseDataStds
	self.SpeechStds = kSpeechDataStds

	// Index and Minimum value vectors.
	for i := 0; i < 16*NumChannels; i++ {
		self.LowValueVector[i] = 10000
		self.IndexVector[i] = 0
	}

	// Splitting and high pass filter states.
	self.UpperState = [5]int16{}
	self.LowerState = [5]int16{}
	self.HpFilterState = [4]int16{}

	// Mean value memory, for FindMinimum().
	for i := 0; i < NumChannels; i++ {
		self.MeanValue[i] = 1600
	}

	// Set aggressiveness mode to default (cannot fail for kDefaultMode).
	self.SetModeCore(kDefaultMode)

	self.InitFlag = kInitCheck
}

// SetModeCore ports WebRtcVad_set_mode_core: select the aggressiveness
// mode's threshold tables. Returns 0 on success, -1 for an invalid
// mode.
func (self *Inst) SetModeCore(mode int) int {
	switch mode {
	case 0:
		// Quality mode.
		self.OverHangMax1 = kOverHangMax1Q
		self.OverHangMax2 = kOverHangMax2Q
		self.Individual = kLocalThresholdQ
		self.Total = kGlobalThresholdQ
	case 1:
		// Low bitrate mode.
		self.OverHangMax1 = kOverHangMax1LBR
		self.OverHangMax2 = kOverHangMax2LBR
		self.Individual = kLocalThresholdLBR
		self.Total = kGlobalThresholdLBR
	case 2:
		// Aggressive mode.
		self.OverHangMax1 = kOverHangMax1AGG
		self.OverHangMax2 = kOverHangMax2AGG
		self.Individual = kLocalThresholdAGG
		self.Total = kGlobalThresholdAGG
	case 3:
		// Very aggressive mode.
		self.OverHangMax1 = kOverHangMax1VAG
		self.OverHangMax2 = kOverHangMax2VAG
		self.Individual = kLocalThresholdVAG
		self.Total = kGlobalThresholdVAG
	default:
		return -1
	}
	return 0
}

// CalcVad48khz ports WebRtcVad_CalcVad48khz. NOTE the faithfully
// replicated upstream quirk: the C never advances the input pointer
// across its per-10 ms resample loop, so for 20/30 ms frames the FIRST
// 480 input samples are resampled num_10ms_frames times (with evolving
// filter state) and the rest of the frame never reaches the resampler.
// The port reproduces this exactly — bit-exactness with the reference
// outranks fixing inherited bugs.
func CalcVad48khz(inst *Inst, speechFrame []int16) int {
	var speechNB [240]int16 // 30 ms in 8 kHz.
	// Temporary memory for the resampler: 10 ms at 48 kHz (480 samples)
	// + 256 extra, zeroed once per call like the C's = {0} initializer.
	var tmpMem [resample48To8TmpLen]int32
	const kFrameLen10ms48khz = 480
	const kFrameLen10ms8khz = 80
	num10msFrames := len(speechFrame) / kFrameLen10ms48khz

	for i := 0; i < num10msFrames; i++ {
		Resample48khzTo8khz(speechFrame[:kFrameLen10ms48khz],
			speechNB[i*kFrameLen10ms8khz:],
			&inst.State48To8, tmpMem[:])
	}

	// Do VAD on an 8 kHz signal.
	return CalcVad8khz(inst, speechNB[:len(speechFrame)/6])
}

// CalcVad32khz ports WebRtcVad_CalcVad32khz: downsample 32→16→8 kHz,
// then run the 8 kHz VAD.
func CalcVad32khz(inst *Inst, speechFrame []int16) int {
	var speechWB [480]int16 // Downsampled speech frame: 960 samples (30 ms in SWB)
	var speechNB [240]int16 // Downsampled speech frame: 480 samples (30 ms in WB)

	// Downsample signal 32->16->8 before doing VAD.
	Downsampling(speechFrame, speechWB[:], inst.DownsamplingFilterStates[2:], len(speechFrame))
	length := len(speechFrame) / 2

	Downsampling(speechWB[:length], speechNB[:], inst.DownsamplingFilterStates[:], length)
	length /= 2

	// Do VAD on an 8 kHz signal.
	return CalcVad8khz(inst, speechNB[:length])
}

// CalcVad16khz ports WebRtcVad_CalcVad16khz: downsample 16→8 kHz, then
// run the 8 kHz VAD.
func CalcVad16khz(inst *Inst, speechFrame []int16) int {
	var speechNB [240]int16 // Downsampled speech frame: 480 samples (30 ms in WB)

	// Wideband: Downsample signal before doing VAD.
	Downsampling(speechFrame, speechNB[:], inst.DownsamplingFilterStates[:], len(speechFrame))

	length := len(speechFrame) / 2
	return CalcVad8khz(inst, speechNB[:length])
}

// CalcVad8khz ports WebRtcVad_CalcVad8khz: extract band features, then
// compute the GMM-based decision. Returns 0 for no active speech, >0
// (1–6, and transiently 2+overhang) for active speech, mirroring the C.
func CalcVad8khz(inst *Inst, speechFrame []int16) int {
	// Get power in the bands.
	inst.TotalPower = CalculateFeatures(inst, speechFrame, inst.FeatureVector[:])

	// Make a VAD.
	inst.Vad = int(gmmProbability(inst, inst.FeatureVector[:], inst.TotalPower, len(speechFrame)))

	return inst.Vad
}
