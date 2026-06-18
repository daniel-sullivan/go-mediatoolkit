package inspection

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAutocorrelation(t *testing.T) {
	// Autocorrelation of a DC signal: result[k] = (N-k) * value^2
	data := []float64{3, 3, 3, 3, 3}
	r := Autocorrelation(data, 5)
	assert.InDelta(t, 45.0, r[0], 1e-10) // 5 * 9
	assert.InDelta(t, 36.0, r[1], 1e-10) // 4 * 9
	assert.InDelta(t, 27.0, r[2], 1e-10) // 3 * 9
	assert.InDelta(t, 18.0, r[3], 1e-10) // 2 * 9
	assert.InDelta(t, 9.0, r[4], 1e-10)  // 1 * 9
}

func TestAutocorrelationPeriodic(t *testing.T) {
	// A periodic signal should have peaks at multiples of the period.
	n := 200
	period := 20
	data := make([]float64, n)
	for i := range data {
		data[i] = math.Sin(2 * math.Pi * float64(i) / float64(period))
	}

	r := Autocorrelation(data, 60)

	// r[0] should be the largest.
	assert.Greater(t, r[0], r[1])

	// r[period] should be a local maximum (close to r[0] in magnitude).
	assert.Greater(t, r[period], r[period-1])
	assert.Greater(t, r[period], r[period+1])
}

func TestAutocorrelationMaxLagClamp(t *testing.T) {
	data := []float64{1, 2, 3}
	r := Autocorrelation(data, 100) // maxLag > len(data)
	assert.Len(t, r, 3)             // clamped to len(data)
}
