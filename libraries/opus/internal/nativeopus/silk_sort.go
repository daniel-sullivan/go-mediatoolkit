package nativeopus

// Port of libopus/silk/sort.c.
//
// Insertion-sort variants used throughout SILK for small arrays.
// Contract and name taken verbatim from the C source so callers
// transcribe identically.

// silk_insertion_sort_increasing — sort `a[:L]` in place in increasing
// order, additionally writing an index vector of the top K elements.
// The K-correctness guarantee matches the C behaviour: the first K
// positions of `a` and `idx` are correct; the remaining positions of
// `a` get shifted but are not fully sorted.
func silk_insertion_sort_increasing(a []opus_int32, idx []opus_int, L, K opus_int) {
	celt_assert(K > 0)
	celt_assert(L > 0)
	celt_assert(L >= K)

	// Write start indices in index vector.
	for i := opus_int(0); i < K; i++ {
		idx[i] = i
	}

	// Sort vector elements by value, increasing order.
	for i := opus_int(1); i < K; i++ {
		value := a[i]
		j := i - 1
		for ; j >= 0 && value < a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}

	// Check remaining values, but only ensure K first positions are
	// correct.
	for i := K; i < L; i++ {
		value := a[i]
		if value < a[K-1] {
			j := K - 2
			for ; j >= 0 && value < a[j]; j-- {
				a[j+1] = a[j]
				idx[j+1] = idx[j]
			}
			a[j+1] = value
			idx[j+1] = i
		}
	}
}

// silk_insertion_sort_decreasing_int16 — int16 variant, decreasing order.
func silk_insertion_sort_decreasing_int16(a []opus_int16, idx []opus_int, L, K opus_int) {
	celt_assert(K > 0)
	celt_assert(L > 0)
	celt_assert(L >= K)

	for i := opus_int(0); i < K; i++ {
		idx[i] = i
	}

	for i := opus_int(1); i < K; i++ {
		value := a[i]
		j := i - 1
		for ; j >= 0 && value > a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}

	for i := K; i < L; i++ {
		value := a[i]
		if value > a[K-1] {
			j := K - 2
			for ; j >= 0 && value > a[j]; j-- {
				a[j+1] = a[j]
				idx[j+1] = idx[j]
			}
			a[j+1] = value
			idx[j+1] = i
		}
	}
}

// silk_insertion_sort_increasing_all_values_int16 — sort the entire
// int16 array in increasing order (no index output).
func silk_insertion_sort_increasing_all_values_int16(a []opus_int16, L opus_int) {
	celt_assert(L > 0)
	for i := opus_int(1); i < L; i++ {
		value := a[i]
		j := i - 1
		for ; j >= 0 && value < a[j]; j-- {
			a[j+1] = a[j]
		}
		a[j+1] = value
	}
}
