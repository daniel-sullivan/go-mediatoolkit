package nativeopus

// 1:1 port of libopus/silk/float/k2a_FLP.c.
// Step-up function converting reflection coefficients to prediction
// coefficients. All arithmetic is float32; each `a + b*c` in C compiled
// with -ffp-contract=off is two separate rounds — use fma_add.

func silk_k2a_FLP(A, rc []silk_float, order opus_int32) {
	var k, n opus_int32
	var rck, tmp1, tmp2 silk_float

	for k = 0; k < order; k++ {
		rck = rc[k]
		for n = 0; n < (k+1)>>1; n++ {
			tmp1 = A[n]
			tmp2 = A[k-n-1]
			// C: A[n]         = tmp1 + tmp2 * rck;
			// C: A[k-n-1]     = tmp2 + tmp1 * rck;
			A[n] = fma_add(tmp1, tmp2, rck)
			A[k-n-1] = fma_add(tmp2, tmp1, rck)
		}
		A[k] = -rck
	}
}
