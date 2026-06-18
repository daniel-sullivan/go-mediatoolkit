//go:build cgo

package benchcmp

/*
#include "config.h"
#include "modes.h"
#include "rate.h"
#include "entcode.h"
#include "entenc.h"
#include "entdec.h"
#include <stdlib.h>
#include <string.h>

// Mode wrapper: fetch a standard static CELTMode for a given (Fs,
// frame_size) pair via opus_custom_mode_create. In a CUSTOM_MODES=off
// build this returns the matching static_mode_list entry.
static const CELTMode* rate_mode_get(int Fs, int frame_size) {
    int err = 0;
    return opus_custom_mode_create(Fs, frame_size, &err);
}

// Field getters for mirroring into Go.
static int rate_Fs(const CELTMode *m)            { return m->Fs; }
static int rate_overlap(const CELTMode *m)       { return m->overlap; }
static int rate_nbEBands(const CELTMode *m)      { return m->nbEBands; }
static int rate_effEBands(const CELTMode *m)     { return m->effEBands; }
static int rate_maxLM(const CELTMode *m)         { return m->maxLM; }
static int rate_nbShortMdcts(const CELTMode *m)  { return m->nbShortMdcts; }
static int rate_shortMdctSize(const CELTMode *m) { return m->shortMdctSize; }
static int rate_nbAllocVectors(const CELTMode *m){ return m->nbAllocVectors; }
static short rate_eBand(const CELTMode *m, int i){ return m->eBands[i]; }
static short rate_logN(const CELTMode *m, int i) { return m->logN[i]; }
static unsigned char rate_allocVec(const CELTMode *m, int i) { return m->allocVectors[i]; }
static int rate_cacheSize(const CELTMode *m)     { return m->cache.size; }
static short rate_cacheIdx(const CELTMode *m, int i) { return m->cache.index[i]; }
static unsigned char rate_cacheBit(const CELTMode *m, int i) { return m->cache.bits[i]; }
static unsigned char rate_cacheCap(const CELTMode *m, int i) { return m->cache.caps[i]; }
// allocVectors length = nbAllocVectors * nbEBands
static int rate_allocVecLen(const CELTMode *m)   { return m->nbAllocVectors*m->nbEBands; }
// cache.index length = (maxLM+2) * nbEBands per compute_pulse_cache loop.
static int rate_cacheIdxLen(const CELTMode *m)   { return (m->maxLM+2)*m->nbEBands; }

// Run clt_compute_allocation on the C mode, with a caller-provided
// ec_ctx + buffers. Returns codedBands.
static int c_compute_allocation(const CELTMode *m, int start, int end,
    const int *offsets, const int *cap, int alloc_trim,
    int *intensity, int *dual_stereo,
    opus_int32 total, opus_int32 *balance,
    int *pulses, int *ebits, int *fine_priority,
    int C, int LM, ec_ctx *ec, int encode, int prev, int signalBandwidth) {
    return clt_compute_allocation(m, start, end, offsets, cap, alloc_trim,
        intensity, dual_stereo, total, balance, pulses, ebits, fine_priority,
        C, LM, ec, encode, prev, signalBandwidth);
}

static int c_get_pulses(int i) { return get_pulses(i); }
static int c_bits2pulses(const CELTMode *m, int band, int LM, int bits) {
    return bits2pulses(m, band, LM, bits);
}
static int c_pulses2bits(const CELTMode *m, int band, int LM, int pulses) {
    return pulses2bits(m, band, LM, pulses);
}
*/
import "C"
import "unsafe"

type cMode struct{ p *C.CELTMode }

func cModeGet(Fs, frameSize int) cMode {
	return cMode{p: C.rate_mode_get(C.int(Fs), C.int(frameSize))}
}

func (m cMode) Fs() int             { return int(C.rate_Fs(m.p)) }
func (m cMode) Overlap() int        { return int(C.rate_overlap(m.p)) }
func (m cMode) NbEBands() int       { return int(C.rate_nbEBands(m.p)) }
func (m cMode) EffEBands() int      { return int(C.rate_effEBands(m.p)) }
func (m cMode) MaxLM() int          { return int(C.rate_maxLM(m.p)) }
func (m cMode) NbShortMdcts() int   { return int(C.rate_nbShortMdcts(m.p)) }
func (m cMode) ShortMdctSize() int  { return int(C.rate_shortMdctSize(m.p)) }
func (m cMode) NbAllocVectors() int { return int(C.rate_nbAllocVectors(m.p)) }
func (m cMode) EBand(i int) int16   { return int16(C.rate_eBand(m.p, C.int(i))) }
func (m cMode) LogN(i int) int16    { return int16(C.rate_logN(m.p, C.int(i))) }
func (m cMode) AllocVec(i int) byte { return byte(C.rate_allocVec(m.p, C.int(i))) }
func (m cMode) CacheSize() int      { return int(C.rate_cacheSize(m.p)) }
func (m cMode) CacheIdx(i int) int16 {
	return int16(C.rate_cacheIdx(m.p, C.int(i)))
}
func (m cMode) CacheBit(i int) byte {
	return byte(C.rate_cacheBit(m.p, C.int(i)))
}
func (m cMode) CacheCap(i int) byte {
	return byte(C.rate_cacheCap(m.p, C.int(i)))
}
func (m cMode) AllocVecLen() int { return int(C.rate_allocVecLen(m.p)) }
func (m cMode) CacheIdxLen() int { return int(C.rate_cacheIdxLen(m.p)) }

func cComputeAllocation(m cMode, start, end int, offsets, cap_ []int,
	allocTrim int, intensity, dualStereo *int,
	total int32, balance *int32,
	pulses, ebits, finePriority []int,
	C_ int, LM int, ec cEc, encode, prev, signalBandwidth int) int {

	cOffsets := make([]C.int, len(offsets))
	cCap := make([]C.int, len(cap_))
	cPulses := make([]C.int, len(pulses))
	cEbits := make([]C.int, len(ebits))
	cFp := make([]C.int, len(finePriority))
	for i, v := range offsets {
		cOffsets[i] = C.int(v)
	}
	for i, v := range cap_ {
		cCap[i] = C.int(v)
	}
	for i, v := range pulses {
		cPulses[i] = C.int(v)
	}
	for i, v := range ebits {
		cEbits[i] = C.int(v)
	}
	for i, v := range finePriority {
		cFp[i] = C.int(v)
	}
	cIntensity := C.int(*intensity)
	cDual := C.int(*dualStereo)
	cBalance := C.opus_int32(*balance)

	cb := C.c_compute_allocation(m.p,
		C.int(start), C.int(end),
		(*C.int)(unsafe.Pointer(&cOffsets[0])),
		(*C.int)(unsafe.Pointer(&cCap[0])),
		C.int(allocTrim),
		&cIntensity, &cDual,
		C.opus_int32(total), &cBalance,
		(*C.int)(unsafe.Pointer(&cPulses[0])),
		(*C.int)(unsafe.Pointer(&cEbits[0])),
		(*C.int)(unsafe.Pointer(&cFp[0])),
		C.int(C_), C.int(LM),
		ec.p, C.int(encode), C.int(prev), C.int(signalBandwidth))

	*intensity = int(cIntensity)
	*dualStereo = int(cDual)
	*balance = int32(cBalance)
	for i := range pulses {
		pulses[i] = int(cPulses[i])
	}
	for i := range ebits {
		ebits[i] = int(cEbits[i])
	}
	for i := range finePriority {
		finePriority[i] = int(cFp[i])
	}
	return int(cb)
}

func cGetPulses(i int) int { return int(C.c_get_pulses(C.int(i))) }
func cBits2Pulses(m cMode, band, LM, bits int) int {
	return int(C.c_bits2pulses(m.p, C.int(band), C.int(LM), C.int(bits)))
}
func cPulses2Bits(m cMode, band, LM, pulses int) int {
	return int(C.c_pulses2bits(m.p, C.int(band), C.int(LM), C.int(pulses)))
}
