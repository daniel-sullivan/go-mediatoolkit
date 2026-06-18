package mutations

// ResizeScratch grows or shrinks buf to hold exactly n samples,
// reusing existing capacity when possible. Use it to manage per-call
// scratch buffers on hot paths without re-allocating each invocation:
//
//	s.scratch = mutations.ResizeScratch(s.scratch, want)
//
// The returned slice aliases the caller's storage when cap(buf) ≥ n
// and allocates a fresh slice otherwise.
func ResizeScratch(buf []float64, n int) []float64 {
	if cap(buf) < n {
		return make([]float64, n)
	}
	return buf[:n]
}
