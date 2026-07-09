// This file ports modules/audio_processing/aec3/
// adaptive_fir_filter_erl.{h,cc}: computation of the echo return loss
// (ERL) from a filter's per-partition frequency responses. Only the
// scalar aec3::ErlComputer kernel is ported (see common.go's doc
// comment on this package being scalar-only); ComputeErl's
// Aec3Optimization dispatch collapses to a direct call.
package aec3

// ComputeErl computes the echo return loss from the frequency
// responses of the filter partitions, storing the per-bin sum into
// erl (length FFTLengthBy2Plus1). C: aec3::ComputeErl (dispatches to
// the scalar ErlComputer in this port).
func ComputeErl(h2 [][FFTLengthBy2Plus1]float32, erl []float32) {
	erlComputer(h2, erl)
}

// erlComputer is the scalar ERL kernel. C: aec3::ErlComputer.
func erlComputer(h2 [][FFTLengthBy2Plus1]float32, erl []float32) {
	for k := range erl {
		erl[k] = 0
	}
	for _, h2J := range h2 {
		for k := range erl {
			erl[k] += h2J[k]
		}
	}
}
