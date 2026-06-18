package inspection

// Autocorrelation computes the autocorrelation of data at lags 0..maxLag-1.
// Returns a slice of length maxLag where result[k] = Σ data[i]*data[i+k].
// If maxLag exceeds len(data), it is clamped.
func Autocorrelation(data []float64, maxLag int) []float64 {
	n := len(data)
	if maxLag > n {
		maxLag = n
	}
	result := make([]float64, maxLag)
	for k := 0; k < maxLag; k++ {
		var sum float64
		for i := 0; i < n-k; i++ {
			sum += data[i] * data[i+k]
		}
		result[k] = sum
	}
	return result
}
