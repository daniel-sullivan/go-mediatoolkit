// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psdecapply

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// TestInlineFixpCosSinParity isolates the inline_fixp_cos_sin rotation kernel
// (FDK_trigFcts.h) used by initSlotBasedRotation, asserting the Go
// nativeaac.InlineFixpCosSin matches the genuine C bit-for-bit at scale==2 (the
// PS call's scale) over random angles.
func TestInlineFixpCosSinParity(t *testing.T) {
	r := rand.New(rand.NewSource(99))
	for it := 0; it < 20000; it++ {
		x1 := int32(r.Uint32())
		x2 := int32(r.Uint32())
		want := cCosSin(x1, x2, 2)
		var got [4]int32
		nativeaac.InlineFixpCosSin(x1, x2, 2, got[:])
		require.Equalf(t, want, got, "it=%d x1=%d x2=%d", it, x1, x2)
	}
}
