package nativeopus

// 1:1 port of libopus/silk/float/bwexpander_FLP.c.
// Chirp (bw expand) LP AR filter in place.

func silk_bwexpander_FLP(ar []silk_float, d opus_int, chirp silk_float) {
	cfac := chirp
	for i := opus_int(0); i < d-1; i++ {
		ar[i] *= cfac
		cfac *= chirp
	}
	ar[d-1] *= cfac
}
