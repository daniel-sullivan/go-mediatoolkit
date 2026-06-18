// Package nativeflac is a 1:1 Go translation of the vendored libFLAC C
// reference implementation under libraries/flac/libflac. It exists so
// the public libraries/flac package can serve a pure-Go path when cgo
// is unavailable, and so the project's parity infrastructure can use
// the cgo build of libFLAC as a bit-exact oracle.
//
// # Strict mode
//
// The package supports two compilation modes selected by the
// `flac_strict` build tag:
//
//   - default (tag absent) — production build. There is no SIMD in the
//     port; this build simply lets Go's compiler backend auto-fuse a*b+c
//     into an FMA and inline plain float ops as it sees fit. Output is
//     within PSNR noise of the reference but not strictly bit-exact in
//     every corner.
//
//   - flac_strict — parity build. Every floating-point sequence is
//     decomposed into the same multiply / add ordering libFLAC's
//     scalar paths use, and FMA is disabled by routing each float
//     multiply/add through a //go:noinline helper so the backend
//     cannot fuse a*b+c into an FMADD. This is the build the
//     parity_tests/ directory uses to assert bit-exactness against the
//     cgo libFLAC oracle.
//
// The package is internal to libraries/flac. External code goes
// through libraries/flac.NewNativeDecoder / NewNativeEncoder.
//
// # Porting status (Phase 5)
//
// Foundational TUs land first; the encoder and decoder state machines
// follow once their dependencies have parity. Each function carries a
// reference comment to its libFLAC counterpart (file:line) so a future
// reader can diff against the C.
//
// Status:
//   - crc.c          — ported, parity verified
//   - bitmath.c      — ported, parity verified
//   - md5.c          — ported, parity verified
//   - format.c       — ported (validators + rice partition helpers +
//     VORBIS_COMMENT legality + the partitioned-rice-
//     contents lifecycle: PartitionedRiceContentsInit /
//     Clear / EnsureSize, and FormatSeektableIsLegal /
//     FormatCuesheetIsLegal / FormatPictureIsLegal),
//     parity verified for the validator/rice family.
//     FormatSeektableSort and the cuesheet/picture
//     `violation` out-param are still omitted (the
//     encoder consults only the bool).
//   - bitreader.c    — ported (every reader entry point + CRC track),
//     parity verified against the cgo bitreader. The
//     Go port uses a byte buffer rather than libFLAC's
//     uint64-word buffer; CRC results match because
//     [CRC16] over the byte slice is identical to
//     libFLAC's CRC16UpdateWords64.
//   - fixed.c        — restore_signal trio ported (orders 0–4, narrow /
//     wide / wide-33-bit), parity verified. Encoder
//     analysis ported (fixed_encode.go):
//     FixedComputeBestPredictor (32-bit accumulators)
//     and FixedComputeBestPredictorWide (64-bit),
//     parity verified under fixed_encode/. The forward
//     residual + limit-residual best-predictor variants
//     (FixedComputeResidual / _Wide / _Wide33Bit,
//     FixedComputeBestPredictorLimitResidual / _33Bit)
//     live in fixed_encode.go.
//   - lpc.c          — restore_signal trio + LPCMaxResidualBPS /
//     LPCMaxPredictionBeforeShiftBPS /
//     LPCMaxPredictionValueBeforeShift ported, parity
//     verified. Encoder analysis ported in lpc_encode.go:
//     LPCComputeAutocorrelation (both loop variants),
//     LPCComputeLPCoefficients (Levinson-Durbin),
//     LPCQuantizeCoefficients, and
//     LPCComputeResidualFromQLPCoefficients (+ _wide),
//     parity verified under lpc_encode/ (both tags).
//     Autocorrelation + Levinson are float64; parity is
//     asserted under flac_strict. FP-PARITY CONVENTION: the
//     cgo oracle is now compiled with -ffp-contract=off
//     (matching the opus port's parity build), so clang does
//     NOT contract any a*b+c in the Levinson recursion —
//     every multiply and add is separately rounded. The Go
//     strict build is correspondingly FMA-FREE: the f64
//     helper family (lpc_fp_strict.go: f64mul/f64add/f64sub
//     all //go:noinline, and f64fma = separately-rounded
//     a*b+c) routes each operation through a noinline
//     boundary so Go's SSA cannot fuse it. The default build
//     (lpc_fp_default.go) keeps a genuine fused math.FMA and
//     is not a parity target. The window-data application family
//     (LPCWindowData / _Wide / _Partial / _PartialWide),
//     LPCComputeExpectedBitsPerResidualSample (+
//     WithErrorScale), LPCComputeBestOrder, and the
//     limit-residual residual variants live in lpc_encode.go.
//   - window.c       — ported (window_fn.go): all 17 FLAC__window_*
//     generators (bartlett, bartlett_hann, blackman,
//     blackman_harris_4term_92db, connes, flattop, gauss,
//     hamming, hann, kaiser_bessel, nuttall, rectangle,
//     triangle, tukey, partial_tukey, punchout_tukey,
//     welch) + WindowType enum + ApplyWindow dispatch.
//     Strict-mode FP fidelity is split across
//     window_fp_strict.go (noinline scalar mul/add to
//     block FMA fusion) and window_fp_default.go.
//     Parity verified under window/ (flac_strict). Per the
//     FP parity convention (see below), the oracle now compiles
//     window.c with -ffp-contract=off and shims the single-
//     precision libm calls (cosf, fabsf) to their double kernel
//     narrowed ((float)cos((double)x)); the Go strict build
//     computes the same way (float32(math.Cos(float64(x))),
//     f32abs) and decomposes the polynomials through the
//     //go:noinline f32 helper family so no a*b+c fuses. With
//     both sides separately rounded and transcendentals matched,
//     all 17 generators — including the formerly-divergent cosf
//     windows (bartlett_hann, blackman, blackman_harris_4term_92db,
//     flattop, hamming, hann, kaiser_bessel, nuttall, tukey,
//     partial_tukey, punchout_tukey) — are bit-exact under
//     flac_strict. The DEFAULT build still diverges 1-2 ULP on a
//     handful of cosf-window coefficients (e.g. nuttall) because
//     it fuses float32 a+b*c into FMADDS; this is by design — the
//     default build is documented as NOT a bit-exact target.
//     subdivide_tukey and the apodization-string parser live in
//     stream_encoder.c.
//   - stream_decoder.c — ported. 5.7a frame structures + ReadFrameHeader;
//     5.7b ReadSubframeConstant / ReadSubframeVerbatim /
//     ReadResidualPartitionedRice / ReadZeroPadding;
//     5.7c ReadSubframeFixed / ReadSubframeLPC compose
//     the 5.6 restore_signal predictors (bit-exact
//     decoded samples confirmed by parity oracle);
//     5.7d undo_channel_coding + footer CRC (channel.go),
//     read_subframe_ dispatch incl. wasted-bits shift +
//     read_frame_ orchestration (decode_frame.go:
//     ReadSubframe / ReadFrame + FrameDecodeState);
//     5.7e decode-side metadata (metadata_decode.go:
//     FindMetadata / skipID3v2Tag / ReadMetadata +
//     readMetadataStreamInfo); 5.7f/5.7g the decoder
//     state machine + driver loop (decoder_state.go:
//     DecoderState/Error/Write enums + Decoder struct +
//     NewDecoder / InitStream / resetInternal;
//     decoder_stream.go: ProcessSingle /
//     ProcessUntilEndOfMetadata / ProcessUntilEndOfStream,
//     frameSync, readFrame, writeAudioFrameToClient with
//     running MD5, ensureFrameBuffers, Finish).
//     Per-TU parity packages landed for channel / frame /
//     metadata (all parity verified); the state machine +
//     driver are exercised by the decode_e2e capstone
//     (parity verified).
//   - stream_encoder.c — ported across four families.
//     A state/init/verify (encoder_state.go): owns the
//     StreamEncoder{prot,priv} state struct, the
//     StreamEncoder*Status enums, compressionLevels[],
//     NewStreamEncoder / Delete / setDefaults / free /
//     resizeBuffers / initStreamInternal + the disable_*/
//     get_* accessors + verify-FIFO + file-mode I/O
//     callbacks (single-thread, non-Ogg; Ogg paths are
//     no-op/unsupported per FLAC__HAS_OGG==0).
//     B subframe evaluation (encoder_subframe.go):
//     processSubframes_ /
//     processSubframe_ / applyApodization_ / the four
//     evaluate_* funcs / findBestPartitionOrder_ /
//     precomputePartitionInfo{Sums,Escapes}_ /
//     countRiceBitsInPartition_ (non-EXACT) /
//     setPartitionedRice_ / getWastedBits_(+Wide).
//     C frame/metadata writing (encoder_frame.go):
//     writeBitbuffer_ / writeFrame_ / updateMetadata_ /
//     updateOggMetadata_ (no-op) / addSubframe_;
//     spotcheck_subframe_estimate_ omitted
//     (SPOTCHECK_ESTIMATE==0). D stream I/O init +
//     callbacks (encoder_stream.go). Encoder parity is
//     covered by the encode_e2e capstone round-trip +
//     byte-identical comparison, parity verified under
//     flac_strict. The formerly-divergent cases
//     (stereo24_l5_bs4096, stereo16_l8_bs4096) were a single
//     root cascade — the 1-ULP window/cosf divergence and the
//     Levinson reflection/update arithmetic feeding LPC
//     autocorrelation -> quantized coefficients -> predictor/
//     channel decisions -> frame sizes. Both are resolved by the
//     FP parity convention (see below): the oracle is compiled
//     -ffp-contract=off with the window transcendentals shimmed,
//     and the Go strict Levinson + window paths are FMA-FREE via
//     the //go:noinline helper families, so encode_e2e is now
//     byte-identical under flac_strict. (The DEFAULT build remains
//     a non-target and may still diverge via FMADDS fusion.)
//     FormatSeektableSort is still unported.
//   - bitwriter.c    — ported (every writer entry point + word-buffer
//     growth + wide-accumulator rice-block path + CRC
//     track), parity verified against the cgo
//     bitwriter. Ported for the ENABLE_64_BIT_WORDS==1
//     config (64-bit bwword) that the vendored build
//     uses; the buffer is a []uint64 of host-order
//     words and GetBuffer reproduces libFLAC's exact
//     big-endian byte layout. Parity verified under
//     bitwriter/ (both tags).
//   - stream_encoder_framing.c — ported (encode_framing.go):
//     FLAC__add_metadata_block (STREAMINFO / PADDING /
//     APPLICATION / SEEKTABLE / VORBIS_COMMENT /
//     CUESHEET / PICTURE / unknown), FLAC__frame_add_header
//     (sync, block-size/sample-rate/channel/bps coding,
//     UTF-8 frame/sample number, CRC-8), and the
//     FLAC__subframe_add_* family (constant / fixed / lpc /
//     verbatim) + add_entropy_coding_method_ +
//     add_residual_partitioned_rice_. The metadata block
//     structs (StreamMetadata + SeekPoint/SeekTable/
//     Application/VorbisComment*/CueSheet*/Picture) live
//     alongside. libFLAC's vendor-string override is
//     preserved (VendorString). Parity verified
//     byte-identical vs libFLAC's framing functions
//     (framing/, both tags).
//
// New parity_tests packages landed this run (cgo-tagged,
// each self-contained — its own copy of the needed libFLAC .c TUs,
// never importing libraries/flac):
//   - parity_tests/channel       — UndoChannelCoding + frame-footer CRC [GREEN]
//   - parity_tests/frame         — read_subframe_/read_frame_ whole-frame decode [GREEN]
//   - parity_tests/metadata      — find_metadata_/read_metadata_ + STREAMINFO [GREEN]
//   - parity_tests/bitwriter     — every bitwriter entry point [GREEN]
//   - parity_tests/window        — all 17 window generators (Float32bits-exact)
//     [GREEN under flac_strict] — cosf-based windows now bit-exact via the FP
//     parity convention; DEFAULT tag still 1-2 ULP off on some cosf windows
//     (FMADDS fusion, by design — not a parity target)
//   - parity_tests/fixed_encode  — compute_best_predictor(+wide) [GREEN]
//   - parity_tests/lpc_encode    — autocorrelation/Levinson/quantize/residual [GREEN]
//   - parity_tests/framing       — FLAC__add_metadata_block + frame/subframe add [GREEN]
//   - parity_tests/decode_e2e    — native decoder vs libFLAC, end-to-end [GREEN]
//   - parity_tests/encode_e2e    — native encoder round-trip + byte-identical
//     [GREEN under flac_strict] — the 2 formerly-divergent cases now match via
//     the FP parity convention; DEFAULT tag may still diverge (non-target)
//
// # Phase 5 completion
//
// The cgo parity suite has been compiled and RUN under both the
// flac_strict and default tags. Results:
//
// GREEN (bit-exact under flac_strict — the parity target): channel, frame,
// metadata, decode_e2e, bitwriter, fixed_encode, lpc_encode, framing, window,
// encode_e2e. The pure-Go decoder and encoder are now fully parity-verified
// end-to-end, and every encoder analysis / framing / bitwriter unit is
// bit-exact under flac_strict. (channel, frame, metadata, decode_e2e,
// bitwriter, fixed_encode, lpc_encode, framing are additionally bit-exact
// under the DEFAULT tag.)
//
// DEFAULT-tag-only residual (BY DESIGN — the default build is documented as
// NOT a bit-exact target; these pass under flac_strict):
//   - window — a handful of cosf-window coefficients (e.g. nuttall) differ
//     1-2 ULP because the default build fuses float32 a+b*c into FMADDS in
//     the polynomial, whereas the oracle (-ffp-contract=off) rounds each op
//     separately.
//   - lpc_encode — Levinson-Durbin last-bit drift: the default f64fma uses a
//     genuine single-rounded math.FMA (lpc_fp_default.go) while the oracle
//     uses separately-rounded a*b then +c.
//
// These are intentional consequences of letting the default build fuse/SIMD
// for speed and are not regressions.
//
// # FP parity convention
//
// flac_strict parity follows the same floating-point convention proven in
// the libraries/opus port (see opus's fma_strict.go / blackbox/run.sh):
//
//   - Oracle compilation. The cgo parity oracle compiles its libFLAC TUs
//     with clang -ffp-contract=off -fno-vectorize -fno-slp-vectorize
//     -fno-unroll-loops. This removes FMA contraction AND the reassociation
//     license clang otherwise takes at -O2 (measured in Opus to cause 1-48
//     ULP drift in long reductions such as autocorrelation). Every C
//     multiply and add is then separately rounded.
//   - Go strict build is FMA-FREE. Every float multiply/add in the strict
//     build routes through a //go:noinline helper (window_fp_strict.go's
//     f32mul/f32div/f32sub family for single precision; lpc_fp_strict.go's
//     f64mul/f64add/f64sub + separately-rounded f64fma for double) so Go's
//     SSA backend cannot auto-fuse a*b+c into an FMADD. The DEFAULT build
//     (window_fp_default.go / lpc_fp_default.go) MAY fuse and SIMD and is
//     explicitly NOT a bit-exact target.
//   - Transcendentals shimmed. libFLAC's window.c calls single-precision
//     libm cosf, which is neither correctly-rounded nor portable. The
//     oracle is made correctly-rounded + portable by shimming cosf to its
//     double kernel narrowed: #define cosf(x) ((float)cos((double)(x))) in
//     every TU that includes window.c. The Go side computes identically —
//     float32(math.Cos(float64(x))) — so the two match bit-for-bit on
//     every platform. fabsf is NOT a transcendental and needs no double
//     kernel: it only clears the sign bit, so the Go side matches it with a
//     plain branch (f32abs returns -a for a<0 else a). The oracle's
//     #define fabsf(x) ((float)fabs((double)(x))) is bit-identical to that
//     branch because narrowing a sign-flipped double is exact.
//   - Invocation. The clang flags are supplied through the mise env
//     (CGO_CFLAGS + CGO_CFLAGS_ALLOW=".*"; see the repo-root mise.toml and
//     libraries/flac/mise.toml), NOT via in-source #cgo directives — Go's
//     cgo flag allowlist rejects -ffp-contract=off in source. Run the
//     bit-exact gate with `mise run //libraries/flac:parity` (or :test).
//     A bare `go test` without that env and `-tags flac_strict` is not the
//     parity gate and will diverge on the FP-heavy packages by design.
package nativeflac

// FLAC format limits, mirroring the FLAC__* macros in
// libflac/include/FLAC/format.h. Repeating them here lets the package
// build without depending on cgo headers.
const (
	MinBlockSize     = 16
	MaxBlockSize     = 65535
	MaxChannels      = 8
	MinBitsPerSample = 4
	MaxBitsPerSample = 32
	MaxSampleRate    = 1048575
	MaxLPCOrder      = 32

	// FrameSyncCode is the 14-bit sync at the start of every audio
	// frame, packed as the high 14 bits of a uint16 (with the low 2
	// bits = reserved + blocking strategy). The byte stream is
	// 0xFF, 0xF8 (fixed blocking) or 0xFF, 0xF9 (variable blocking).
	FrameSyncCode = 0x3FFE
)

// VendorString matches FLAC__VENDOR_STRING in format.c. Used by the
// encoder when it stamps a default vendor onto VORBIS_COMMENT blocks.
//
// libFLAC always overrides any caller-provided vendor with this value
// when update_vendor_string is set (FLAC__add_metadata_block,
// stream_encoder_framing.c:65–68 adjusts the framed length and :133–136
// writes FLAC__VENDOR_STRING); the native port matches that behaviour in
// AddMetadataBlock (encode_framing.go:157).
const VendorString = "reference libFLAC 1.5.0 20250211"
