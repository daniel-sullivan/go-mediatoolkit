// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencanalysis

/*
#include <stdint.h>
extern void eparity_energy(int32_t *realFlat, int32_t *imagFlat, int numberBands,
                           int numberCols, int qmfScaleIn, int32_t *energyOut,
                           int *qmfScaleOut, int *energyScaleOut);
extern void eparity_energy_full(int32_t *realFlat, int32_t *imagFlat, int numberBands,
                                int numberCols, int qmfScaleIn, int32_t *energyOut,
                                int *qmfScaleOut, int *energyScaleOut);
extern int32_t eparity_tonality(const int32_t *quotaFlat, int totEst, int qmfChannels,
                                const int32_t *energyFlat, int numberCols, int noEstPerFrame,
                                int startIndex, int startBand, int stopBand);
extern int eparity_map_panorama(int nrgVal, int ampRes, int *quantError);
extern void eparity_noise_quant(const int32_t *noiseLevels, int coupling, signed char *out);
extern void eparity_couple_noise(int32_t *left, int32_t *right);
extern int32_t eparity_env_sfb_energy(int li, int ui, int startPos, int stopPos, int borderPos,
                                      int32_t *yFlat, int numYRows, int qmfChannels,
                                      int yBufferSzShift, int scaleNrg0, int scaleNrg1);
extern int32_t eparity_mh_lowering(int32_t nrg, int M);
extern int32_t eparity_nmh_lowering(int32_t nrg, int32_t nrgSum, int nrgSumScale, int M);
*/
import "C"

import "unsafe"

func cEnergy(realFlat, imagFlat []int32, numberBands, numberCols, qmfScaleIn int) (energy, realOut, imagOut []int32, qmfScale, energyScale int) {
	rc := append([]int32(nil), realFlat...)
	ic := append([]int32(nil), imagFlat...)
	energy = make([]int32, (numberCols/2)*numberBands)
	var qs, es C.int
	C.eparity_energy((*C.int32_t)(unsafe.Pointer(&rc[0])), (*C.int32_t)(unsafe.Pointer(&ic[0])),
		C.int(numberBands), C.int(numberCols), C.int(qmfScaleIn),
		(*C.int32_t)(unsafe.Pointer(&energy[0])), &qs, &es)
	return energy, rc, ic, int(qs), int(es)
}

func cEnergyFull(realFlat, imagFlat []int32, numberBands, numberCols, qmfScaleIn int) (energy, realOut, imagOut []int32, qmfScale, energyScale int) {
	rc := append([]int32(nil), realFlat...)
	ic := append([]int32(nil), imagFlat...)
	energy = make([]int32, numberCols*numberBands)
	var qs, es C.int
	C.eparity_energy_full((*C.int32_t)(unsafe.Pointer(&rc[0])), (*C.int32_t)(unsafe.Pointer(&ic[0])),
		C.int(numberBands), C.int(numberCols), C.int(qmfScaleIn),
		(*C.int32_t)(unsafe.Pointer(&energy[0])), &qs, &es)
	return energy, rc, ic, int(qs), int(es)
}

func cTonality(quotaFlat []int32, totEst, qmfChannels int, energyFlat []int32, numberCols, noEstPerFrame, startIndex, startBand, stopBand int) int32 {
	return int32(C.eparity_tonality((*C.int32_t)(unsafe.Pointer(&quotaFlat[0])), C.int(totEst), C.int(qmfChannels),
		(*C.int32_t)(unsafe.Pointer(&energyFlat[0])), C.int(numberCols), C.int(noEstPerFrame),
		C.int(startIndex), C.int(startBand), C.int(stopBand)))
}

func cMapPanorama(nrgVal, ampRes int) (pan, quantError int) {
	var qe C.int
	p := C.eparity_map_panorama(C.int(nrgVal), C.int(ampRes), &qe)
	return int(p), int(qe)
}

func cNoiseQuant(noiseLevels []int32, coupling int) []int8 {
	out := make([]C.schar, 10)
	C.eparity_noise_quant((*C.int32_t)(unsafe.Pointer(&noiseLevels[0])), C.int(coupling), &out[0])
	res := make([]int8, len(out))
	for i := range out {
		res[i] = int8(out[i])
	}
	return res
}

func cCoupleNoise(left, right []int32) (l, r []int32) {
	lc := append([]int32(nil), left...)
	rc := append([]int32(nil), right...)
	C.eparity_couple_noise((*C.int32_t)(unsafe.Pointer(&lc[0])), (*C.int32_t)(unsafe.Pointer(&rc[0])))
	return lc, rc
}

func cEnvSfbEnergy(li, ui, startPos, stopPos, borderPos int, yFlat []int32, numYRows, qmfChannels, yBufferSzShift, scaleNrg0, scaleNrg1 int) int32 {
	return int32(C.eparity_env_sfb_energy(C.int(li), C.int(ui), C.int(startPos), C.int(stopPos), C.int(borderPos),
		(*C.int32_t)(unsafe.Pointer(&yFlat[0])), C.int(numYRows), C.int(qmfChannels),
		C.int(yBufferSzShift), C.int(scaleNrg0), C.int(scaleNrg1)))
}

func cMhLowering(nrg int32, M int) int32 {
	return int32(C.eparity_mh_lowering(C.int32_t(nrg), C.int(M)))
}
func cNmhLowering(nrg, nrgSum int32, nrgSumScale, M int) int32 {
	return int32(C.eparity_nmh_lowering(C.int32_t(nrg), C.int32_t(nrgSum), C.int(nrgSumScale), C.int(M)))
}
