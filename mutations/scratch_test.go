package mutations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go-mediatoolkit/mutations"
)

func TestResizeScratchAllocatesWhenCapSmall(t *testing.T) {
	buf := make([]float64, 0, 4)
	got := mutations.ResizeScratch(buf, 10)
	assert.Len(t, got, 10)
	assert.GreaterOrEqual(t, cap(got), 10)
}

func TestResizeScratchReusesWhenCapSufficient(t *testing.T) {
	buf := make([]float64, 0, 32)
	got := mutations.ResizeScratch(buf, 10)
	assert.Len(t, got, 10)
	assert.Equal(t, 32, cap(got))
	// Ensure it aliases the original backing array.
	got[0] = 42
	got = got[:cap(got)] // extend to full cap
	assert.Equal(t, 42.0, got[0])
}

func TestResizeScratchNilInput(t *testing.T) {
	got := mutations.ResizeScratch(nil, 5)
	assert.Len(t, got, 5)
}

func TestResizeScratchShrink(t *testing.T) {
	buf := make([]float64, 20)
	got := mutations.ResizeScratch(buf, 5)
	assert.Len(t, got, 5)
	assert.Equal(t, 20, cap(got))
}
