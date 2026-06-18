/*
 * oracle.h — C oracle surface for the psychoacoustic-model parity slice.
 *
 * The floating-point foundation of LAME 3.100's psychoacoustic model is the
 * FFT/FHT in libmp3lame/fft.c: fft_long / fft_short window a granule of PCM and
 * run fht() (Ron Mayer's fast Hartley transform, fft.c:62) to produce the real
 * spectra the model squares into the per-line energies that drive masking,
 * attack detection and the block-type decision. fht() is THE per-frame FP
 * kernel the whole model is built on, so it is the bit-exactness pin for the
 * "psychoacoustic-model" area.
 *
 * fht() is file-static inside fft.c, so there is no public C API that reaches
 * it. Per the parity discipline in CONTRIBUTING.md
 * this oracle TU (oracle.c) #includes the vendored libmp3lame/fft.c directly —
 * bringing the static fht into scope — and re-exports it through the thin
 * oracle_fht wrapper declared below. The wrapper lives in the same translation
 * unit as the static and adds no logic, so the C side of every assertion is
 * the genuine vendored LAME code, not a hand reimplementation.
 *
 * Including fft.c pulls in the whole LAME header tree (lame.h, machine.h,
 * encoder.h, util.h, fft.h), so FLOAT, BLKSIZE and BLKSIZE_s are defined for
 * us; the wrapper signature uses float* so the Go cgo bridge needs none of
 * those headers. (FLOAT is `float` per liblame/config.h SIZEOF_FLOAT 4, so
 * `float *` is ABI-identical to `FLOAT *`.)
 *
 * Inputs are fabricated directly as the raw FLOAT working buffer fht operates
 * on: fft_long calls fht(x, BLKSIZE/2) over a BLKSIZE-long buffer and
 * fft_short calls fht(x, BLKSIZE_s/2) over a BLKSIZE_s-long buffer (fft.c:244,
 * fft.c:297), so the parity test fills a buffer of 2*n random floats and runs
 * both the C oracle_fht and the Go nativemp3.Fht over identical bytes,
 * asserting the in-place result is bit-identical.
 */
#ifndef MP3_PSYMODEL_ORACLE_H
#define MP3_PSYMODEL_ORACLE_H

/* oracle_fht runs the vendored static fht() in place over fz, a buffer of 2*n
 * floats (n = the value fft_long/fft_short pass: BLKSIZE/2 or BLKSIZE_s/2).
 * float* is ABI-identical to LAME's FLOAT* (SIZEOF_FLOAT == 4). */
void oracle_fht(float *fz, int n);

#endif /* MP3_PSYMODEL_ORACLE_H */
