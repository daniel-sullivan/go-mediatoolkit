//go:build amd64

package inspection

// butterflyPass performs one stage of radix-2 FFT butterflies in-place.
// Uses SSE2 for complex multiply (SHUFPD+MULPD+ADDSUBPD) and butterfly (ADDPD/SUBPD).
//
//go:noescape
func butterflyPass(x []complex128, tw []complex128, half, step int)
