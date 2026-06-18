// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.

// This file exposes thin exported views of the inverse-filterbank ROM tables
// (aac_rom_filterbank.go) as flat int16 [re,im,...] slices so the cgo parity
// oracle can assert they are byte-identical to the genuine narrowed C
// SineTable1024 / SineWindowNNN / KBDWindowNNN tables. They add no logic.

// flatFixSTP flattens a fixSTP table to [re0,im0,re1,im1,...] int16.
func flatFixSTP(t []fixSTP) []int16 {
	out := make([]int16, 2*len(t))
	for i, e := range t {
		out[2*i] = e.re
		out[2*i+1] = e.im
	}
	return out
}

// SineTable1024Flat returns the narrowed SineTable1024 (513 entries) as flat int16.
func SineTable1024Flat() []int16 { return flatFixSTP(sineTable1024[:]) }

// SineWindow1024Flat returns the narrowed SineWindow1024 (512 entries) as flat int16.
func SineWindow1024Flat() []int16 { return flatFixSTP(sineWindow1024[:]) }

// SineWindow128Flat returns the narrowed SineWindow128 (64 entries) as flat int16.
func SineWindow128Flat() []int16 { return flatFixSTP(sineWindow128[:]) }

// KBDWindow1024Flat returns the narrowed KBDWindow1024 (512 entries) as flat int16.
func KBDWindow1024Flat() []int16 { return flatFixSTP(kbdWindow1024[:]) }

// KBDWindow128Flat returns the narrowed KBDWindow128 (64 entries) as flat int16.
func KBDWindow128Flat() []int16 { return flatFixSTP(kbdWindow128[:]) }
