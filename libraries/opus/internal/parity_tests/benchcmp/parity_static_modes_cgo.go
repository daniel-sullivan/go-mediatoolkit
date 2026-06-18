//go:build cgo

package benchcmp

/*
#include "config.h"
#include "modes.h"
#include "kiss_fft.h"
#include "mdct.h"

// Accessors into the C static mode48000_960_120 obtained via
// opus_custom_mode_create, which in a CUSTOM_MODES=off build returns
// the pre-built static_mode_list[0].

static const CELTMode* cmode_get48(void) {
    int err = 0;
    return opus_custom_mode_create(48000, 960, &err);
}
static int cmode_Fs(const CELTMode *m) { return m->Fs; }
static int cmode_overlap(const CELTMode *m) { return m->overlap; }
static int cmode_nbEBands(const CELTMode *m) { return m->nbEBands; }
static int cmode_effEBands(const CELTMode *m) { return m->effEBands; }
static int cmode_maxLM(const CELTMode *m) { return m->maxLM; }
static int cmode_nbShortMdcts(const CELTMode *m) { return m->nbShortMdcts; }
static int cmode_shortMdctSize(const CELTMode *m) { return m->shortMdctSize; }
static int cmode_nbAllocVectors(const CELTMode *m) { return m->nbAllocVectors; }
static float cmode_preemph(const CELTMode *m, int i) { return m->preemph[i]; }
static short cmode_eBand(const CELTMode *m, int i) { return m->eBands[i]; }
static short cmode_logN(const CELTMode *m, int i) { return m->logN[i]; }
static unsigned char cmode_allocVec(const CELTMode *m, int i) { return m->allocVectors[i]; }
static float cmode_window(const CELTMode *m, int i) { return m->window[i]; }

static int cmode_mdct_n(const CELTMode *m) { return m->mdct.n; }
static int cmode_mdct_maxshift(const CELTMode *m) { return m->mdct.maxshift; }
static float cmode_mdct_trig(const CELTMode *m, int i) { return m->mdct.trig[i]; }
static int cmode_mdct_trig_len(const CELTMode *m) {
    return m->mdct.n - (m->mdct.n>>1 >> m->mdct.maxshift);
}

static int cmode_fft_nfft(const CELTMode *m, int s) { return m->mdct.kfft[s]->nfft; }
static float cmode_fft_scale(const CELTMode *m, int s) { return m->mdct.kfft[s]->scale; }
static int cmode_fft_shift(const CELTMode *m, int s) { return m->mdct.kfft[s]->shift; }
static short cmode_fft_factor(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->factors[i]; }
static short cmode_fft_bitrev(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->bitrev[i]; }
static float cmode_fft_twr(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->twiddles[i].r; }
static float cmode_fft_twi(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->twiddles[i].i; }

static int cmode_cache_size(const CELTMode *m) { return m->cache.size; }
static int cmode_cache_idx_len(const CELTMode *m) {
    return (m->maxLM+2) * m->nbEBands;
}
static short cmode_cache_idx(const CELTMode *m, int i) { return m->cache.index[i]; }
static unsigned char cmode_cache_bit(const CELTMode *m, int i) { return m->cache.bits[i]; }
static unsigned char cmode_cache_cap(const CELTMode *m, int i) { return m->cache.caps[i]; }
static int cmode_alloc_vec_len(const CELTMode *m) { return m->nbAllocVectors * m->nbEBands; }
*/
import "C"

// cStaticMode48 returns a wrapper around the C static 48 kHz mode.
func cStaticMode48() cStaticMode {
	return cStaticMode{p: C.cmode_get48()}
}

type cStaticMode struct{ p *C.CELTMode }

func (m cStaticMode) Fs() int32           { return int32(C.cmode_Fs(m.p)) }
func (m cStaticMode) Overlap() int        { return int(C.cmode_overlap(m.p)) }
func (m cStaticMode) NbEBands() int       { return int(C.cmode_nbEBands(m.p)) }
func (m cStaticMode) EffEBands() int      { return int(C.cmode_effEBands(m.p)) }
func (m cStaticMode) MaxLM() int          { return int(C.cmode_maxLM(m.p)) }
func (m cStaticMode) NbShortMdcts() int   { return int(C.cmode_nbShortMdcts(m.p)) }
func (m cStaticMode) ShortMdctSize() int  { return int(C.cmode_shortMdctSize(m.p)) }
func (m cStaticMode) NbAllocVectors() int { return int(C.cmode_nbAllocVectors(m.p)) }

func (m cStaticMode) Preemph() [4]float32 {
	var p [4]float32
	for i := 0; i < 4; i++ {
		p[i] = float32(C.cmode_preemph(m.p, C.int(i)))
	}
	return p
}

func (m cStaticMode) EBands() []int16 {
	n := m.NbEBands() + 1
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(C.cmode_eBand(m.p, C.int(i)))
	}
	return out
}

func (m cStaticMode) LogN() []int16 {
	n := m.NbEBands()
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(C.cmode_logN(m.p, C.int(i)))
	}
	return out
}

func (m cStaticMode) AllocVectors() []byte {
	n := int(C.cmode_alloc_vec_len(m.p))
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(C.cmode_allocVec(m.p, C.int(i)))
	}
	return out
}

func (m cStaticMode) Window() []float32 {
	n := m.Overlap()
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(C.cmode_window(m.p, C.int(i)))
	}
	return out
}

func (m cStaticMode) MdctN() int        { return int(C.cmode_mdct_n(m.p)) }
func (m cStaticMode) MdctMaxshift() int { return int(C.cmode_mdct_maxshift(m.p)) }
func (m cStaticMode) MdctTrig() []float32 {
	n := int(C.cmode_mdct_trig_len(m.p))
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(C.cmode_mdct_trig(m.p, C.int(i)))
	}
	return out
}

func (m cStaticMode) FftNfft(s int) int      { return int(C.cmode_fft_nfft(m.p, C.int(s))) }
func (m cStaticMode) FftScale(s int) float32 { return float32(C.cmode_fft_scale(m.p, C.int(s))) }
func (m cStaticMode) FftShift(s int) int     { return int(C.cmode_fft_shift(m.p, C.int(s))) }
func (m cStaticMode) FftFactors(s int) [16]int16 {
	var out [16]int16
	for i := 0; i < 16; i++ {
		out[i] = int16(C.cmode_fft_factor(m.p, C.int(s), C.int(i)))
	}
	return out
}

func (m cStaticMode) FftBitrev(s int) []int16 {
	n := m.FftNfft(s)
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(C.cmode_fft_bitrev(m.p, C.int(s), C.int(i)))
	}
	return out
}

// FftTwiddles returns the shift-0 state's twiddles; sub-states share it.
func (m cStaticMode) FftTwiddles(s int) [][2]float32 {
	n := m.FftNfft(0)
	_ = s
	out := make([][2]float32, n)
	for i := 0; i < n; i++ {
		out[i][0] = float32(C.cmode_fft_twr(m.p, 0, C.int(i)))
		out[i][1] = float32(C.cmode_fft_twi(m.p, 0, C.int(i)))
	}
	return out
}

func (m cStaticMode) CacheSize() int { return int(C.cmode_cache_size(m.p)) }
func (m cStaticMode) CacheIndex() []int16 {
	n := int(C.cmode_cache_idx_len(m.p))
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(C.cmode_cache_idx(m.p, C.int(i)))
	}
	return out
}
func (m cStaticMode) CacheBits() []byte {
	n := m.CacheSize()
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(C.cmode_cache_bit(m.p, C.int(i)))
	}
	return out
}
func (m cStaticMode) CacheCaps() []byte {
	// rate.c: cache->caps length = (LM+1)*2*nbEBands.
	n := (m.MaxLM() + 1) * 2 * m.NbEBands()
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(C.cmode_cache_cap(m.p, C.int(i)))
	}
	return out
}
