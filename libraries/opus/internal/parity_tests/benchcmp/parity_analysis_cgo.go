//go:build cgo

package benchcmp

/*
// Phase 9d analysis oracle — compile analysis.c and its direct
// dependencies with the same CFLAGS as the rest of benchcmp so
// bit-exact parity is achievable under -ffp-contract=off.
//
// Tested entry points (in parity_analysis_test.go):
//   - tonality_analysis_init       (post-init struct equality)
//   - tonality_get_info            (seeded state -> AnalysisInfo)
//   - silk_resampler_down2_hp      (3-tap all-pass downsampler)
//   - is_digital_silence           (silence detector, float branch)
// tonality_analysis / run_analysis full-pipeline parity is deferred;
// it requires the 480-point FFT + MLP forward-pass plumbing which
// lands later.

#include "config.h"
#include <stdlib.h>
#include <string.h>

// ─── Symbol rename block ─────────────────────────────────────────
// analysis.c, mlp.c, and mlp_data.c are source-included below. Their
// external symbols collide with any other TU that pulls in the same
// files (e.g. parity_mlp_cgo.go also #includes mlp.c under its own
// rename). Private rename'd names keep each TU's symbols separate.
#define tonality_analysis_init     tonality_analysis_init_oracle
#define tonality_analysis_reset    tonality_analysis_reset_oracle
#define tonality_get_info          tonality_get_info_oracle
#define run_analysis               run_analysis_oracle
#define tonality_analysis          tonality_analysis_oracle
#define downmix_and_resample       downmix_and_resample_oracle
#define silk_resampler_down2_hp    silk_resampler_down2_hp_oracle

#define analysis_compute_dense     analysis_compute_dense_a_oracle
#define analysis_compute_gru       analysis_compute_gru_a_oracle
#define gemm_accum                 gemm_accum_a_oracle
#define layer0                     layer0_a_oracle
#define layer1                     layer1_a_oracle
#define layer2                     layer2_a_oracle

// is_digital_silence lives in opus_encoder.c in production nativeopus.
// Rename it here so our local definition (below) becomes the
// oracle-private version and doesn't clash with any other TU/dylib.
#define is_digital_silence         is_digital_silence_a_oracle

// The source includes.
#include "mlp.c"
#include "mlp_data.c"
#include "analysis.c"

// Reimplement is_digital_silence (float branch) so the reference
// inside analysis.c's is_digital_silence32 macro resolves locally.
// Thanks to the #define above, the visible symbol is
// is_digital_silence_a_oracle.
int is_digital_silence(const opus_res* pcm, int frame_size, int channels, int lsb_depth)
{
   opus_val32 sample_max = celt_maxabs_res(pcm, frame_size*channels);
   return (sample_max <= (opus_val16) 1 / (1 << lsb_depth));
}

// ─── downmix_float oracle ────────────────────────────────────────
// run_analysis calls downmix_func — the standard float-branch
// downmix_float lives in opus_encoder.c which we don't source here.
// Reimplement it literally so the C oracle matches Go's downmix_float.
static void a_oracle_downmix_float(const void *_x, opus_val32 *y, int subframe,
                                   int offset, int c1, int c2, int C)
{
   const float *x = (const float *)_x;
   int j;
   for (j = 0; j < subframe; j++)
      y[j] = FLOAT2SIG(x[(j+offset)*C+c1]);
   if (c2 > -1) {
      for (j = 0; j < subframe; j++)
         y[j] += FLOAT2SIG(x[(j+offset)*C+c2]);
   } else if (c2 == -2) {
      int c;
      for (c = 1; c < C; c++) {
         for (j = 0; j < subframe; j++)
            y[j] += FLOAT2SIG(x[(j+offset)*C+c]);
      }
   }
   // Cap signal to +6 dBFS to avoid problems in the analysis.
   for (j = 0; j < subframe; j++) {
      if (y[j] < -65536.f) y[j] = -65536.f;
      if (y[j] >  65536.f) y[j] =  65536.f;
      if (celt_isnan(y[j])) y[j] = 0;
   }
}

// ─── 480-pt FFT allocation (private to analysis oracle TU) ──────
// Mirrors the CUSTOM_MODES-gated alloc helpers from kiss_fft.c so
// the analysis TU has its own 480-point kiss_fft_state to inject
// into a synthesized CELTMode. Duplicates parity_kiss_fft_cgo.go's
// logic; cgo TUs are compiled separately so statics don't leak.
static void a_compute_bitrev_table(int Fout, opus_int16 *f, size_t fstride,
    int in_stride, opus_int16 *factors) {
    int p = *factors++;
    int m = *factors++;
    if (m == 1) {
        for (int j = 0; j < p; j++) { *f = Fout+j; f += fstride*in_stride; }
    } else {
        for (int j = 0; j < p; j++) {
            a_compute_bitrev_table(Fout, f, fstride*p, in_stride, factors);
            f += fstride*in_stride;
            Fout += m;
        }
    }
}
static int a_kf_factor(int n, opus_int16 *facbuf) {
    int p = 4;
    int stages = 0;
    int nbak = n;
    do {
        while (n % p) {
            switch (p) { case 4: p = 2; break; case 2: p = 3; break;
                         default: p += 2; break; }
            if (p > 32000 || p*p > n) p = n;
        }
        n /= p;
        if (p > 5) return 0;
        facbuf[2*stages] = p;
        if (p == 2 && stages > 1) { facbuf[2*stages] = 4; facbuf[2] = 2; }
        stages++;
    } while (n > 1);
    n = nbak;
    for (int i = 0; i < stages/2; i++) {
        int tmp = facbuf[2*i];
        facbuf[2*i] = facbuf[2*(stages-i-1)];
        facbuf[2*(stages-i-1)] = tmp;
    }
    for (int i = 0; i < stages; i++) {
        n /= facbuf[2*i];
        facbuf[2*i+1] = n;
    }
    return 1;
}
static void a_compute_twiddles(kiss_twiddle_cpx *twiddles, int nfft) {
    for (int i = 0; i < nfft; i++) {
        const double pi = 3.14159265358979323846264338327;
        double phase = (-2*pi/nfft) * i;
        twiddles[i].r = (float)cos(phase);
        twiddles[i].i = (float)sin(phase);
    }
}
static kiss_fft_state* a_fft_alloc(int nfft) {
    kiss_fft_state *st = (kiss_fft_state*)calloc(1, sizeof(kiss_fft_state));
    st->nfft = nfft;
    st->scale = 1.f / nfft;
    st->shift = -1;
    kiss_twiddle_cpx *tw = (kiss_twiddle_cpx*)malloc(sizeof(kiss_twiddle_cpx)*nfft);
    a_compute_twiddles(tw, nfft);
    st->twiddles = tw;
    if (!a_kf_factor(nfft, st->factors)) { free(tw); free(st); return NULL; }
    opus_int16 *br = (opus_int16*)malloc(sizeof(opus_int16)*nfft);
    a_compute_bitrev_table(0, br, 1, 1, st->factors);
    st->bitrev = br;
    return st;
}
static void a_fft_free(kiss_fft_state *st) {
    if (!st) return;
    free((void*)st->bitrev);
    free((void*)st->twiddles);
    free(st);
}
// Field getters so Go-side can mirror the table contents exactly.
static int   a_fft_nfft(const kiss_fft_state *st)    { return st->nfft; }
static float a_fft_scale(const kiss_fft_state *st)   { return st->scale; }
static int   a_fft_shift(const kiss_fft_state *st)   { return st->shift; }
static short a_fft_factor(const kiss_fft_state *st, int i) { return st->factors[i]; }
static short a_fft_bitrev(const kiss_fft_state *st, int i) { return st->bitrev[i]; }
static float a_fft_twr(const kiss_fft_state *st, int i)    { return st->twiddles[i].r; }
static float a_fft_twi(const kiss_fft_state *st, int i)    { return st->twiddles[i].i; }

// ─── run_analysis wrapper ───────────────────────────────────────
// Synthesize a CELTMode whose only-populated field is mdct.kfft[0].
// run_analysis only consumes celt_mode->mdct.kfft[0] inside
// tonality_analysis; everything else goes unreferenced.
static void a_run_analysis(TonalityAnalysisState *st, kiss_fft_state *kfft,
                           const float *pcm, int analysis_frame_size,
                           int frame_size, int c1, int c2, int C,
                           opus_int32 Fs, int lsb_depth, AnalysisInfo *out) {
    CELTMode mode;
    memset(&mode, 0, sizeof(mode));
    mode.mdct.kfft[0] = kfft;
    mode.mdct.n = 0;        // unused by analysis
    mode.mdct.maxshift = 0; // unused
    run_analysis_oracle(st, &mode, pcm, analysis_frame_size, frame_size,
                        c1, c2, C, Fs, lsb_depth, a_oracle_downmix_float, out);
}

// AnalysisInfo snapshot getters extended with leak_boost access.
static unsigned char c_info_leak_boost(const AnalysisInfo *a, int i) { return a->leak_boost[i]; }

// ─── wrappers ────────────────────────────────────────────────────

static void c_tonality_analysis_init(TonalityAnalysisState *st, opus_int32 Fs) {
    tonality_analysis_init_oracle(st, Fs);
}

static size_t c_tonality_state_size(void) {
    return sizeof(TonalityAnalysisState);
}

// Getters.
static int   c_state_arch(const TonalityAnalysisState *s)          { return s->arch; }
static int   c_state_application(const TonalityAnalysisState *s)   { return s->application; }
static opus_int32 c_state_Fs(const TonalityAnalysisState *s)       { return s->Fs; }
static float c_state_angle(const TonalityAnalysisState *s, int i)  { return s->angle[i]; }
static float c_state_d_angle(const TonalityAnalysisState *s, int i){ return s->d_angle[i]; }
static float c_state_d2_angle(const TonalityAnalysisState *s, int i){return s->d2_angle[i]; }
static float c_state_inmem(const TonalityAnalysisState *s, int i)  { return s->inmem[i]; }
static int   c_state_mem_fill(const TonalityAnalysisState *s)      { return s->mem_fill; }
static float c_state_prev_band_tonality(const TonalityAnalysisState *s, int i) { return s->prev_band_tonality[i]; }
static float c_state_prev_tonality(const TonalityAnalysisState *s) { return s->prev_tonality; }
static int   c_state_prev_bandwidth(const TonalityAnalysisState *s){ return s->prev_bandwidth; }
static float c_state_lowE(const TonalityAnalysisState *s, int i)   { return s->lowE[i]; }
static float c_state_highE(const TonalityAnalysisState *s, int i)  { return s->highE[i]; }
static float c_state_meanE(const TonalityAnalysisState *s, int i)  { return s->meanE[i]; }
static float c_state_mem(const TonalityAnalysisState *s, int i)    { return s->mem[i]; }
static float c_state_cmean(const TonalityAnalysisState *s, int i)  { return s->cmean[i]; }
static float c_state_std(const TonalityAnalysisState *s, int i)    { return s->std[i]; }
static float c_state_Etracker(const TonalityAnalysisState *s)      { return s->Etracker; }
static float c_state_lowECount(const TonalityAnalysisState *s)     { return s->lowECount; }
static int   c_state_E_count(const TonalityAnalysisState *s)       { return s->E_count; }
static int   c_state_count(const TonalityAnalysisState *s)         { return s->count; }
static int   c_state_analysis_offset(const TonalityAnalysisState *s){return s->analysis_offset; }
static int   c_state_write_pos(const TonalityAnalysisState *s)     { return s->write_pos; }
static int   c_state_read_pos(const TonalityAnalysisState *s)      { return s->read_pos; }
static int   c_state_read_subframe(const TonalityAnalysisState *s) { return s->read_subframe; }
static float c_state_hp_ener_accum(const TonalityAnalysisState *s) { return s->hp_ener_accum; }
static int   c_state_initialized(const TonalityAnalysisState *s)   { return s->initialized; }
static float c_state_rnn_state(const TonalityAnalysisState *s, int i){return s->rnn_state[i]; }
static float c_state_downmix_state(const TonalityAnalysisState *s, int i){return s->downmix_state[i]; }
static float c_state_E(const TonalityAnalysisState *s, int f, int b) { return s->E[f][b]; }
static float c_state_logE(const TonalityAnalysisState *s, int f, int b) { return s->logE[f][b]; }
static int   c_state_info_valid(const TonalityAnalysisState *s, int i) { return s->info[i].valid; }
static float c_state_info_tonality(const TonalityAnalysisState *s, int i) { return s->info[i].tonality; }
static float c_state_info_music_prob(const TonalityAnalysisState *s, int i) { return s->info[i].music_prob; }
static float c_state_info_activity_prob(const TonalityAnalysisState *s, int i) { return s->info[i].activity_probability; }
static float c_state_info_tonality_slope(const TonalityAnalysisState *s, int i) { return s->info[i].tonality_slope; }
static float c_state_info_noisiness(const TonalityAnalysisState *s, int i) { return s->info[i].noisiness; }
static float c_state_info_activity(const TonalityAnalysisState *s, int i) { return s->info[i].activity; }
static int   c_state_info_bandwidth(const TonalityAnalysisState *s, int i) { return s->info[i].bandwidth; }

// Setters for seeding state prior to tonality_get_info.
static void c_state_set_count(TonalityAnalysisState *s, int v)         { s->count = v; }
static void c_state_set_write_pos(TonalityAnalysisState *s, int v)     { s->write_pos = v; }
static void c_state_set_read_pos(TonalityAnalysisState *s, int v)      { s->read_pos = v; }
static void c_state_set_read_subframe(TonalityAnalysisState *s, int v) { s->read_subframe = v; }
static void c_state_set_prev_bandwidth(TonalityAnalysisState *s, int v){ s->prev_bandwidth = v; }
static void c_state_set_info(TonalityAnalysisState *s, int i,
                             int valid, float tonality, int bandwidth,
                             float music_prob, float activity_prob) {
    s->info[i].valid = valid;
    s->info[i].tonality = tonality;
    s->info[i].bandwidth = bandwidth;
    s->info[i].music_prob = music_prob;
    s->info[i].activity_probability = activity_prob;
}

// Entry-point wrappers.
static void c_tonality_get_info(TonalityAnalysisState *s,
                                AnalysisInfo *out, int frame_size) {
    tonality_get_info_oracle(s, out, frame_size);
}

static opus_val32 c_silk_resampler_down2_hp(opus_val32 *S, opus_val32 *out,
                                             const opus_val32 *in, int inLen) {
    return silk_resampler_down2_hp_oracle(S, out, in, inLen);
}

static int c_is_digital_silence(const float *pcm, int frame_size, int channels, int lsb_depth) {
    return is_digital_silence_a_oracle(pcm, frame_size, channels, lsb_depth);
}

// AnalysisInfo accessors.
static int   c_info_valid(const AnalysisInfo *a)                { return a->valid; }
static float c_info_tonality(const AnalysisInfo *a)             { return a->tonality; }
static float c_info_tonality_slope(const AnalysisInfo *a)       { return a->tonality_slope; }
static float c_info_noisiness(const AnalysisInfo *a)            { return a->noisiness; }
static float c_info_activity(const AnalysisInfo *a)             { return a->activity; }
static float c_info_music_prob(const AnalysisInfo *a)           { return a->music_prob; }
static float c_info_music_prob_min(const AnalysisInfo *a)       { return a->music_prob_min; }
static float c_info_music_prob_max(const AnalysisInfo *a)       { return a->music_prob_max; }
static int   c_info_bandwidth(const AnalysisInfo *a)            { return a->bandwidth; }
static float c_info_activity_probability(const AnalysisInfo *a) { return a->activity_probability; }
static float c_info_max_pitch_ratio(const AnalysisInfo *a)      { return a->max_pitch_ratio; }
*/
import "C"
import "unsafe"

type cAnalysisState struct{ p *C.TonalityAnalysisState }

func cAnalysisStateNew() *cAnalysisState {
	sz := C.c_tonality_state_size()
	p := C.malloc(sz)
	C.memset(p, 0, sz)
	return &cAnalysisState{p: (*C.TonalityAnalysisState)(p)}
}

func (s *cAnalysisState) Free() {
	if s.p != nil {
		C.free(unsafe.Pointer(s.p))
		s.p = nil
	}
}

func cTonalityAnalysisInit(Fs int32) *cAnalysisState {
	s := cAnalysisStateNew()
	C.c_tonality_analysis_init(s.p, C.opus_int32(Fs))
	return s
}

type cAnalysisStateSeed struct {
	Count                   int
	WritePos                int
	ReadPos                 int
	ReadSubframe            int
	Fs                      int32
	PrevBandwidth           int
	InfoValid               []int32
	InfoTonality            []float32
	InfoBandwidth           []int32
	InfoMusicProb           []float32
	InfoActivityProbability []float32
}

func cTonalityGetInfoFromSeed(seed cAnalysisStateSeed, frameLen int) (valid int32,
	tonality, tonalitySlope, noisiness, activity, musicProb, musicProbMin, musicProbMax float32,
	bandwidth int32, activityProbability, maxPitchRatio float32) {
	s := cAnalysisStateNew()
	defer s.Free()
	C.c_tonality_analysis_init(s.p, C.opus_int32(seed.Fs))
	C.c_state_set_count(s.p, C.int(seed.Count))
	C.c_state_set_write_pos(s.p, C.int(seed.WritePos))
	C.c_state_set_read_pos(s.p, C.int(seed.ReadPos))
	C.c_state_set_read_subframe(s.p, C.int(seed.ReadSubframe))
	C.c_state_set_prev_bandwidth(s.p, C.int(seed.PrevBandwidth))
	for i := 0; i < len(seed.InfoValid); i++ {
		C.c_state_set_info(s.p, C.int(i),
			C.int(seed.InfoValid[i]),
			C.float(seed.InfoTonality[i]),
			C.int(seed.InfoBandwidth[i]),
			C.float(seed.InfoMusicProb[i]),
			C.float(seed.InfoActivityProbability[i]))
	}
	var info C.AnalysisInfo
	C.c_tonality_get_info(s.p, &info, C.int(frameLen))
	return int32(C.c_info_valid(&info)),
		float32(C.c_info_tonality(&info)),
		float32(C.c_info_tonality_slope(&info)),
		float32(C.c_info_noisiness(&info)),
		float32(C.c_info_activity(&info)),
		float32(C.c_info_music_prob(&info)),
		float32(C.c_info_music_prob_min(&info)),
		float32(C.c_info_music_prob_max(&info)),
		int32(C.c_info_bandwidth(&info)),
		float32(C.c_info_activity_probability(&info)),
		float32(C.c_info_max_pitch_ratio(&info))
}

func cSilkResamplerDown2HP(S, out, in []float32) float32 {
	if len(S) != 3 {
		panic("S must be len 3")
	}
	if len(in) == 0 {
		return 0
	}
	if len(out) < len(in)/2 {
		panic("out too small")
	}
	ret := C.c_silk_resampler_down2_hp(
		(*C.opus_val32)(unsafe.Pointer(&S[0])),
		(*C.opus_val32)(unsafe.Pointer(&out[0])),
		(*C.opus_val32)(unsafe.Pointer(&in[0])),
		C.int(len(in)))
	return float32(ret)
}

func cIsDigitalSilence(pcm []float32, lsbDepth int) int {
	if len(pcm) == 0 {
		return int(C.c_is_digital_silence(nil, 0, 1, C.int(lsbDepth)))
	}
	return int(C.c_is_digital_silence(
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(len(pcm)), C.int(1), C.int(lsbDepth)))
}

type cAnalysisInitSnapshot struct {
	Arch              int
	Application       int
	Fs                int32
	MemFill           int
	PrevBandwidth     int
	PrevTonality      float32
	Etracker          float32
	LowECount         float32
	ECount            int
	Count             int
	AnalysisOffset    int
	WritePos          int
	ReadPos           int
	ReadSubframe      int
	HpEnerAccum       float32
	Initialized       int
	Angle             [240]float32
	DAngle            [240]float32
	D2Angle           [240]float32
	PrevBandTonality  [18]float32
	LowE              [18]float32
	HighE             [18]float32
	MeanE             [19]float32
	Mem               [32]float32
	Cmean             [8]float32
	Std               [9]float32
	DownmixState      [3]float32
	RnnState          [32]float32
	Inmem             [720]float32
	E                 [8 * 18]float32
	LogE              [8 * 18]float32
	InfoValid         [100]int32
	InfoTonality      [100]float32
	InfoMusicProb     [100]float32
	InfoActivityProb  [100]float32
	InfoTonalitySlope [100]float32
	InfoNoisiness     [100]float32
	InfoActivity      [100]float32
	InfoBandwidth     [100]int32
}

func cSnapshotInit(s *cAnalysisState) cAnalysisInitSnapshot {
	var snap cAnalysisInitSnapshot
	snap.Arch = int(C.c_state_arch(s.p))
	snap.Application = int(C.c_state_application(s.p))
	snap.Fs = int32(C.c_state_Fs(s.p))
	snap.MemFill = int(C.c_state_mem_fill(s.p))
	snap.PrevBandwidth = int(C.c_state_prev_bandwidth(s.p))
	snap.PrevTonality = float32(C.c_state_prev_tonality(s.p))
	snap.Etracker = float32(C.c_state_Etracker(s.p))
	snap.LowECount = float32(C.c_state_lowECount(s.p))
	snap.ECount = int(C.c_state_E_count(s.p))
	snap.Count = int(C.c_state_count(s.p))
	snap.AnalysisOffset = int(C.c_state_analysis_offset(s.p))
	snap.WritePos = int(C.c_state_write_pos(s.p))
	snap.ReadPos = int(C.c_state_read_pos(s.p))
	snap.ReadSubframe = int(C.c_state_read_subframe(s.p))
	snap.HpEnerAccum = float32(C.c_state_hp_ener_accum(s.p))
	snap.Initialized = int(C.c_state_initialized(s.p))
	for i := 0; i < 240; i++ {
		snap.Angle[i] = float32(C.c_state_angle(s.p, C.int(i)))
		snap.DAngle[i] = float32(C.c_state_d_angle(s.p, C.int(i)))
		snap.D2Angle[i] = float32(C.c_state_d2_angle(s.p, C.int(i)))
	}
	for i := 0; i < 18; i++ {
		snap.PrevBandTonality[i] = float32(C.c_state_prev_band_tonality(s.p, C.int(i)))
		snap.LowE[i] = float32(C.c_state_lowE(s.p, C.int(i)))
		snap.HighE[i] = float32(C.c_state_highE(s.p, C.int(i)))
	}
	for i := 0; i < 19; i++ {
		snap.MeanE[i] = float32(C.c_state_meanE(s.p, C.int(i)))
	}
	for i := 0; i < 32; i++ {
		snap.Mem[i] = float32(C.c_state_mem(s.p, C.int(i)))
		snap.RnnState[i] = float32(C.c_state_rnn_state(s.p, C.int(i)))
	}
	for i := 0; i < 8; i++ {
		snap.Cmean[i] = float32(C.c_state_cmean(s.p, C.int(i)))
	}
	for i := 0; i < 9; i++ {
		snap.Std[i] = float32(C.c_state_std(s.p, C.int(i)))
	}
	for i := 0; i < 3; i++ {
		snap.DownmixState[i] = float32(C.c_state_downmix_state(s.p, C.int(i)))
	}
	for i := 0; i < 720; i++ {
		snap.Inmem[i] = float32(C.c_state_inmem(s.p, C.int(i)))
	}
	for f := 0; f < 8; f++ {
		for b := 0; b < 18; b++ {
			snap.E[f*18+b] = float32(C.c_state_E(s.p, C.int(f), C.int(b)))
			snap.LogE[f*18+b] = float32(C.c_state_logE(s.p, C.int(f), C.int(b)))
		}
	}
	for i := 0; i < 100; i++ {
		snap.InfoValid[i] = int32(C.c_state_info_valid(s.p, C.int(i)))
		snap.InfoTonality[i] = float32(C.c_state_info_tonality(s.p, C.int(i)))
		snap.InfoMusicProb[i] = float32(C.c_state_info_music_prob(s.p, C.int(i)))
		snap.InfoActivityProb[i] = float32(C.c_state_info_activity_prob(s.p, C.int(i)))
		snap.InfoTonalitySlope[i] = float32(C.c_state_info_tonality_slope(s.p, C.int(i)))
		snap.InfoNoisiness[i] = float32(C.c_state_info_noisiness(s.p, C.int(i)))
		snap.InfoActivity[i] = float32(C.c_state_info_activity(s.p, C.int(i)))
		snap.InfoBandwidth[i] = int32(C.c_state_info_bandwidth(s.p, C.int(i)))
	}
	return snap
}

// ─── run_analysis oracle helpers ───────────────────────────────────

// cAnalysisFFT — opaque handle to a C-side 480-point kiss_fft_state.
type cAnalysisFFT struct{ p *C.kiss_fft_state }

func cAnalysisFFTAlloc(nfft int) cAnalysisFFT {
	return cAnalysisFFT{p: C.a_fft_alloc(C.int(nfft))}
}

func (h cAnalysisFFT) Free() {
	if h.p != nil {
		C.a_fft_free(h.p)
		h.p = nil
	}
}

func (h cAnalysisFFT) Nfft() int          { return int(C.a_fft_nfft(h.p)) }
func (h cAnalysisFFT) Scale() float32     { return float32(C.a_fft_scale(h.p)) }
func (h cAnalysisFFT) Shift() int         { return int(C.a_fft_shift(h.p)) }
func (h cAnalysisFFT) Factor(i int) int16 { return int16(C.a_fft_factor(h.p, C.int(i))) }
func (h cAnalysisFFT) Bitrev(i int) int16 { return int16(C.a_fft_bitrev(h.p, C.int(i))) }
func (h cAnalysisFFT) Twr(i int) float32  { return float32(C.a_fft_twr(h.p, C.int(i))) }
func (h cAnalysisFFT) Twi(i int) float32  { return float32(C.a_fft_twi(h.p, C.int(i))) }

// cAnalysisInfoSnapshot — flat AnalysisInfo mirror (includes leak_boost).
type cAnalysisInfoSnapshot struct {
	Valid               int32
	Tonality            float32
	TonalitySlope       float32
	Noisiness           float32
	Activity            float32
	MusicProb           float32
	MusicProbMin        float32
	MusicProbMax        float32
	Bandwidth           int32
	ActivityProbability float32
	MaxPitchRatio       float32
	LeakBoost           [19]byte // LEAK_BANDS
}

func cSnapshotInfo(info *C.AnalysisInfo) cAnalysisInfoSnapshot {
	snap := cAnalysisInfoSnapshot{
		Valid:               int32(C.c_info_valid(info)),
		Tonality:            float32(C.c_info_tonality(info)),
		TonalitySlope:       float32(C.c_info_tonality_slope(info)),
		Noisiness:           float32(C.c_info_noisiness(info)),
		Activity:            float32(C.c_info_activity(info)),
		MusicProb:           float32(C.c_info_music_prob(info)),
		MusicProbMin:        float32(C.c_info_music_prob_min(info)),
		MusicProbMax:        float32(C.c_info_music_prob_max(info)),
		Bandwidth:           int32(C.c_info_bandwidth(info)),
		ActivityProbability: float32(C.c_info_activity_probability(info)),
		MaxPitchRatio:       float32(C.c_info_max_pitch_ratio(info)),
	}
	for i := 0; i < len(snap.LeakBoost); i++ {
		snap.LeakBoost[i] = byte(C.c_info_leak_boost(info, C.int(i)))
	}
	return snap
}

// cRunAnalysisFloat drives the C-side run_analysis oracle.
func cRunAnalysisFloat(s *cAnalysisState, fft cAnalysisFFT, pcm []float32,
	analysisFrameSize, frameSize, c1, c2, C_ int, Fs int32, lsbDepth int) cAnalysisInfoSnapshot {
	var info C.AnalysisInfo
	var pcmPtr *C.float
	if len(pcm) > 0 {
		pcmPtr = (*C.float)(unsafe.Pointer(&pcm[0]))
	}
	C.a_run_analysis(s.p, fft.p, pcmPtr,
		C.int(analysisFrameSize), C.int(frameSize),
		C.int(c1), C.int(c2), C.int(C_),
		C.opus_int32(Fs), C.int(lsbDepth), &info)
	return cSnapshotInfo(&info)
}
