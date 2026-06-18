//go:build cgo

package benchcmp

/*
#include "config.h"
#include "quant_bands.h"
#include "entenc.h"
#include "entdec.h"

static void c_amp2Log2(const CELTMode *m, int effEnd, int end,
    celt_ener *bandE, celt_glog *bandLogE, int C) {
    amp2Log2(m, effEnd, end, bandE, bandLogE, C);
}

static void c_quant_coarse_energy(const CELTMode *m, int start, int end, int effEnd,
    const celt_glog *eBands, celt_glog *oldEBands, opus_uint32 budget,
    celt_glog *error, ec_enc *enc, int C, int LM,
    int nbAvailableBytes, int force_intra, opus_val32 *delayedIntra,
    int two_pass, int loss_rate, int lfe) {
    quant_coarse_energy(m, start, end, effEnd, eBands, oldEBands, budget,
        error, enc, C, LM, nbAvailableBytes, force_intra, delayedIntra,
        two_pass, loss_rate, lfe);
}
static void c_quant_fine_energy(const CELTMode *m, int start, int end,
    celt_glog *oldEBands, celt_glog *error, int *prev_quant, int *extra_quant,
    ec_enc *enc, int C) {
    quant_fine_energy(m, start, end, oldEBands, error, prev_quant, extra_quant, enc, C);
}
static void c_quant_energy_finalise(const CELTMode *m, int start, int end,
    celt_glog *oldEBands, celt_glog *error, int *fine_quant, int *fine_priority,
    int bits_left, ec_enc *enc, int C) {
    quant_energy_finalise(m, start, end, oldEBands, error, fine_quant, fine_priority, bits_left, enc, C);
}
static void c_unquant_coarse_energy(const CELTMode *m, int start, int end,
    celt_glog *oldEBands, int intra, ec_dec *dec, int C, int LM) {
    unquant_coarse_energy(m, start, end, oldEBands, intra, dec, C, LM);
}
static void c_unquant_fine_energy(const CELTMode *m, int start, int end,
    celt_glog *oldEBands, int *prev_quant, int *extra_quant, ec_dec *dec, int C) {
    unquant_fine_energy(m, start, end, oldEBands, prev_quant, extra_quant, dec, C);
}
static void c_unquant_energy_finalise(const CELTMode *m, int start, int end,
    celt_glog *oldEBands, int *fine_quant, int *fine_priority, int bits_left,
    ec_dec *dec, int C) {
    unquant_energy_finalise(m, start, end, oldEBands, fine_quant, fine_priority, bits_left, dec, C);
}
*/
import "C"
import "unsafe"

func cAmp2Log2(m cMode, effEnd, end int, bandE, bandLogE []float32, C_ int) {
	C.c_amp2Log2(m.p, C.int(effEnd), C.int(end),
		(*C.celt_ener)(unsafe.Pointer(&bandE[0])),
		(*C.celt_glog)(unsafe.Pointer(&bandLogE[0])), C.int(C_))
}

func cQuantCoarseEnergy(m cMode, start, end, effEnd int,
	eBands, oldEBands []float32, budget uint32, error_ []float32,
	enc cEc, C_, LM, nbAvail, forceIntra int,
	delayedIntra *float32, twoPass, lossRate, lfe int) {
	di := C.opus_val32(*delayedIntra)
	C.c_quant_coarse_energy(m.p, C.int(start), C.int(end), C.int(effEnd),
		(*C.celt_glog)(unsafe.Pointer(&eBands[0])),
		(*C.celt_glog)(unsafe.Pointer(&oldEBands[0])),
		C.opus_uint32(budget),
		(*C.celt_glog)(unsafe.Pointer(&error_[0])),
		enc.p, C.int(C_), C.int(LM), C.int(nbAvail), C.int(forceIntra),
		&di, C.int(twoPass), C.int(lossRate), C.int(lfe))
	*delayedIntra = float32(di)
}

func cQuantFineEnergy(m cMode, start, end int, oldEBands, error_ []float32,
	prevQuant, extraQuant []int, enc cEc, C_ int) {
	cPrev := make([]C.int, len(prevQuant))
	cExtra := make([]C.int, len(extraQuant))
	for i, v := range prevQuant {
		cPrev[i] = C.int(v)
	}
	for i, v := range extraQuant {
		cExtra[i] = C.int(v)
	}
	var prevPtr *C.int
	if len(cPrev) > 0 {
		prevPtr = (*C.int)(unsafe.Pointer(&cPrev[0]))
	}
	C.c_quant_fine_energy(m.p, C.int(start), C.int(end),
		(*C.celt_glog)(unsafe.Pointer(&oldEBands[0])),
		(*C.celt_glog)(unsafe.Pointer(&error_[0])),
		prevPtr,
		(*C.int)(unsafe.Pointer(&cExtra[0])),
		enc.p, C.int(C_))
}

func cQuantEnergyFinalise(m cMode, start, end int, oldEBands, error_ []float32,
	fineQuant, finePriority []int, bitsLeft int, enc cEc, C_ int) {
	cFq := make([]C.int, len(fineQuant))
	cFp := make([]C.int, len(finePriority))
	for i, v := range fineQuant {
		cFq[i] = C.int(v)
	}
	for i, v := range finePriority {
		cFp[i] = C.int(v)
	}
	C.c_quant_energy_finalise(m.p, C.int(start), C.int(end),
		(*C.celt_glog)(unsafe.Pointer(&oldEBands[0])),
		(*C.celt_glog)(unsafe.Pointer(&error_[0])),
		(*C.int)(unsafe.Pointer(&cFq[0])),
		(*C.int)(unsafe.Pointer(&cFp[0])),
		C.int(bitsLeft), enc.p, C.int(C_))
}

func cUnquantCoarseEnergy(m cMode, start, end int, oldEBands []float32,
	intra int, dec cEc, C_, LM int) {
	C.c_unquant_coarse_energy(m.p, C.int(start), C.int(end),
		(*C.celt_glog)(unsafe.Pointer(&oldEBands[0])),
		C.int(intra), dec.p, C.int(C_), C.int(LM))
}

func cUnquantFineEnergy(m cMode, start, end int, oldEBands []float32,
	prevQuant, extraQuant []int, dec cEc, C_ int) {
	cPrev := make([]C.int, len(prevQuant))
	cExtra := make([]C.int, len(extraQuant))
	for i, v := range prevQuant {
		cPrev[i] = C.int(v)
	}
	for i, v := range extraQuant {
		cExtra[i] = C.int(v)
	}
	var prevPtr *C.int
	if len(cPrev) > 0 {
		prevPtr = (*C.int)(unsafe.Pointer(&cPrev[0]))
	}
	C.c_unquant_fine_energy(m.p, C.int(start), C.int(end),
		(*C.celt_glog)(unsafe.Pointer(&oldEBands[0])),
		prevPtr,
		(*C.int)(unsafe.Pointer(&cExtra[0])),
		dec.p, C.int(C_))
}

func cUnquantEnergyFinalise(m cMode, start, end int, oldEBands []float32,
	fineQuant, finePriority []int, bitsLeft int, dec cEc, C_ int) {
	cFq := make([]C.int, len(fineQuant))
	cFp := make([]C.int, len(finePriority))
	for i, v := range fineQuant {
		cFq[i] = C.int(v)
	}
	for i, v := range finePriority {
		cFp[i] = C.int(v)
	}
	C.c_unquant_energy_finalise(m.p, C.int(start), C.int(end),
		(*C.celt_glog)(unsafe.Pointer(&oldEBands[0])),
		(*C.int)(unsafe.Pointer(&cFq[0])),
		(*C.int)(unsafe.Pointer(&cFp[0])),
		C.int(bitsLeft), dec.p, C.int(C_))
}
