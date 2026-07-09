// This file ports modules/audio_processing/aec3/
// subtractor_output_analyzer.{h,cc}: analyzes the properties of the
// subtractor output to determine per-channel filter convergence and
// divergence.
package aec3

// SubtractorOutputAnalyzer analyzes the properties of the subtractor
// output. Port of subtractor_output_analyzer.{h,cc}'s
// SubtractorOutputAnalyzer.
type SubtractorOutputAnalyzer struct {
	filtersConverged []bool
}

// NewSubtractorOutputAnalyzer mirrors SubtractorOutputAnalyzer's C++
// constructor.
func NewSubtractorOutputAnalyzer(numCaptureChannels int) *SubtractorOutputAnalyzer {
	return &SubtractorOutputAnalyzer{filtersConverged: make([]bool, numCaptureChannels)}
}

// Update analyses the subtractor output. C:
// SubtractorOutputAnalyzer::Update.
func (a *SubtractorOutputAnalyzer) Update(subtractorOutput []SubtractorOutput, anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged *bool) {
	*anyFilterConverged = false
	*anyCoarseFilterConverged = false
	*allFiltersDiverged = true

	for ch := 0; ch < len(subtractorOutput); ch++ {
		y2 := subtractorOutput[ch].Y2
		e2Refined := subtractorOutput[ch].E2RefinedScalar
		e2Coarse := subtractorOutput[ch].E2CoarseScalar

		const convergenceThreshold = 50 * 50 * BlockSize
		const convergenceThresholdLowLevel = 20 * 20 * BlockSize
		refinedFilterConverged := e2Refined < 0.5*y2 && y2 > convergenceThreshold
		coarseFilterConvergedStrict := e2Coarse < 0.05*y2 && y2 > convergenceThreshold
		coarseFilterConvergedRelaxed := e2Coarse < 0.3*y2 && y2 > convergenceThresholdLowLevel
		minE2 := e2Refined
		if e2Coarse < minE2 {
			minE2 = e2Coarse
		}
		filterDiverged := minE2 > 1.5*y2 && y2 > 30.0*30.0*BlockSize
		a.filtersConverged[ch] = refinedFilterConverged || coarseFilterConvergedStrict

		*anyFilterConverged = *anyFilterConverged || a.filtersConverged[ch]
		*anyCoarseFilterConverged = *anyCoarseFilterConverged || coarseFilterConvergedRelaxed
		*allFiltersDiverged = *allFiltersDiverged && filterDiverged
	}
}

// ConvergedFilters returns the per-channel convergence state. C:
// SubtractorOutputAnalyzer::ConvergedFilters.
func (a *SubtractorOutputAnalyzer) ConvergedFilters() []bool { return a.filtersConverged }

// HandleEchoPathChange handles an echo path change. C:
// SubtractorOutputAnalyzer::HandleEchoPathChange.
func (a *SubtractorOutputAnalyzer) HandleEchoPathChange() {
	for i := range a.filtersConverged {
		a.filtersConverged[i] = false
	}
}
