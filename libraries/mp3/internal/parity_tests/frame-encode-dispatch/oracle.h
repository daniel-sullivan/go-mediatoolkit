/*
 * oracle.h — C API the frame-encode-dispatch parity tests call into.
 *
 * Declares an opaque handle over a heap lame_internal_flags and trampolines
 * that drive the genuine vendored lame_encode_mp3_frame / lame_encode_frame_init
 * (compiled in oracle.c via #include "encoder.c"). The Go side configures the
 * session, the padding accumulator, the PE-smoothing history and the fabricated
 * per-granule psy-model outputs, runs one frame through the genuine dispatcher,
 * then reads back the observable results (padding, slot_lag, mode_ext, the
 * block-type flags, the rolled PE-firbuf, frame_number, the latched
 * frame-init flag and the primebuff the filterbank prime shifted).
 *
 * The handle layout is opaque to Go; cgo.go only passes the pointer back.
 */

#ifndef MP3PARITY_FRAME_ENCODE_DISPATCH_ORACLE_H
#define MP3PARITY_FRAME_ENCODE_DISPATCH_ORACLE_H

typedef struct mp3parity_fe_t mp3parity_fe_t;

mp3parity_fe_t *mp3parity_fe_new(void);
void mp3parity_fe_free(mp3parity_fe_t *h);

void mp3parity_fe_set_cfg(mp3parity_fe_t *h, int samplerate_out, int channels_out,
                          int mode_gr, int mode, int force_ms, int vbr, int write_lame_tag);
void mp3parity_fe_set_pad(mp3parity_fe_t *h, int frac_spf, int slot_lag);
void mp3parity_fe_set_pefirbuf(mp3parity_fe_t *h, const float *buf19);
void mp3parity_fe_set_frame_init(mp3parity_fe_t *h, int v);
void mp3parity_fe_set_psy(int gr, const float *pe2, const float *pe_ms2,
                          const float *tot_ener4, const int *blocktype2);
void mp3parity_fe_set_psy_ret(int ret);
void mp3parity_fe_arm_capture(void);

void mp3parity_fe_reset_capture(void);
void mp3parity_fe_alloc_input(mp3parity_fe_t *h, int inlen);
void mp3parity_fe_set_input(mp3parity_fe_t *h, int ch, const float *vals, int n);

int mp3parity_fe_encode(mp3parity_fe_t *h);

int   mp3parity_fe_padding(const mp3parity_fe_t *h);
int   mp3parity_fe_slot_lag(const mp3parity_fe_t *h);
int   mp3parity_fe_mode_ext(const mp3parity_fe_t *h);
int   mp3parity_fe_frame_number(const mp3parity_fe_t *h);
int   mp3parity_fe_frame_init(const mp3parity_fe_t *h);
float mp3parity_fe_pefirbuf(const mp3parity_fe_t *h, int i);
int   mp3parity_fe_block_type(const mp3parity_fe_t *h, int gr, int ch);
int   mp3parity_fe_mixed_block_flag(const mp3parity_fe_t *h, int gr, int ch);

int   mp3parity_fe_mdct_calls(void);
float mp3parity_fe_prime0(int i);
float mp3parity_fe_prime1(int i);

#endif /* MP3PARITY_FRAME_ENCODE_DISPATCH_ORACLE_H */
