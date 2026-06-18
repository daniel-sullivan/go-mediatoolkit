// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// PsInitRotRun mirrors the C qparity_initRot probe: CreatePsDec + ReadPsData +
// DecodePs + initSlotBasedRotation(env 0), returning the resulting H / Delta
// coefficient arrays (NO_IID_GROUPS each) and the DecodePs flag.
func PsInitRotRun(pBuffer []byte, bufSize, validBits uint32, usb int) (h11, h12, h21, h22, d11, d12, d21, d22 []int32, flag int) {
	h := new(psDec)
	if createPsDec(h, 1024) != 0 {
		return
	}
	h.BsLastSlot = 0
	h.BsReadSlot = 0
	h.ProcessSlot = 0
	h.ProcFrameBased = 0

	bs := newSbrBitStream(pBuffer, bufSize, validBits)
	readPsData(h, bs, int(validBits))

	var coef psDecCoefficients
	flag = decodePs(h, 0, &coef)
	h11 = make([]int32, psNoIidGroups)
	h12 = make([]int32, psNoIidGroups)
	h21 = make([]int32, psNoIidGroups)
	h22 = make([]int32, psNoIidGroups)
	d11 = make([]int32, psNoIidGroups)
	d12 = make([]int32, psNoIidGroups)
	d21 = make([]int32, psNoIidGroups)
	d22 = make([]int32, psNoIidGroups)
	if flag == 1 {
		initSlotBasedRotation(h, 0, usb)
		for g := 0; g < psNoIidGroups; g++ {
			h11[g] = coef.H11r[g]
			h12[g] = coef.H12r[g]
			h21[g] = coef.H21r[g]
			h22[g] = coef.H22r[g]
			d11[g] = coef.DeltaH11r[g]
			d12[g] = coef.DeltaH12r[g]
			d21[g] = coef.DeltaH21r[g]
			d22[g] = coef.DeltaH22r[g]
		}
	}
	return
}
