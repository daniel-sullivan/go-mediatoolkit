package mutations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go-mediatoolkit/mutations"
)

func TestDownmixStereoToMono(t *testing.T) {
	stereo := []float64{1, 3, 2, 4, 0.5, -0.5}
	dst := make([]float64, 3)
	n := mutations.DownmixStereoToMono(stereo, dst)
	assert.Equal(t, 3, n)
	assert.Equal(t, []float64{2, 3, 0}, dst)
}

func TestDownmixStereoToMonoShortDst(t *testing.T) {
	stereo := []float64{1, 3, 2, 4, 0.5, -0.5}
	dst := make([]float64, 2)
	n := mutations.DownmixStereoToMono(stereo, dst)
	assert.Equal(t, 2, n)
	assert.Equal(t, []float64{2, 3}, dst)
}

func TestUpmixMonoToStereo(t *testing.T) {
	mono := []float64{1, 2, 3, 4}
	dst := make([]float64, 8)
	n := mutations.UpmixMonoToStereo(mono, dst)
	assert.Equal(t, 8, n)
	assert.Equal(t, []float64{1, 1, 2, 2, 3, 3, 4, 4}, dst)
}

func TestUpmixMonoToStereoShortDst(t *testing.T) {
	mono := []float64{1, 2, 3, 4}
	dst := make([]float64, 4)
	n := mutations.UpmixMonoToStereo(mono, dst)
	assert.Equal(t, 4, n)
	assert.Equal(t, []float64{1, 1, 2, 2}, dst)
}
