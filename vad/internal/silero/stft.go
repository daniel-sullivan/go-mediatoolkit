package silero

import "math"

// STFT front end of the graph (VERSION step 1): reflect-pad the
// 576-sample network input (64 context + 512 window) by 64 samples on
// the right, then a strided correlation against the fixed
// forward-basis buffer — 258 kernels of length 256 at hop 128, rows
// 0..128 the real part and 129..257 the imaginary part of a windowed
// DFT — and take per-bin magnitudes.
const (
	stftKernel = 256 // basis kernel length (the analysis window)
	stftHop    = 128 // convolution stride
	stftPad    = 64  // right reflect padding
	stftBins   = 129 // output frequency bins (real/imag pairs)

	// stftFrames is the frame count for the fixed network input:
	// (netInput + stftPad − stftKernel)/stftHop + 1 = 4.
	stftFrames = (netInput+stftPad-stftKernel)/stftHop + 1
)

// reflectPadRight writes x followed by pad reflected samples into dst
// (len(dst) == len(x)+pad): dst[len(x)+j] = x[len(x)-2-j], the
// edge-excluding mirror ONNX Pad mode "reflect" (and torch
// F.pad(reflect)) uses. Requires pad ≤ len(x)−1.
func reflectPadRight(dst, x []float32, pad int) {
	n := copy(dst, x)
	for j := 0; j < pad; j++ {
		dst[n+j] = x[n-2-j]
	}
}

// stftMagnitude computes the [stftBins × stftFrames] magnitude
// spectrogram of the 576-sample network input x into out (row-major,
// time minor: out[bin*stftFrames+frame]), using padded as the
// 640-sample reflect-pad scratch. Accumulation is fixed sequential
// float32, like every kernel in this package.
func stftMagnitude(basis, x, padded, out []float32) {
	reflectPadRight(padded, x, stftPad)
	for frame := 0; frame < stftFrames; frame++ {
		seg := padded[frame*stftHop : frame*stftHop+stftKernel]
		for bin := 0; bin < stftBins; bin++ {
			var re, im float32
			rw := basis[bin*stftKernel : (bin+1)*stftKernel]
			iw := basis[(bin+stftBins)*stftKernel : (bin+stftBins+1)*stftKernel]
			for n := 0; n < stftKernel; n++ {
				re += rw[n] * seg[n]
				im += iw[n] * seg[n]
			}
			out[bin*stftFrames+frame] = float32(math.Sqrt(float64(re*re + im*im)))
		}
	}
}
