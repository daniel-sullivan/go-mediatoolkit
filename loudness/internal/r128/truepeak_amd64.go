//go:build amd64

package r128

// oversamplePair processes channels c0 and c0+1 of the interleaved
// stream with the SSE2 2-lane kernel in truepeak_amd64.s (baseline
// SSE2 only — MULPD/ADDPD, no FMA, no runtime feature detection; same
// policy as inspection/butterfly_amd64.s). It follows the same cursor
// protocol as oversamplePairGeneric (which it shadows, and whose
// per-lane arithmetic it reproduces bit-for-bit — see the assembly's
// header comment): reads tp.zi, returns the final cursor, never writes
// tp.zi.
func (tp *TruePeaker) oversamplePair(in []float32, out []float32, c0 int) int {
	return oversamplePairAsm(tp.z, in, out, tp.coeffFlat,
		len(in)/tp.channels, tp.factor, tp.simdCount, tp.channels,
		c0, tp.delay, tp.phase0Index, tp.zi)
}

// oversamplePairAsm is the SSE2 channel-pair polyphase kernel. z is the
// mirrored interleaved delay line, coeff the flattened phases-1..factor-1
// coefficient table ((factor-1)*count float64s); the remaining scalars
// mirror the TruePeaker fields of the same names. It returns the delay
// cursor after processing `frames` frames.
//
//go:noescape
func oversamplePairAsm(z []float32, in []float32, out []float32, coeff []float64,
	frames, factor, count, ch, c0, delay, phase0Index, zi int) int
