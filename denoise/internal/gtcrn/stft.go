package gtcrn

import "math"

// STFT/ISTFT front end — OUTSIDE the GTCRN model and with no ORT oracle,
// so it is gated independently against torch.stft/torch.istft goldens
// (testdata/stft_golden.json). The convention is fixed by the upstream
// streaming pipeline (VERSION): n_fft 512, hop 256, win_length 512,
// window = sqrt of the PERIODIC Hann, center=True (reflect-pad n_fft//2
// each end), onesided (257 bins), unnormalised.
const (
	// NFFT is the DFT size / analysis window length.
	NFFT = 512
	// Hop is the frame advance in samples (50% overlap).
	Hop = 256
	// Bins is the onesided spectrum size, NFFT/2+1.
	Bins = NFFT/2 + 1
	// centerPad is the reflect padding applied to each end when center=True.
	centerPad = NFFT / 2
)

// sqrtHann is the analysis/synthesis window: the square root of the
// periodic Hann window, w[n] = sqrt(0.5 - 0.5*cos(2*pi*n/NFFT)).
var sqrtHann = func() [NFFT]float32 {
	var w [NFFT]float32
	for n := 0; n < NFFT; n++ {
		w[n] = float32(math.Sqrt(0.5 - 0.5*math.Cos(2*math.Pi*float64(n)/float64(NFFT))))
	}
	return w
}()

// reflectPad returns x with pad samples of edge-excluding reflection on
// each end — the ONNX/torch "reflect" convention: the boundary sample is
// not repeated. Requires pad <= len(x)-1.
func reflectPad(x []float32, pad int) []float32 {
	n := len(x)
	out := make([]float32, n+2*pad)
	for i := 0; i < pad; i++ {
		out[i] = x[pad-i] // out[0]=x[pad] … out[pad-1]=x[1]
	}
	copy(out[pad:], x)
	for i := 0; i < pad; i++ {
		out[pad+n+i] = x[n-2-i] // mirror x[n-2], x[n-3], …
	}
	return out
}

// STFT computes the onesided complex spectrogram of a 16 kHz signal with
// the fixed GTCRN convention. re and im are freq-major (index k*frames+f,
// matching the golden's [Bins, frames] layout); frames is the time count.
func STFT(x []float32) (re, im []float32, frames int) {
	padded := reflectPad(x, centerPad)
	frames = (len(padded)-NFFT)/Hop + 1
	re = make([]float32, Bins*frames)
	im = make([]float32, Bins*frames)

	var fr, fi [NFFT]float64
	for f := 0; f < frames; f++ {
		seg := padded[f*Hop:]
		for n := 0; n < NFFT; n++ {
			fr[n] = float64(seg[n] * sqrtHann[n])
			fi[n] = 0
		}
		fft(fr[:], fi[:], false)
		for k := 0; k < Bins; k++ {
			re[k*frames+f] = float32(fr[k])
			im[k*frames+f] = float32(fi[k])
		}
	}
	return re, im, frames
}

// ISTFT inverts STFT with window-squared overlap-add normalisation and
// removes the center padding. re/im are freq-major [Bins, frames]. The
// result is the center-trimmed reconstruction of length (frames-1)*Hop;
// callers compare against the golden over the interior.
func ISTFT(re, im []float32, frames int) []float32 {
	paddedLen := (frames-1)*Hop + NFFT
	acc := make([]float64, paddedLen)
	wsum := make([]float64, paddedLen)

	var fr, fi [NFFT]float64
	for f := 0; f < frames; f++ {
		for k := 0; k < Bins; k++ {
			fr[k] = float64(re[k*frames+f])
			fi[k] = float64(im[k*frames+f])
		}
		// Hermitian completion of the onesided spectrum.
		for k := Bins; k < NFFT; k++ {
			fr[k] = float64(re[(NFFT-k)*frames+f])
			fi[k] = -float64(im[(NFFT-k)*frames+f])
		}
		fft(fr[:], fi[:], true)
		for n := 0; n < NFFT; n++ {
			w := float64(sqrtHann[n])
			acc[f*Hop+n] += fr[n] * w
			wsum[f*Hop+n] += w * w
		}
	}
	for i := range acc {
		if wsum[i] > 1e-12 {
			acc[i] /= wsum[i]
		}
	}
	trimmed := acc[centerPad : paddedLen-centerPad]
	out := make([]float32, len(trimmed))
	for i, v := range trimmed {
		out[i] = float32(v)
	}
	return out
}

// fft is an in-place iterative radix-2 Cooley-Tukey transform on the
// length-NFFT complex signal (re, im). inverse=true applies the +i sign
// and 1/NFFT scaling. NFFT is a fixed power of two (512), so no bounds
// generality is needed.
func fft(re, im []float64, inverse bool) {
	n := len(re)
	// Bit-reversal permutation.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	sign := -1.0
	if inverse {
		sign = 1.0
	}
	for length := 2; length <= n; length <<= 1 {
		ang := sign * 2 * math.Pi / float64(length)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			cr, ci := 1.0, 0.0
			for k := 0; k < length/2; k++ {
				ur := re[i+k]
				ui := im[i+k]
				vr := re[i+k+length/2]*cr - im[i+k+length/2]*ci
				vi := re[i+k+length/2]*ci + im[i+k+length/2]*cr
				re[i+k] = ur + vr
				im[i+k] = ui + vi
				re[i+k+length/2] = ur - vr
				im[i+k+length/2] = ui - vi
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
	if inverse {
		inv := 1.0 / float64(n)
		for i := range re {
			re[i] *= inv
			im[i] *= inv
		}
	}
}
