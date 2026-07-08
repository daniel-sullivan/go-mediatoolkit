//go:build !arm64 && !amd64

package r128

// oversamplePair on architectures without an assembly kernel is simply
// the pure-Go pair kernel. Keeping the indirection (rather than calling
// oversamplePairGeneric from Oversample directly) gives every
// architecture the same dispatch shape, and keeps
// oversamplePairGeneric compiled everywhere so the asm-vs-generic
// equality tests can exercise both on arm64/amd64.
func (tp *TruePeaker) oversamplePair(in []float32, out []float32, c0 int) int {
	return tp.oversamplePairGeneric(in, out, c0)
}
