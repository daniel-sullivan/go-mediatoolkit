// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psdecapply

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// TestPsDecorrParity isolates the PS decorrelator (FDKdecorrelateInit(DECORR_PS)
// + per-slot FDKdecorrelateApply: the INDEP_CPLX_PS allpass cascade + the PS
// ducker) from the rest of the synthesis.
func TestPsDecorrParity(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	const nSlots = 32
	const nb = 71
	inRe := make([]int32, nSlots*nb)
	inImg := make([]int32, nSlots*nb)
	amp := int32(1 << 24)
	for i := range inRe {
		inRe[i] = int32(r.Int63())%amp - amp/2
		inImg[i] = int32(r.Int63())%amp - amp/2
	}

	cLRe, cLImg, cRRe, cRImg := cDecorr(nSlots, inRe, inImg)
	goLRe, goLImg, goRRe, goRImg := sbr.PsDecorrRun(nSlots, inRe, inImg)

	require.Equal(t, cLRe, goLRe, "decorr left real")
	require.Equal(t, cLImg, goLImg, "decorr left imag")
	require.Equal(t, cRRe, goRRe, "decorr right real")
	require.Equal(t, cRImg, goRImg, "decorr right imag")
}
