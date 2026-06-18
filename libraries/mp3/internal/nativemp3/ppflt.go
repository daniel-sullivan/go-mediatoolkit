// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "math"

// ppflt.go — a 1:1 Go translation of LAME 3.100's lame_init_params_ppflt
// (libmp3lame/lame.c:104-192) and its filter_coef helper (lame.c:93-102). It
// designs the polyphase low/high-pass filter: it band-quantizes cfg.Lowpass1 /
// Lowpass2 (and Highpass1 / Highpass2) to the *actual* transition band the 32-band
// polyphase filterbank implements, then fills sv_enc.amp_filter[32] — the per-band
// amplitude weights mdct_sub48 (mdct_analysis_filterbank.go) multiplies the subband
// samples by, zeroing the bands above lowpass2.
//
// lame_init_params (init.go:524) reaches it through the EncoderStages seam exactly
// where lame.c:910 calls lame_init_params_ppflt(gfc), after cfg.Lowpass1/Lowpass2
// are seeded from lowpassfreq (init.go:507-521 == lame.c:888-901). The amp_filter
// fill is load-bearing for byte-identical output: without it the high subbands keep
// their unfiltered energy and the bit allocation inflates.
//
// FP discipline: filter_coef is cos(PI/2 * x) — PI/2 is a double constant, x a
// FLOAT(float32) promoted to double, one product, one cos, narrowed back to
// float32 (math.Cos is bit-identical to the oracle libm per the package FP
// convention). The amp_filter product fc1*fc2 is a single float32 multiply. The
// band/lowpass arithmetic divides by the double literal 31.0 (lame.c:121/134-139)
// except the per-band freq loop, which uses the float literal 31.0f (lame.c:177).

// filterCoef is lame.c:93-102's filter_coef. x is FLOAT(float32); the cosine is
// evaluated in double precision (PI/2 * (double)x) and narrowed to float32.
func filterCoef(x float32) float32 {
	if x > 1.0 {
		return 0.0
	}
	if x <= 0.0 {
		return 1.0
	}
	return float32(math.Cos(math.Pi / 2 * float64(x)))
}

// initParamsPpflt is lame_init_params_ppflt (lame.c:104-192).
func (gfc *LameInternalFlags) initParamsPpflt() {
	cfg := &gfc.Cfg

	lowpassBand := 32
	highpassBand := -1

	if cfg.Lowpass1 > 0 {
		minband := 999
		for band := 0; band <= 31; band++ {
			freq := float32(float64(band) / 31.0)
			// this band and above will be zeroed:
			if freq >= cfg.Lowpass2 {
				lowpassBand = minInt(lowpassBand, band)
			}
			if cfg.Lowpass1 < freq && freq < cfg.Lowpass2 {
				minband = minInt(minband, band)
			}
		}
		// compute the *actual* transition band implemented by the polyphase filter.
		if minband == 999 {
			cfg.Lowpass1 = float32((float64(lowpassBand) - 0.75) / 31.0)
		} else {
			cfg.Lowpass1 = float32((float64(minband) - 0.75) / 31.0)
		}
		cfg.Lowpass2 = float32(float64(lowpassBand) / 31.0)
	}

	// make sure highpass filter is within 90% of the effective highpass frequency.
	if cfg.Highpass2 > 0 {
		if cfg.Highpass2 < 0.9*(0.75/31.0) {
			cfg.Highpass1 = 0
			cfg.Highpass2 = 0
		}
	}

	if cfg.Highpass2 > 0 {
		maxband := -1
		for band := 0; band <= 31; band++ {
			freq := float32(float64(band) / 31.0)
			// this band and below will be zeroed:
			if freq <= cfg.Highpass1 {
				highpassBand = maxInt(highpassBand, band)
			}
			if cfg.Highpass1 < freq && freq < cfg.Highpass2 {
				maxband = maxInt(maxband, band)
			}
		}
		// compute the *actual* transition band implemented by the polyphase filter.
		cfg.Highpass1 = float32(float64(highpassBand) / 31.0)
		if maxband == -1 {
			cfg.Highpass2 = float32((float64(highpassBand) + 0.75) / 31.0)
		} else {
			cfg.Highpass2 = float32((float64(maxband) + 0.75) / 31.0)
		}
	}

	for band := 0; band < 32; band++ {
		var fc1, fc2 float32
		freq := float32(band) / 31.0 // lame.c:177 uses 31.0f
		if cfg.Highpass2 > cfg.Highpass1 {
			fc1 = filterCoef((cfg.Highpass2 - freq) / (cfg.Highpass2 - cfg.Highpass1 + 1e-20))
		} else {
			fc1 = 1.0
		}
		if cfg.Lowpass2 > cfg.Lowpass1 {
			fc2 = filterCoef((freq - cfg.Lowpass1) / (cfg.Lowpass2 - cfg.Lowpass1 + 1e-20))
		} else {
			fc2 = 1.0
		}
		gfc.SvEnc.AmpFilter[band] = fc1 * fc2
	}
}
