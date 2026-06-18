package nativeflac

// 1:1 port of libflac/src/libFLAC/window.c — the encoder's apodization
// window generators.
//
// Each FLAC__window_* function fills a window[0..L-1] of FLAC__real
// (= float32) coefficients for a given window length L. The encoder
// multiplies a block of integer samples by one of these windows before
// running the LPC autocorrelation analysis, so the windows must match
// libFLAC bit-for-bit when the strict build is used.
//
// # Floating-point fidelity
//
// window.c is FP-heavy and the exact bit pattern depends on the operand
// types and ordering C uses:
//
//   - FLAC__real is `float` (float32). The window array is float32.
//   - cosf / fabsf / expf operate in float precision; their *argument*
//     is rounded to float32 before the kernel runs. The angle
//     expressions like `2.0f * M_PI * n / N` promote to `double`
//     (because M_PI is a double constant), so the double angle is
//     rounded to float32 at the cosf call boundary.
//   - cos / exp (connes, gauss) operate in double precision; the
//     surrounding arithmetic is double until the final cast to float32.
//
// The arithmetic surrounding the transcendental calls is left-to-right
// float32 (e.g. `0.42f - 0.5f*c1 + 0.08f*c2` is
// `(0.42 - (0.5*c1)) + (0.08*c2)`). In the strict build every float32
// multiply / add is routed through the //go:noinline helpers in
// window_fp_strict.go so Go's arm64 backend cannot fuse a multiply-add
// (FMADDS) and break parity with libFLAC's `-ffp-contract=off` oracle.
// The default build (window_fp_default.go) inlines the plain operators.
//
// piF mirrors C's M_PI as the IEEE-754 double constant 3.14159265358979323846.

const piF = 3.14159265358979323846

// WindowType enumerates the apodization functions the encoder can
// select. Values mirror the FLAC__APODIZATION_* enum in
// private/stream_encoder.h; the encoder's apodization-string parser
// (in stream_encoder.c) maps user specifications onto these. The
// dispatch helper ApplyWindow mirrors the switch in
// resize_buffers_ / process_subframe_ that calls the matching
// FLAC__window_* function.
type WindowType int

const (
	WindowBartlett WindowType = iota
	WindowBartlettHann
	WindowBlackman
	WindowBlackmanHarris4Term92dBSidelobe
	WindowConnes
	WindowFlattop
	WindowGauss // parameter: stddev
	WindowHamming
	WindowHann
	WindowKaiserBessel
	WindowNuttall
	WindowRectangle
	WindowTriangle
	WindowTukey         // parameter: p
	WindowPartialTukey  // parameters: p, start, end
	WindowPunchoutTukey // parameters: p, start, end
	WindowWelch
)

// WindowBartlett — port of FLAC__window_bartlett (window.c:50).
func WindowBartlettFn(window []float32, L int32) {
	N := L - 1
	var n int32
	if L&1 != 0 {
		for n = 0; n <= N/2; n++ {
			window[n] = f32div(f32mul(2.0, float32(n)), float32(N))
		}
		for ; n <= N; n++ {
			window[n] = f32sub(2.0, f32div(f32mul(2.0, float32(n)), float32(N)))
		}
	} else {
		for n = 0; n <= L/2-1; n++ {
			window[n] = f32div(f32mul(2.0, float32(n)), float32(N))
		}
		for ; n <= N; n++ {
			window[n] = f32sub(2.0, f32div(f32mul(2.0, float32(n)), float32(N)))
		}
	}
}

// WindowBartlettHann — port of FLAC__window_bartlett_hann (window.c:69).
func WindowBartlettHannFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n < L; n++ {
		// 0.62f - 0.48f*fabsf((float)n/(float)N-0.5f)
		//       - 0.38f*cosf(2.0f*M_PI*((float)n/(float)N))
		// Both n/N occurrences are float32 divisions (cast to float
		// before the divide). The cosf argument promotes to double via
		// the 2.0f*M_PI*... chain (M_PI is a double constant), with the
		// already-rounded float32 (float)n/(float)N as the final factor.
		nOverN := f32div(float32(n), float32(N))
		t1 := f32mul(0.48, f32abs(f32sub(nOverN, 0.5)))
		angle := 2.0 * piF * float64(nOverN)
		t2 := f32mul(0.38, cosfStrict(angle))
		window[n] = f32sub(f32sub(0.62, t1), t2)
	}
}

// WindowBlackman — port of FLAC__window_blackman (window.c:78).
func WindowBlackmanFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n < L; n++ {
		// 0.42f - 0.5f*cosf(2*M_PI*n/N) + 0.08f*cosf(4*M_PI*n/N)
		c1 := cosfStrict(2.0 * piF * float64(n) / float64(N))
		c2 := cosfStrict(4.0 * piF * float64(n) / float64(N))
		window[n] = f32add(f32sub(0.42, f32mul(0.5, c1)), f32mul(0.08, c2))
	}
}

// WindowBlackmanHarris4Term92dBSidelobe — port of
// FLAC__window_blackman_harris_4term_92db_sidelobe (window.c:88).
func WindowBlackmanHarris4Term92dBSidelobeFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n <= N; n++ {
		c1 := cosfStrict(2.0 * piF * float64(n) / float64(N))
		c2 := cosfStrict(4.0 * piF * float64(n) / float64(N))
		c3 := cosfStrict(6.0 * piF * float64(n) / float64(N))
		// 0.35875 - 0.48829*c1 + 0.14128*c2 - 0.01168*c3
		w := f32sub(0.35875, f32mul(0.48829, c1))
		w = f32add(w, f32mul(0.14128, c2))
		w = f32sub(w, f32mul(0.01168, c3))
		window[n] = w
	}
}

// WindowConnes — port of FLAC__window_connes (window.c:97).
func WindowConnesFn(window []float32, L int32) {
	N := L - 1
	N2 := float64(N) / 2.0
	var n int32
	for n = 0; n <= N; n++ {
		k := (float64(n) - N2) / N2
		k = f64sub(1.0, f64mul(k, k))
		window[n] = float32(k * k)
	}
}

// WindowFlattop — port of FLAC__window_flattop (window.c:110).
func WindowFlattopFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n < L; n++ {
		c1 := cosfStrict(2.0 * piF * float64(n) / float64(N))
		c2 := cosfStrict(4.0 * piF * float64(n) / float64(N))
		c3 := cosfStrict(6.0 * piF * float64(n) / float64(N))
		c4 := cosfStrict(8.0 * piF * float64(n) / float64(N))
		// 0.21557895 - 0.41663158*c1 + 0.277263158*c2 - 0.083578947*c3 + 0.006947368*c4
		w := f32sub(0.21557895, f32mul(0.41663158, c1))
		w = f32add(w, f32mul(0.277263158, c2))
		w = f32sub(w, f32mul(0.083578947, c3))
		w = f32add(w, f32mul(0.006947368, c4))
		window[n] = w
	}
}

// WindowGauss — port of FLAC__window_gauss (window.c:119). Recurses
// with stddev 0.25 when stddev is out of (0,0.5] (NaN-safe, matching C).
func WindowGaussFn(window []float32, L int32, stddev float32) {
	N := L - 1
	N2 := float64(N) / 2.0
	if !(stddev > 0.0 && stddev <= 0.5) {
		WindowGaussFn(window, L, 0.25)
		return
	}
	var n int32
	for n = 0; n <= N; n++ {
		// k = (n - N2) / (stddev * N2); window[n] = exp(-0.5f * k * k)
		k := (float64(n) - N2) / (float64(stddev) * N2)
		window[n] = float32(expDouble(-0.5 * k * k))
	}
}

// WindowHamming — port of FLAC__window_hamming (window.c:137).
func WindowHammingFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n < L; n++ {
		c1 := cosfStrict(2.0 * piF * float64(n) / float64(N))
		window[n] = f32sub(0.54, f32mul(0.46, c1))
	}
}

// WindowHann — port of FLAC__window_hann (window.c:146).
func WindowHannFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n < L; n++ {
		c1 := cosfStrict(2.0 * piF * float64(n) / float64(N))
		window[n] = f32sub(0.5, f32mul(0.5, c1))
	}
}

// WindowKaiserBessel — port of FLAC__window_kaiser_bessel (window.c:155).
func WindowKaiserBesselFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n < L; n++ {
		c1 := cosfStrict(2.0 * piF * float64(n) / float64(N))
		c2 := cosfStrict(4.0 * piF * float64(n) / float64(N))
		c3 := cosfStrict(6.0 * piF * float64(n) / float64(N))
		// 0.402 - 0.498*c1 + 0.098*c2 - 0.001*c3
		w := f32sub(0.402, f32mul(0.498, c1))
		w = f32add(w, f32mul(0.098, c2))
		w = f32sub(w, f32mul(0.001, c3))
		window[n] = w
	}
}

// WindowNuttall — port of FLAC__window_nuttall (window.c:164).
func WindowNuttallFn(window []float32, L int32) {
	N := L - 1
	var n int32
	for n = 0; n < L; n++ {
		c1 := cosfStrict(2.0 * piF * float64(n) / float64(N))
		c2 := cosfStrict(4.0 * piF * float64(n) / float64(N))
		c3 := cosfStrict(6.0 * piF * float64(n) / float64(N))
		// 0.3635819 - 0.4891775*c1 + 0.1365995*c2 - 0.0106411*c3
		w := f32sub(0.3635819, f32mul(0.4891775, c1))
		w = f32add(w, f32mul(0.1365995, c2))
		w = f32sub(w, f32mul(0.0106411, c3))
		window[n] = w
	}
}

// WindowRectangle — port of FLAC__window_rectangle (window.c:173).
func WindowRectangleFn(window []float32, L int32) {
	var n int32
	for n = 0; n < L; n++ {
		window[n] = 1.0
	}
}

// WindowTriangle — port of FLAC__window_triangle (window.c:181).
func WindowTriangleFn(window []float32, L int32) {
	var n int32
	if L&1 != 0 {
		for n = 1; n <= (L+1)/2; n++ {
			window[n-1] = f32div(f32mul(2.0, float32(n)), f32add(float32(L), 1.0))
		}
		for ; n <= L; n++ {
			window[n-1] = f32div(float32(2*(L-n+1)), f32add(float32(L), 1.0))
		}
	} else {
		for n = 1; n <= L/2; n++ {
			window[n-1] = f32div(f32mul(2.0, float32(n)), f32add(float32(L), 1.0))
		}
		for ; n <= L; n++ {
			window[n-1] = f32div(float32(2*(L-n+1)), f32add(float32(L), 1.0))
		}
	}
}

// WindowTukey — port of FLAC__window_tukey (window.c:199). Degenerates
// to rectangle (p<=0) / hann (p>=1) and is NaN-safe (defaults to 0.5).
func WindowTukeyFn(window []float32, L int32, p float32) {
	if p <= 0.0 {
		WindowRectangleFn(window, L)
	} else if p >= 1.0 {
		WindowHannFn(window, L)
	} else if !(p > 0.0 && p < 1.0) {
		WindowTukeyFn(window, L, 0.5)
	} else {
		// Np = (FLAC__int32)(p/2.0f * L) - 1
		Np := int32(f32mul(f32div(p, 2.0), float32(L))) - 1
		var n int32
		WindowRectangleFn(window, L)
		if Np > 0 {
			for n = 0; n <= Np; n++ {
				window[n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(n)/float64(Np))))
				window[L-Np-1+n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(n+Np)/float64(Np))))
			}
		}
	}
}

// WindowPartialTukey — port of FLAC__window_partial_tukey (window.c:224).
func WindowPartialTukeyFn(window []float32, L int32, p, start, end float32) {
	startN := int32(f32mul(start, float32(L)))
	endN := int32(f32mul(end, float32(L)))
	N := endN - startN
	var Np, n, i int32

	if p <= 0.0 {
		WindowPartialTukeyFn(window, L, 0.05, start, end)
	} else if p >= 1.0 {
		WindowPartialTukeyFn(window, L, 0.95, start, end)
	} else if !(p > 0.0 && p < 1.0) {
		WindowPartialTukeyFn(window, L, 0.5, start, end)
	} else {
		Np = int32(f32mul(f32div(p, 2.0), float32(N)))

		for n = 0; n < startN && n < L; n++ {
			window[n] = 0.0
		}
		for i = 1; n < (startN+Np) && n < L; n, i = n+1, i+1 {
			window[n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(i)/float64(Np))))
		}
		for ; n < (endN-Np) && n < L; n++ {
			window[n] = 1.0
		}
		for i = Np; n < endN && n < L; n, i = n+1, i-1 {
			window[n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(i)/float64(Np))))
		}
		for ; n < L; n++ {
			window[n] = 0.0
		}
	}
}

// WindowPunchoutTukey — port of FLAC__window_punchout_tukey (window.c:256).
func WindowPunchoutTukeyFn(window []float32, L int32, p, start, end float32) {
	startN := int32(f32mul(start, float32(L)))
	endN := int32(f32mul(end, float32(L)))
	var Ns, Ne, n, i int32

	if p <= 0.0 {
		WindowPunchoutTukeyFn(window, L, 0.05, start, end)
	} else if p >= 1.0 {
		WindowPunchoutTukeyFn(window, L, 0.95, start, end)
	} else if !(p > 0.0 && p < 1.0) {
		WindowPunchoutTukeyFn(window, L, 0.5, start, end)
	} else {
		Ns = int32(f32mul(f32div(p, 2.0), float32(startN)))
		Ne = int32(f32mul(f32div(p, 2.0), float32(L-endN)))

		for n, i = 0, 1; n < Ns && n < L; n, i = n+1, i+1 {
			window[n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(i)/float64(Ns))))
		}
		for ; n < startN-Ns && n < L; n++ {
			window[n] = 1.0
		}
		for i = Ns; n < startN && n < L; n, i = n+1, i-1 {
			window[n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(i)/float64(Ns))))
		}
		for ; n < endN && n < L; n++ {
			window[n] = 0.0
		}
		for i = 1; n < endN+Ne && n < L; n, i = n+1, i+1 {
			window[n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(i)/float64(Ne))))
		}
		for ; n < L-Ne && n < L; n++ {
			window[n] = 1.0
		}
		for i = Ne; n < L; n, i = n+1, i-1 {
			window[n] = f32sub(0.5, f32mul(0.5, cosfStrict(piF*float64(i)/float64(Ne))))
		}
	}
}

// WindowWelch — port of FLAC__window_welch (window.c:292).
func WindowWelchFn(window []float32, L int32) {
	N := L - 1
	N2 := float64(N) / 2.0
	var n int32
	for n = 0; n <= N; n++ {
		k := (float64(n) - N2) / N2
		window[n] = float32(f64sub(1.0, f64mul(k, k)))
	}
}

// ApplyWindow dispatches to the matching FLAC__window_* generator,
// mirroring the apodization switch in stream_encoder.c (line 2903).
// p is used by tukey / partial_tukey / punchout_tukey and as the
// gauss stddev; start/end are used by the partial/punchout tukey
// variants. Unused parameters are ignored.
func ApplyWindow(window []float32, L int32, typ WindowType, p, start, end float32) {
	switch typ {
	case WindowBartlett:
		WindowBartlettFn(window, L)
	case WindowBartlettHann:
		WindowBartlettHannFn(window, L)
	case WindowBlackman:
		WindowBlackmanFn(window, L)
	case WindowBlackmanHarris4Term92dBSidelobe:
		WindowBlackmanHarris4Term92dBSidelobeFn(window, L)
	case WindowConnes:
		WindowConnesFn(window, L)
	case WindowFlattop:
		WindowFlattopFn(window, L)
	case WindowGauss:
		WindowGaussFn(window, L, p)
	case WindowHamming:
		WindowHammingFn(window, L)
	case WindowHann:
		WindowHannFn(window, L)
	case WindowKaiserBessel:
		WindowKaiserBesselFn(window, L)
	case WindowNuttall:
		WindowNuttallFn(window, L)
	case WindowRectangle:
		WindowRectangleFn(window, L)
	case WindowTriangle:
		WindowTriangleFn(window, L)
	case WindowTukey:
		WindowTukeyFn(window, L, p)
	case WindowPartialTukey:
		WindowPartialTukeyFn(window, L, p, start, end)
	case WindowPunchoutTukey:
		WindowPunchoutTukeyFn(window, L, p, start, end)
	case WindowWelch:
		WindowWelchFn(window, L)
	default:
		// libFLAC asserts then falls back to hann (window.c via the
		// encoder default branch, stream_encoder.c:2958).
		WindowHannFn(window, L)
	}
}
