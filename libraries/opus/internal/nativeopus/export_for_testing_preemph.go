package nativeopus

// Pre-emphasis and tf-analysis shims so benchcmp can exercise encoder
// helpers on their own and isolate drift.

// ExportTestTfAnalysis runs tf_analysis on a caller-supplied X[].
// Returns tf_select and fills tf_res[].
func ExportTestTfAnalysis(h CeltModeHandle, length, isTransient int, tf_res []int,
	lambda int, X []float32, N0, LM int, tf_estimate float32, tf_chan int,
	importance []int) int {
	return tf_analysis(h.p, length, isTransient, tf_res, lambda, X, N0, LM,
		opus_val16(tf_estimate), tf_chan, importance)
}

// ExportTestTransientAnalysis runs transient_analysis on a caller-
// supplied `in` buffer. Writes tf_estimate, tf_chan, weak_transient;
// returns isTransient.
func ExportTestTransientAnalysis(in []float32, length, C int,
	tf_estimate *float32, tf_chan *int, allow_weak int,
	weak_transient *int, tone_freq float32, toneishness float32) int {
	te := opus_val16(*tf_estimate)
	r := transient_analysis(in, length, C, &te, tf_chan, allow_weak,
		weak_transient, opus_val16(tone_freq), opus_val32(toneishness),
		make([]opus_val16, length))
	*tf_estimate = float32(te)
	return r
}

// ExportTestToneDetect runs tone_detect. Returns freq; writes toneishness.
func ExportTestToneDetect(in []float32, CC, N int, toneishness *float32,
	Fs int32) float32 {
	var t opus_val32
	r := tone_detect(in, CC, N, &t, opus_int32(Fs))
	*toneishness = float32(t)
	return float32(r)
}

func ExportTestCeltPreemphasis(pcm []float32, inp []float32, N, CC, upsample int,
	coef [4]float32, mem *float32, clip int) {
	var m opus_val32 = opus_val32(*mem)
	var c [4]opus_val16
	for i, v := range coef {
		c[i] = opus_val16(v)
	}
	celt_preemphasis(pcm, inp, N, CC, upsample, c[:], &m, clip)
	*mem = float32(m)
}
