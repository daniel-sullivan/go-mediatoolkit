// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// env_calc.cpp's calculateSbrEnvelope references expandPredEsg (pvc_dec.cpp) in
// the pvc_mode>0 branch — PVC is out of HE-AAC v1 scope. The oracle always sets
// pvc_mode==0, so the symbol is never CALLED, only referenced for linking. A
// link-only stub avoids pulling in the whole PVC subsystem. Never executed.
#include "pvc_dec.h"

void expandPredEsg(const PVC_DYNAMIC_DATA *pPvcDynamicData, const int timeSlot,
                   const int maxNoOfBands, FIXP_DBL *output, SCHAR *output_e) {
  (void)pPvcDynamicData; (void)timeSlot; (void)maxNoOfBands; (void)output; (void)output_e;
}
