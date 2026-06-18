// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine vendored libSBRdec/src/psdec.cpp as its own TU: CreatePsDec /
// ResetPsDec / PreparePsProcessing / initSlotBasedRotation / applySlotBasedRotation
// (static) / ApplyPsSlot — the parametric-stereo mono->stereo upmix. GetRam_ps_dec
// / FreeRam_ps_dec are provided by the bridge (not sbr_ram.cpp). See cgo.go.
#include "libfdk/libSBRdec/src/psdec.cpp"
