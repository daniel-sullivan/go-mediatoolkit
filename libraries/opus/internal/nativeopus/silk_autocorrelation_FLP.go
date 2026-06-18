package nativeopus

// 1:1 port of libopus/silk/float/autocorrelation_FLP.c.
// Computes the autocorrelation as inner products of the input with
// itself at increasing lags.

func silk_autocorrelation_FLP(results, inputData []silk_float, inputDataSize, correlationCount opus_int, arch int) {
	if correlationCount > inputDataSize {
		correlationCount = inputDataSize
	}
	for i := opus_int(0); i < correlationCount; i++ {
		results[i] = silk_float(silk_inner_product_FLP(inputData, inputData[i:], inputDataSize-i, arch))
	}
}
