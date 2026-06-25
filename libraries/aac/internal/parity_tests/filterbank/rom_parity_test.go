// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package filterbank

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// TestFilterbankROMParity asserts the pure-Go inverse-filterbank ROM tables
// (aac_rom_filterbank.go) — SineTable1024, SineWindow1024/128, KBDWindow1024/128
// — are byte-identical to the genuine narrowed C tables the radix-2 AAC-LC
// filterbank reaches. The C side is the FDKgetWindowSlope / dct_getTables ROM
// under the active SINETABLE_16BIT / WINDOWTABLE_16BIT config (int16 packed).
func TestFilterbankROMParity(t *testing.T) {
	// SineTable1024: dct_getTables sin_twiddle for every radix-2 length. The
	// dct oracle hands it back as the stCount-entry sin_twiddle for tl=1024
	// (sin_step 1), i.e. all 513 entries.
	_, cSine1024, _ := cDctTables(1024, 1024, sineTable1024Len)
	require.Equal(t, cSine1024, nativeaac.SineTable1024Flat(), "SineTable1024")

	// Window slopes via FDKgetWindowSlope(length, shape).
	require.Equal(t, cWindowSlope(1024, 0, 512), nativeaac.SineWindow1024Flat(), "SineWindow1024")
	require.Equal(t, cWindowSlope(128, 0, 64), nativeaac.SineWindow128Flat(), "SineWindow128")
	require.Equal(t, cWindowSlope(1024, 1, 512), nativeaac.KBDWindow1024Flat(), "KBDWindow1024")
	require.Equal(t, cWindowSlope(128, 1, 64), nativeaac.KBDWindow128Flat(), "KBDWindow128")
}
