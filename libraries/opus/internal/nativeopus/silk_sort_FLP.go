package nativeopus

// 1:1 port of libopus/silk/float/sort_FLP.c:silk_insertion_sort_decreasing_FLP.
// Partial insertion sort: sorts top K values in decreasing order and
// writes their original indices into idx. Stable tie behaviour matches C.

func silk_insertion_sort_decreasing_FLP(a []silk_float, idx []opus_int, L, K opus_int) {
	var value silk_float
	var i, j opus_int

	// Write start indices in index vector.
	for i = 0; i < K; i++ {
		idx[i] = i
	}

	// Sort vector elements by value, decreasing order.
	for i = 1; i < K; i++ {
		value = a[i]
		for j = i - 1; j >= 0 && value > a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}

	// Remaining values: only keep top K correct.
	for i = K; i < L; i++ {
		value = a[i]
		if value > a[K-1] {
			for j = K - 2; j >= 0 && value > a[j]; j-- {
				a[j+1] = a[j]
				idx[j+1] = idx[j]
			}
			a[j+1] = value
			idx[j+1] = i
		}
	}
}
