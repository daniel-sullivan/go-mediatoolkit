//go:build arm64

package inspection

// butterflyPass performs one stage of radix-2 FFT butterflies in-place.
// For each group of `size` elements starting at every `size` stride in x:
//
//	for j := 0; j < half; j++ {
//	    w := tw[j*step]
//	    u := x[i+j]
//	    v := x[i+j+half] * w
//	    x[i+j]      = u + v
//	    x[i+j+half] = u - v
//	}
//
// The function processes ALL groups for one FFT stage.
// Uses NEON for complex multiply (EXT+FMUL+FMLA) and butterfly (FADD/FSUB).
//
//go:noescape
func butterflyPass(x []complex128, tw []complex128, half, step int)
