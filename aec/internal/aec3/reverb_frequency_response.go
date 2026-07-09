// This file ports modules/audio_processing/aec3/
// reverb_frequency_response.{h,cc}: ReverbFrequencyResponse, which
// estimates the frequency response of the reverberant echo tail.
package aec3

// averageDecayWithinFilter computes the ratio of the energies between
// the direct path and the tail, in the power spectrum domain,
// discarding the DC contribution. C: (anonymous namespace)
// AverageDecayWithinFilter.
func averageDecayWithinFilter(freqRespDirectPath, freqRespTail []float32) float32 {
	// Skipping the DC bin for the ratio computation.
	const skipBins = 1

	var directPathEnergy float32
	for _, v := range freqRespDirectPath[skipBins:] {
		directPathEnergy += v
	}

	if directPathEnergy == 0 {
		return 0
	}

	var tailEnergy float32
	for _, v := range freqRespTail[skipBins:] {
		tailEnergy += v
	}
	return tailEnergy / directPathEnergy
}

// ReverbFrequencyResponse estimates the frequency response of the
// reverb tail. C: reverb_frequency_response.{h,cc}'s
// ReverbFrequencyResponse.
type ReverbFrequencyResponse struct {
	useConservativeTailFrequencyResponse bool
	averageDecay                         float32
	tailResponse                         [FFTLengthBy2Plus1]float32
}

// NewReverbFrequencyResponse constructs a ReverbFrequencyResponse. C:
// ReverbFrequencyResponse::ReverbFrequencyResponse.
func NewReverbFrequencyResponse(useConservativeTailFrequencyResponse bool) *ReverbFrequencyResponse {
	return &ReverbFrequencyResponse{useConservativeTailFrequencyResponse: useConservativeTailFrequencyResponse}
}

// FrequencyResponse returns the estimated frequency response for the
// reverb. C: ReverbFrequencyResponse::FrequencyResponse.
func (r *ReverbFrequencyResponse) FrequencyResponse() [FFTLengthBy2Plus1]float32 {
	return r.tailResponse
}

// Update updates the frequency response estimate of the reverb.
// linearFilterQuality is nil to mirror std::optional's empty state.
// C: ReverbFrequencyResponse::Update (public overload).
func (r *ReverbFrequencyResponse) Update(frequencyResponse [][FFTLengthBy2Plus1]float32, filterDelayBlocks int, linearFilterQuality *float32, stationaryBlock bool) {
	if stationaryBlock || linearFilterQuality == nil {
		return
	}

	r.update(frequencyResponse, filterDelayBlocks, *linearFilterQuality)
}

// update is the private overload of Update. C:
// ReverbFrequencyResponse::Update (private overload).
func (r *ReverbFrequencyResponse) update(frequencyResponse [][FFTLengthBy2Plus1]float32, filterDelayBlocks int, linearFilterQuality float32) {
	freqRespTail := frequencyResponse[len(frequencyResponse)-1]
	freqRespDirectPath := frequencyResponse[filterDelayBlocks]

	averageDecay := averageDecayWithinFilter(freqRespDirectPath[:], freqRespTail[:])

	smoothing := mul32(0.2, linearFilterQuality)
	r.averageDecay = mla(r.averageDecay, smoothing, sub32(averageDecay, r.averageDecay))

	for k := 0; k < FFTLengthBy2Plus1; k++ {
		r.tailResponse[k] = mul32(freqRespDirectPath[k], r.averageDecay)
	}

	if r.useConservativeTailFrequencyResponse {
		for k := 0; k < FFTLengthBy2Plus1; k++ {
			r.tailResponse[k] = max32(freqRespTail[k], r.tailResponse[k])
		}
	}

	for k := 1; k < FFTLengthBy2; k++ {
		avgNeighbour := mul32(0.5, add32(r.tailResponse[k-1], r.tailResponse[k+1]))
		r.tailResponse[k] = max32(r.tailResponse[k], avgNeighbour)
	}
}
