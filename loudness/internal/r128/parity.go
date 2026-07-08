package r128

// This file exposes a few internals of the meter port for the bit-exact
// parity slices under loudness/internal/parity_tests. They let a slice
// read the same intermediate quantities the C oracle exposes through its
// own shims (the static ebur128_calc_gating_block energy, and the static
// gating constants), so energies can be compared bit-for-bit rather than
// only the final log-domain LUFS. They are not part of the meter's
// intended surface; the root loudness package does not use them.

// CalcGatingBlockEnergy returns ebur128_calc_gating_block's energy output:
// the channel-weighted mean-square K-weighted energy over the trailing
// framesPerBlock frames of the ring buffer, in linear energy units. It is
// the exact quantity the C shim reads via
// ebur128_calc_gating_block(st, framesPerBlock, &out).
func (s *State) CalcGatingBlockEnergy(framesPerBlock int) float64 {
	var out float64
	s.calcGatingBlock(framesPerBlock, &out)
	return out
}

// GateConstants returns the port's static gating constants — the values
// libebur128 computes once in ebur128_init and stores in file-scope
// statics: relative_gate_factor, minus_twenty_decibels, and
// histogram_energy_boundaries[0] (the absolute-gate energy). Parity slices
// assert these match the C oracle bit-for-bit, since a 1-ULP difference in
// any of them could flip a gating comparison and diverge the whole
// integrated/LRA result.
func GateConstants() (relativeGateFactorOut, minusTwentyDecibelsOut, boundary0 float64) {
	return relativeGateFactor, minusTwentyDecibels, histogramEnergyBoundaries[0]
}

// HistogramEnergy returns histogram_energies[i] (the representative energy
// of histogram bin i), for parity verification of the histogram tables.
func HistogramEnergy(i int) float64 { return histogramEnergies[i] }

// HistogramBoundary returns histogram_energy_boundaries[i], for parity
// verification of the histogram bin boundaries.
func HistogramBoundary(i int) float64 { return histogramEnergyBoundaries[i] }

// NumShortTermBlocks returns how many short-term blocks the meter has
// retained for LRA — the length of the block list in list mode, or the
// total count across the histogram in histogram mode. Parity slices compare
// this against the C oracle to confirm the 1 s LRA hop and set_max_history
// trimming produce the same structure.
func (s *State) NumShortTermBlocks() int {
	if s.useHistogram {
		n := 0
		for _, c := range s.shortTermBlockEnergyHistogram {
			n += int(c)
		}
		return n
	}
	return len(s.shortTermBlockList)
}
