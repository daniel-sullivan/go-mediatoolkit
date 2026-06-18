package nativeflac

// Family A of the 1:1 port of libflac/src/libFLAC/stream_encoder.c: the
// encoder state machine scaffolding. This file owns the shared
// FLAC__StreamEncoder-equivalent Go state struct (StreamEncoder +
// streamEncoderProtected + streamEncoderPrivate + StreamEncoderThreadTask),
// the encoder/init/read/write/seek/tell status enums, the
// CompressionLevels table, the lifecycle (new/delete/set_defaults_/free_),
// resize_buffers_, init_stream_internal_'s validation + verify-decoder
// setup, the disable_*/get_* accessors, the verify FIFO helpers, and the
// file-mode I/O callbacks. The per-block pump (process*), frame writing
// (write_bitbuffer_/write_frame_/update_metadata_), and subframe
// evaluation live in encoder_stream.go (family D), encoder_frame.go
// (family C), and encoder_subframe.go (family B) respectively and code
// against the field names defined here.
//
// Single-thread parity target: every HAVE_PTHREAD field/branch is omitted;
// only threadtask[0] and num_threadtasks == 1 are modelled. The local_*
// function-pointer indirection in the C Private struct collapses to direct
// calls in the pure-Go families (cpuinfo / SIMD selection is irrelevant);
// those fields are not represented. Ogg paths (FLAC__HAS_OGG == 0 in the
// vendored single build) are ported as no-op / unsupported.

import "io"

// FLAC format limits referenced by the encoder, mirroring the FLAC__*
// macros in FLAC/format.h that stream_encoder.c uses.
const (
	// MinQLPCoeffPrecision / MaxQLPCoeffPrecision — FLAC__MIN/MAX_QLP_COEFF_PRECISION
	// (format.h:137,142).
	MinQLPCoeffPrecision = 5
	MaxQLPCoeffPrecision = 15

	// SubsetMaxBlockSize48000Hz / SubsetMaxLPCOrder48000Hz /
	// SubsetMaxRicePartitionOrder — FLAC__SUBSET_* (format.h:103,132,151).
	SubsetMaxBlockSize48000Hz   = 4608
	SubsetMaxLPCOrder48000Hz    = 12
	SubsetMaxRicePartitionOrder = 8

	// EntropyCodingMethodPartitionedRiceOrderLen ==
	// FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN (format.h:240).
	EntropyCodingMethodPartitionedRiceOrderLen = 4

	// StreamMetadataStreamInfoLength == FLAC__STREAM_METADATA_STREAMINFO_LENGTH
	// (format.h:557): the 34-byte STREAMINFO body length.
	StreamMetadataStreamInfoLength = 34

	// MaxApodizationFunctions == FLAC__MAX_APODIZATION_FUNCTIONS
	// (protected/stream_encoder.h:45).
	MaxApodizationFunctions = 32

	// MaxFixedOrderPlus1 == FLAC__MAX_FIXED_ORDER+1, the residual-bits
	// array size for the fixed-predictor evaluation.
	MaxFixedOrderPlus1 = MaxFixedOrder + 1

	// overread is OVERREAD_ (stream_encoder.c:576). The process*() calls
	// always read blocksize+1 samples so the last block is detected even
	// when total samples is a multiple of blocksize. Some code asserts
	// it equals 1.
	overread = 1
)

// EncoderStateHint — port of EncoderStateHint (stream_encoder.c:113).
// Tracks which phase of the stream the verify decoder is fed from.
type EncoderStateHint uint8

const (
	encoderInMagic    EncoderStateHint = 0
	encoderInMetadata EncoderStateHint = 1
	encoderInAudio    EncoderStateHint = 2
)

// StreamEncoderState — port of FLAC__StreamEncoderState
// (FLAC/stream_encoder.h:243). Ordinals match the C enum.
type StreamEncoderState uint8

const (
	StreamEncoderOK StreamEncoderState = iota
	StreamEncoderUninitialized
	StreamEncoderOggError
	StreamEncoderVerifyDecoderError
	StreamEncoderVerifyMismatchInAudioData
	StreamEncoderClientError
	StreamEncoderIOError
	StreamEncoderFramingError
	StreamEncoderMemoryAllocationError
)

// streamEncoderStateString — port of FLAC__StreamEncoderStateString
// (stream_encoder.c:512). Indexed by StreamEncoderState.
var streamEncoderStateString = [...]string{
	"FLAC__STREAM_ENCODER_OK",
	"FLAC__STREAM_ENCODER_UNINITIALIZED",
	"FLAC__STREAM_ENCODER_OGG_ERROR",
	"FLAC__STREAM_ENCODER_VERIFY_DECODER_ERROR",
	"FLAC__STREAM_ENCODER_VERIFY_MISMATCH_IN_AUDIO_DATA",
	"FLAC__STREAM_ENCODER_CLIENT_ERROR",
	"FLAC__STREAM_ENCODER_IO_ERROR",
	"FLAC__STREAM_ENCODER_FRAMING_ERROR",
	"FLAC__STREAM_ENCODER_MEMORY_ALLOCATION_ERROR",
}

// StreamEncoderInitStatus — port of FLAC__StreamEncoderInitStatus
// (FLAC/stream_encoder.h:298). Ordinals match the C enum.
type StreamEncoderInitStatus uint8

const (
	StreamEncoderInitStatusOK StreamEncoderInitStatus = iota
	StreamEncoderInitStatusEncoderError
	StreamEncoderInitStatusUnsupportedContainer
	StreamEncoderInitStatusInvalidCallbacks
	StreamEncoderInitStatusInvalidNumberOfChannels
	StreamEncoderInitStatusInvalidBitsPerSample
	StreamEncoderInitStatusInvalidSampleRate
	StreamEncoderInitStatusInvalidBlockSize
	StreamEncoderInitStatusInvalidMaxLPCOrder
	StreamEncoderInitStatusInvalidQLPCoeffPrecision
	StreamEncoderInitStatusBlockSizeTooSmallForLPCOrder
	StreamEncoderInitStatusNotStreamable
	StreamEncoderInitStatusInvalidMetadata
	StreamEncoderInitStatusAlreadyInitialized
)

// StreamEncoderReadStatus — port of FLAC__StreamEncoderReadStatus
// (FLAC/stream_encoder.h:367). Used only by the Ogg-FLAC read path.
type StreamEncoderReadStatus uint8

const (
	StreamEncoderReadStatusContinue StreamEncoderReadStatus = iota
	StreamEncoderReadStatusEndOfStream
	StreamEncoderReadStatusAbort
	StreamEncoderReadStatusUnsupported
)

// StreamEncoderWriteStatus — port of FLAC__StreamEncoderWriteStatus
// (FLAC/stream_encoder.h:393). Returned by the write callback.
type StreamEncoderWriteStatus uint8

const (
	StreamEncoderWriteStatusOK StreamEncoderWriteStatus = iota
	StreamEncoderWriteStatusFatalError
)

// StreamEncoderSeekStatus — port of FLAC__StreamEncoderSeekStatus
// (FLAC/stream_encoder.h:413).
type StreamEncoderSeekStatus uint8

const (
	StreamEncoderSeekStatusOK StreamEncoderSeekStatus = iota
	StreamEncoderSeekStatusError
	StreamEncoderSeekStatusUnsupported
)

// StreamEncoderTellStatus — port of FLAC__StreamEncoderTellStatus
// (FLAC/stream_encoder.h:436).
type StreamEncoderTellStatus uint8

const (
	StreamEncoderTellStatusOK StreamEncoderTellStatus = iota
	StreamEncoderTellStatusError
	StreamEncoderTellStatusUnsupported
)

// StreamEncoderSetNumThreadsStatus — port of
// FLAC__StreamEncoderSetNumThreadsStatus (FLAC/stream_encoder.h:291-294).
// Returned by SetNumThreads; the pure-Go port is single-threaded so only
// OK and NotCompiledWithMultithreadingEnabled are reachable.
type StreamEncoderSetNumThreadsStatus uint32

const (
	EncoderSetNumThreadsOK StreamEncoderSetNumThreadsStatus = iota
	EncoderSetNumThreadsNotCompiledWithMultithreadingEnabled
	EncoderSetNumThreadsAlreadyInitialized
	EncoderSetNumThreadsTooManyThreads
)

// Encoder callback signatures, mirroring the libFLAC
// FLAC__StreamEncoder*Callback typedefs the encoder holds on its Private
// struct. The Go port passes the encoder explicitly to each, plus the
// caller's clientData, matching the C callbacks' void *client_data tail.
type (
	// StreamEncoderWriteCallback — port of FLAC__StreamEncoderWriteCallback
	// (FLAC/stream_encoder.h). write_frame_ invokes it with the framed
	// bytes, the sample count of the block (0 for metadata), and the
	// running frame number.
	StreamEncoderWriteCallback func(enc *StreamEncoder, buffer []byte, samples, currentFrame uint32, clientData any) StreamEncoderWriteStatus
	// StreamEncoderSeekCallback — port of FLAC__StreamEncoderSeekCallback.
	StreamEncoderSeekCallback func(enc *StreamEncoder, absoluteByteOffset uint64, clientData any) StreamEncoderSeekStatus
	// StreamEncoderTellCallback — port of FLAC__StreamEncoderTellCallback.
	StreamEncoderTellCallback func(enc *StreamEncoder, clientData any) (absoluteByteOffset uint64, status StreamEncoderTellStatus)
	// StreamEncoderReadCallback — port of FLAC__StreamEncoderReadCallback
	// (Ogg only).
	StreamEncoderReadCallback func(enc *StreamEncoder, buffer []byte, clientData any) (n uint32, status StreamEncoderReadStatus)
	// StreamEncoderMetadataCallback — port of FLAC__StreamEncoderMetadataCallback.
	StreamEncoderMetadataCallback func(enc *StreamEncoder, metadata *StreamMetadata, clientData any)
	// StreamEncoderProgressCallback — port of FLAC__StreamEncoderProgressCallback.
	StreamEncoderProgressCallback func(enc *StreamEncoder, bytesWritten, samplesWritten uint64, framesWritten, totalFramesEstimate uint32, clientData any)
)

// ApodizationFunction — port of FLAC__ApodizationFunction
// (protected/stream_encoder.h:47). Ordinals match the C enum.
type ApodizationFunction uint8

const (
	ApodizationBartlett ApodizationFunction = iota
	ApodizationBartlettHann
	ApodizationBlackman
	ApodizationBlackmanHarris4Term92dBSidelobe
	ApodizationConnes
	ApodizationFlattop
	ApodizationGauss
	ApodizationHamming
	ApodizationHann
	ApodizationKaiserBessel
	ApodizationNuttall
	ApodizationRectangle
	ApodizationTriangle
	ApodizationTukey
	ApodizationPartialTukey
	ApodizationPunchoutTukey
	ApodizationSubdivideTukey
	ApodizationWelch
)

// ApodizationSpecification — port of FLAC__ApodizationSpecification
// (protected/stream_encoder.h:68). The C union of per-type parameters
// flattens into named fields here; consult Type for which apply:
//   - Gauss.StdDev for GAUSS
//   - Tukey.P for TUKEY / SUBDIVIDE_TUKEY (the latter also uses
//     SubdivideTukey)
//   - MultipleTukey.{P,Start,End} for PARTIAL_TUKEY / PUNCHOUT_TUKEY
//   - SubdivideTukey.{P,Parts} for SUBDIVIDE_TUKEY
type ApodizationSpecification struct {
	Type ApodizationFunction

	Gauss struct {
		StdDev float32
	}
	Tukey struct {
		P float32
	}
	MultipleTukey struct {
		P     float32
		Start float32
		End   float32
	}
	SubdivideTukey struct {
		P     float32
		Parts int32
	}
}

// compressionLevel — one row of the CompressionLevels table
// (stream_encoder.c:119). Owned by family A; family D's
// set_compression_level copies a row into the protected fields.
type compressionLevel struct {
	doMidSideStereo           bool
	looseMidSideStereo        bool
	maxLPCOrder               uint32
	qlpCoeffPrecision         uint32
	doQLPCoeffPrecSearch      bool
	doEscapeCoding            bool
	doExhaustiveModelSearch   bool
	minResidualPartitionOrder uint32
	maxResidualPartitionOrder uint32
	riceParameterSearchDist   uint32
	apodization               string
}

// compressionLevels — port of compression_levels_[] (stream_encoder.c:131).
// The locale-independent "5e-1" / "tukey(5e-1)" apodization strings are
// preserved verbatim so set_apodization (family D) parses them identically.
var compressionLevels = [...]compressionLevel{
	{false, false, 0, 0, false, false, false, 0, 3, 0, "tukey(5e-1)"},
	{true, true, 0, 0, false, false, false, 0, 3, 0, "tukey(5e-1)"},
	{true, false, 0, 0, false, false, false, 0, 3, 0, "tukey(5e-1)"},
	{false, false, 6, 0, false, false, false, 0, 4, 0, "tukey(5e-1)"},
	{true, true, 8, 0, false, false, false, 0, 4, 0, "tukey(5e-1)"},
	{true, false, 8, 0, false, false, false, 0, 5, 0, "tukey(5e-1)"},
	{true, false, 8, 0, false, false, false, 0, 6, 0, "subdivide_tukey(2)"},
	{true, false, 12, 0, false, false, false, 0, 6, 0, "subdivide_tukey(2)"},
	{true, false, 12, 0, false, false, false, 0, 6, 0, "subdivide_tukey(3)"},
}

// streamEncoderProtected — port of FLAC__StreamEncoderProtected
// (protected/stream_encoder.h:91). Holds the caller-facing settings + the
// resolved stream offsets. Field names mirror the C struct so the family
// B/C/D ports can be cross-referenced by line.
type streamEncoderProtected struct {
	state              StreamEncoderState
	verify             bool
	streamableSubset   bool
	doMD5              bool
	doMidSideStereo    bool
	looseMidSideStereo bool
	channels           uint32
	bitsPerSample      uint32
	sampleRate         uint32
	blocksize          uint32

	numApodizations uint32
	apodizations    [MaxApodizationFunctions]ApodizationSpecification

	maxLPCOrder               uint32
	qlpCoeffPrecision         uint32
	doQLPCoeffPrecSearch      bool
	doExhaustiveModelSearch   bool
	doEscapeCoding            bool
	minResidualPartitionOrder uint32
	maxResidualPartitionOrder uint32
	riceParameterSearchDist   uint32
	totalSamplesEstimate      uint64
	limitMinBitrate           bool

	metadata          []*StreamMetadata
	numMetadataBlocks uint32
	numThreads        uint32

	streaminfoOffset uint64
	seektableOffset  uint64
	audioOffset      uint64
}

// verifyInputFifo — port of verify_input_fifo (stream_encoder.c:92). Holds
// the original signal so the verify decoder's output can be compared
// sample-by-sample (verify_write_callback_, family D).
type verifyInputFifo struct {
	data [MaxChannels][]int32
	size uint32
	tail uint32
}

// verifyOutput — port of verify_output (stream_encoder.c:98). A sliding
// view over the encoded bytes the verify decoder reads back. data is
// re-sliced forward as bytes are consumed (verify_read_callback_, family D).
type verifyOutput struct {
	data     []byte
	capacity uint32
	bytes    uint32
}

// verifyErrorStats — port of the anonymous verify.error_stats struct
// (stream_encoder.c:476).
type verifyErrorStats struct {
	absoluteSample uint64
	frameNumber    uint32
	channel        uint32
	sample         uint32
	expected       int32
	got            int32
}

// encoderVerify — port of the anonymous verify struct
// (stream_encoder.c:470). The verify decoder reuses the native Decoder.
type encoderVerify struct {
	decoder        *Decoder
	stateHint      EncoderStateHint
	needsMagicHack bool
	inputFifo      verifyInputFifo
	output         verifyOutput
	errorStats     verifyErrorStats
}

// StreamEncoderThreadTask — port of FLAC__StreamEncoderThreadTask
// (stream_encoder.c:151), single-thread variant (threadtask[0] only). The
// alignment-padding "_unaligned" pointer pairs collapse to plain slices in
// Go; integerSignal[ch] keeps libFLAC's leading-4-zeroes guard implicitly
// (the slices are allocated with that guard in resize_buffers_).
type StreamEncoderThreadTask struct {
	// integerSignal[ch] points at the first usable sample; the 4-zero
	// alignment guard libFLAC places at negative indices is held in
	// integerSignalBacking[ch][0:4] (resize_buffers_).
	integerSignal          [MaxChannels][]int32
	integerSignalMidSide   [2][]int32
	integerSignal33bitSide []int64
	windowedSignal         []float32

	subframeBps        [MaxChannels]uint32
	subframeBpsMidSide [2]uint32

	residualWorkspace        [MaxChannels][2][]int32
	residualWorkspaceMidSide [2][2][]int32

	subframeWorkspace           [MaxChannels][2]Subframe
	subframeWorkspaceMidSide    [2][2]Subframe
	subframeWorkspacePtr        [MaxChannels][2]*Subframe
	subframeWorkspacePtrMidSide [2][2]*Subframe

	partitionedRiceContentsWorkspace           [MaxChannels][2]PartitionedRiceContents
	partitionedRiceContentsWorkspaceMidSide    [MaxChannels][2]PartitionedRiceContents
	partitionedRiceContentsWorkspacePtr        [MaxChannels][2]*PartitionedRiceContents
	partitionedRiceContentsWorkspacePtrMidSide [MaxChannels][2]*PartitionedRiceContents

	bestSubframe            [MaxChannels]uint32
	bestSubframeMidSide     [2]uint32
	bestSubframeBits        [MaxChannels]uint32
	bestSubframeBitsMidSide [2]uint32

	absResidualPartitionSums []uint64
	rawBitsPerPartition      []uint32

	frame              *BitWriter
	currentFrameNumber uint32

	// lpCoeff is the per-order LP coefficient scratch moved onto the task
	// to save stack in process_subframe_ (stream_encoder.c:194).
	lpCoeff [MaxLPCOrder][MaxLPCOrder]float32

	// partitionedRiceContentsExtra is the find_best_partition_order_
	// ping-pong scratch (stream_encoder.c:196).
	partitionedRiceContentsExtra [2]PartitionedRiceContents

	disableConstantSubframes bool

	// integerSignalBacking / midSideBacking / signal33Backing hold the
	// full allocations (including libFLAC's leading 4-zero guard) so the
	// guarded views above stay alive and the guard bytes are addressable
	// at negative indices via the backing slice.
	integerSignalBacking [MaxChannels][]int32
	midSideBacking       [2][]int32
}

// streamEncoderPrivate — port of FLAC__StreamEncoderPrivate
// (stream_encoder.c:411), single-thread variant. The cpuinfo + local_*
// function-pointer fields collapse to direct calls and are omitted; the
// disable_* SIMD bools are kept so FLAC__stream_encoder_disable_instruction_set
// parity holds (they have no effect on the pure-Go path).
type streamEncoderPrivate struct {
	threadtask [1]*StreamEncoderThreadTask

	inputCapacity uint32

	// window[i] is the precomputed apodization window for apodization i
	// (stream_encoder.c:418). Allocated/filled in resize_buffers_.
	window [MaxApodizationFunctions][]float32

	streaminfo StreamMetadata // STREAMINFO scratchpad
	seekTable  *SeekTable     // points into protected.metadata's seek table

	currentSampleNumber uint32
	currentFrameNumber  uint32
	md5context          MD5Context

	disableMMX   bool
	disableSSE2  bool
	disableSSSE3 bool
	disableSSE41 bool
	disableSSE42 bool
	disableAVX2  bool
	disableFMA   bool

	disableConstantSubframes bool
	disableFixedSubframes    bool
	disableVerbatimSubframes bool

	isOgg bool

	readCallback     StreamEncoderReadCallback
	writeCallback    StreamEncoderWriteCallback
	seekCallback     StreamEncoderSeekCallback
	tellCallback     StreamEncoderTellCallback
	metadataCallback StreamEncoderMetadataCallback
	progressCallback StreamEncoderProgressCallback
	clientData       any

	firstSeekpointToCheck uint32
	file                  *fileSink
	bytesWritten          uint64
	samplesWritten        uint64
	framesWritten         uint32
	totalFramesEstimate   uint32

	verify encoderVerify

	isBeingDeleted bool
	numThreadtasks uint32
}

// fileSink is the os.File-backed write target the init_file path uses
// (stream_encoder.c file callbacks). Kept abstract so family D can wire
// either an *os.File or an in-memory sink; the file callbacks here operate
// through it. pos tracks the byte offset for the tell callback.
type fileSink struct {
	w   io.WriteSeeker
	pos uint64
}

// Close releases the underlying sink when it is an io.Closer, mirroring the
// fclose libFLAC performs in FLAC__stream_encoder_finish for the file path.
func (f *fileSink) Close() error {
	if c, ok := f.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// StreamEncoder — port of FLAC__StreamEncoder (private/stream_encoder.h:
// the { protected_, private_ } pair). The split is preserved so each
// ported func can cite the C field's struct of origin.
type StreamEncoder struct {
	prot *streamEncoderProtected
	priv *streamEncoderPrivate
}

// NewStreamEncoder — port of FLAC__stream_encoder_new (stream_encoder.c:583).
// Allocates the encoder, its protected/private halves, threadtask[0] with a
// fresh BitWriter, wires the subframe / rice-contents workspace pointers,
// inits the partitioned-rice contents, applies set_defaults_, and lands the
// encoder in the UNINITIALIZED state.
func NewStreamEncoder() *StreamEncoder {
	enc := &StreamEncoder{
		prot: new(streamEncoderProtected),
		priv: new(streamEncoderPrivate),
	}
	tt := new(StreamEncoderThreadTask)
	tt.frame = NewBitWriter()
	enc.priv.threadtask[0] = tt
	enc.priv.file = nil

	enc.prot.state = StreamEncoderUninitialized

	setDefaults(enc)

	enc.priv.isBeingDeleted = false

	// Wire workspace pointer arrays (stream_encoder.c:633-648).
	for i := uint32(0); i < MaxChannels; i++ {
		tt.subframeWorkspacePtr[i][0] = &tt.subframeWorkspace[i][0]
		tt.subframeWorkspacePtr[i][1] = &tt.subframeWorkspace[i][1]
	}
	for i := 0; i < 2; i++ {
		tt.subframeWorkspacePtrMidSide[i][0] = &tt.subframeWorkspaceMidSide[i][0]
		tt.subframeWorkspacePtrMidSide[i][1] = &tt.subframeWorkspaceMidSide[i][1]
	}
	for i := uint32(0); i < MaxChannels; i++ {
		tt.partitionedRiceContentsWorkspacePtr[i][0] = &tt.partitionedRiceContentsWorkspace[i][0]
		tt.partitionedRiceContentsWorkspacePtr[i][1] = &tt.partitionedRiceContentsWorkspace[i][1]
	}
	for i := 0; i < 2; i++ {
		tt.partitionedRiceContentsWorkspacePtrMidSide[i][0] = &tt.partitionedRiceContentsWorkspaceMidSide[i][0]
		tt.partitionedRiceContentsWorkspacePtrMidSide[i][1] = &tt.partitionedRiceContentsWorkspaceMidSide[i][1]
	}

	// init the rice-contents (stream_encoder.c:650-659).
	for i := uint32(0); i < MaxChannels; i++ {
		PartitionedRiceContentsInit(&tt.partitionedRiceContentsWorkspace[i][0])
		PartitionedRiceContentsInit(&tt.partitionedRiceContentsWorkspace[i][1])
	}
	for i := 0; i < 2; i++ {
		PartitionedRiceContentsInit(&tt.partitionedRiceContentsWorkspaceMidSide[i][0])
		PartitionedRiceContentsInit(&tt.partitionedRiceContentsWorkspaceMidSide[i][1])
	}
	for i := 0; i < 2; i++ {
		PartitionedRiceContentsInit(&tt.partitionedRiceContentsExtra[i])
	}

	return enc
}

// Delete — port of FLAC__stream_encoder_delete (stream_encoder.c:664).
// Marks the encoder being-deleted (so finish does not call back), finishes
// it, deletes the verify decoder, and clears the rice contents. In Go the
// GC reclaims the heap; this mirrors the C order so any finish-time
// side-effects (md5 finalize, metadata rewrite) still run.
func (enc *StreamEncoder) Delete() {
	if enc == nil {
		return
	}
	enc.priv.isBeingDeleted = true

	enc.Finish()

	if enc.priv.verify.decoder != nil {
		enc.priv.verify.decoder = nil
	}

	tt := enc.priv.threadtask[0]
	for i := uint32(0); i < MaxChannels; i++ {
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspace[i][0])
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspace[i][1])
	}
	for i := 0; i < 2; i++ {
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspaceMidSide[i][0])
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspaceMidSide[i][1])
	}
	for i := 0; i < 2; i++ {
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsExtra[i])
	}
	tt.frame = nil
}

// setDefaults — port of set_defaults_ (stream_encoder.c:2616). Establishes
// the same default protected/private settings as a fresh libFLAC encoder,
// then applies compression level 5.
func setDefaults(enc *StreamEncoder) {
	p := enc.prot
	pr := enc.priv

	p.verify = false // FLAC__MANDATORY_VERIFY_WHILE_ENCODING not defined
	p.streamableSubset = true
	p.doMD5 = true
	p.doMidSideStereo = false
	p.looseMidSideStereo = false
	p.channels = 2
	p.bitsPerSample = 16
	p.sampleRate = 44100
	p.blocksize = 0

	p.numApodizations = 1
	p.apodizations[0] = ApodizationSpecification{}
	p.apodizations[0].Type = ApodizationTukey
	p.apodizations[0].Tukey.P = 0.5

	p.maxLPCOrder = 0
	p.qlpCoeffPrecision = 0
	p.doQLPCoeffPrecSearch = false
	p.doExhaustiveModelSearch = false
	p.doEscapeCoding = false
	p.minResidualPartitionOrder = 0
	p.maxResidualPartitionOrder = 0
	p.riceParameterSearchDist = 0
	p.totalSamplesEstimate = 0
	p.limitMinBitrate = false
	p.metadata = nil
	p.numMetadataBlocks = 0
	p.numThreads = 1

	pr.seekTable = nil
	pr.disableMMX = false
	pr.disableSSE2 = false
	pr.disableSSSE3 = false
	pr.disableSSE41 = false
	pr.disableSSE42 = false
	pr.disableAVX2 = false
	pr.disableConstantSubframes = false
	pr.disableFixedSubframes = false
	pr.disableVerbatimSubframes = false
	pr.isOgg = false
	pr.readCallback = nil
	pr.writeCallback = nil
	pr.seekCallback = nil
	pr.tellCallback = nil
	pr.metadataCallback = nil
	pr.clientData = nil
	pr.progressCallback = nil
	pr.numThreadtasks = 1

	enc.SetCompressionLevel(5)
}

// free_ — port of free_ (stream_encoder.c:2690). Releases the per-stream
// buffers and clears the rice contents. In Go the GC reclaims the slices;
// we drop the references and clear the rice-contents capacity (matching
// the C clear, which re-inits the contents) so a subsequent init grows
// fresh, and free the verify FIFO references.
func (enc *StreamEncoder) free() {
	p := enc.prot
	pr := enc.priv

	if p.metadata != nil {
		p.metadata = nil
		p.numMetadataBlocks = 0
	}

	for i := uint32(0); i < p.numApodizations; i++ {
		pr.window[i] = nil
	}

	tt := pr.threadtask[0]
	for i := uint32(0); i < p.channels; i++ {
		tt.integerSignal[i] = nil
		tt.integerSignalBacking[i] = nil
	}
	for i := 0; i < 2; i++ {
		tt.integerSignalMidSide[i] = nil
		tt.midSideBacking[i] = nil
	}
	tt.integerSignal33bitSide = nil
	tt.windowedSignal = nil
	for ch := uint32(0); ch < p.channels; ch++ {
		for i := 0; i < 2; i++ {
			tt.residualWorkspace[ch][i] = nil
		}
	}
	for ch := 0; ch < 2; ch++ {
		for i := 0; i < 2; i++ {
			tt.residualWorkspaceMidSide[ch][i] = nil
		}
	}
	tt.absResidualPartitionSums = nil
	tt.rawBitsPerPartition = nil

	for i := uint32(0); i < MaxChannels; i++ {
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspace[i][0])
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspace[i][1])
	}
	for i := 0; i < 2; i++ {
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspaceMidSide[i][0])
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsWorkspaceMidSide[i][1])
	}
	for i := 0; i < 2; i++ {
		PartitionedRiceContentsClear(&tt.partitionedRiceContentsExtra[i])
	}

	if p.verify {
		for i := uint32(0); i < p.channels; i++ {
			pr.verify.inputFifo.data[i] = nil
		}
	}

	pr.inputCapacity = 0
}

// resizeBuffers — port of resize_buffers_ (stream_encoder.c:2808),
// single-thread variant. Grows (never shrinks) the integer-signal,
// residual, abs-residual-partition-sum, raw-bits, windowed-signal, and
// per-channel rice-contents workspaces so they fit newBlocksize, then
// recomputes the apodization windows for the (possibly new) blocksize.
//
// PARITY EXTENTS (load-bearing — downstream B/C indexing depends on them):
//   - integer signal arrays get newBlocksize+4+OVERREAD_ entries with the
//     first 4 zeroed as the negative-index alignment guard; integerSignal[i]
//     is the sub-slice starting at index 4.
//   - integer_signal_33bit_side gets the same +4+OVERREAD_ extent.
//   - windowed_signal gets exactly newBlocksize entries (no guard).
//   - residual workspaces get exactly newBlocksize entries.
//   - abs_residual_partition_sums / raw_bits_per_partition get
//     newBlocksize*2 entries (the 1+1/2+1/4+... tree-occupancy bound).
//
// Returns false (and sets state to MEMORY_ALLOCATION_ERROR) on the
// allocation paths libFLAC guards; in Go make() does not fail, so the
// false path is unreachable, but the signature and state semantics match.
func (enc *StreamEncoder) resizeBuffers(newBlocksize uint32) bool {
	p := enc.prot
	pr := enc.priv
	tt := pr.threadtask[0]

	// FLAC__ASSERT(new_blocksize > 0)
	// FLAC__ASSERT(state == OK)

	if newBlocksize > pr.inputCapacity {
		if p.maxLPCOrder > 0 {
			for i := uint32(0); i < p.numApodizations; i++ {
				pr.window[i] = make([]float32, newBlocksize)
			}
		}

		for i := uint32(0); i < p.channels; i++ {
			backing := make([]int32, newBlocksize+4+overread)
			tt.integerSignalBacking[i] = backing
			tt.integerSignal[i] = backing[4:]
		}
		for i := 0; i < 2; i++ {
			backing := make([]int32, newBlocksize+4+overread)
			tt.midSideBacking[i] = backing
			tt.integerSignalMidSide[i] = backing[4:]
		}
		tt.integerSignal33bitSide = make([]int64, newBlocksize+4+overread)

		if p.maxLPCOrder > 0 {
			tt.windowedSignal = make([]float32, newBlocksize)
		}

		for ch := uint32(0); ch < p.channels; ch++ {
			for i := 0; i < 2; i++ {
				tt.residualWorkspace[ch][i] = make([]int32, newBlocksize)
			}
		}
		for ch := uint32(0); ch < p.channels; ch++ {
			for i := 0; i < 2; i++ {
				PartitionedRiceContentsEnsureSize(&tt.partitionedRiceContentsWorkspace[ch][i], p.maxResidualPartitionOrder)
			}
		}
		for ch := 0; ch < 2; ch++ {
			for i := 0; i < 2; i++ {
				tt.residualWorkspaceMidSide[ch][i] = make([]int32, newBlocksize)
			}
		}
		for ch := 0; ch < 2; ch++ {
			for i := 0; i < 2; i++ {
				PartitionedRiceContentsEnsureSize(&tt.partitionedRiceContentsWorkspaceMidSide[ch][i], p.maxResidualPartitionOrder)
			}
		}
		for i := 0; i < 2; i++ {
			PartitionedRiceContentsEnsureSize(&tt.partitionedRiceContentsExtra[i], p.maxResidualPartitionOrder)
		}

		tt.absResidualPartitionSums = make([]uint64, newBlocksize*2)
		if p.doEscapeCoding {
			tt.rawBitsPerPartition = make([]uint32, newBlocksize*2)
		}
	}

	pr.inputCapacity = newBlocksize

	// Adjust the windows now that the blocksize is known.
	if p.maxLPCOrder > 0 && newBlocksize > 1 {
		for i := uint32(0); i < p.numApodizations; i++ {
			ap := &p.apodizations[i]
			win := pr.window[i]
			L := int32(newBlocksize)
			switch ap.Type {
			case ApodizationBartlett:
				WindowBartlettFn(win, L)
			case ApodizationBartlettHann:
				WindowBartlettHannFn(win, L)
			case ApodizationBlackman:
				WindowBlackmanFn(win, L)
			case ApodizationBlackmanHarris4Term92dBSidelobe:
				WindowBlackmanHarris4Term92dBSidelobeFn(win, L)
			case ApodizationConnes:
				WindowConnesFn(win, L)
			case ApodizationFlattop:
				WindowFlattopFn(win, L)
			case ApodizationGauss:
				WindowGaussFn(win, L, ap.Gauss.StdDev)
			case ApodizationHamming:
				WindowHammingFn(win, L)
			case ApodizationHann:
				WindowHannFn(win, L)
			case ApodizationKaiserBessel:
				WindowKaiserBesselFn(win, L)
			case ApodizationNuttall:
				WindowNuttallFn(win, L)
			case ApodizationRectangle:
				WindowRectangleFn(win, L)
			case ApodizationTriangle:
				WindowTriangleFn(win, L)
			case ApodizationTukey:
				WindowTukeyFn(win, L, ap.Tukey.P)
			case ApodizationPartialTukey:
				WindowPartialTukeyFn(win, L, ap.MultipleTukey.P, ap.MultipleTukey.Start, ap.MultipleTukey.End)
			case ApodizationPunchoutTukey:
				WindowPunchoutTukeyFn(win, L, ap.MultipleTukey.P, ap.MultipleTukey.Start, ap.MultipleTukey.End)
			case ApodizationSubdivideTukey:
				// libFLAC builds the root tukey window here; the
				// subdivision happens in apply_apodization_ (family B).
				// stream_encoder.c:2953 reads parameters.tukey.p, which in
				// the C union aliases the same storage the apodization parser
				// wrote as parameters.subdivide_tukey.p (== P/parts). Go has
				// no union, so read the field the parser actually set:
				// SubdivideTukey.P (Tukey.P is left zero, which would build a
				// rectangular window and diverge from libFLAC).
				WindowTukeyFn(win, L, ap.SubdivideTukey.P)
			case ApodizationWelch:
				WindowWelchFn(win, L)
			default:
				// FLAC__ASSERT(0); double protection.
				WindowHannFn(win, L)
			}
		}
	}
	// The lag>data_len autocorrelation-routine guard collapses (pure-Go
	// path always uses the canonical LPCComputeAutocorrelation).

	return true
}

// PartitionedRiceContentsInit — port of
// FLAC__format_entropy_coding_method_partitioned_rice_contents_init
// (format.c:565). Resets the contents to empty. Owned by family A because
// the encoder (resize_buffers_ / find_best_partition_order_) is the first
// consumer of these lifecycle helpers.
func PartitionedRiceContentsInit(o *PartitionedRiceContents) {
	o.Parameters = nil
	o.RawBits = nil
	o.CapacityByOrder = 0
}

// PartitionedRiceContentsClear — port of
// FLAC__format_entropy_coding_method_partitioned_rice_contents_clear
// (format.c:574). Frees the parameter / raw-bit slices and re-inits.
func PartitionedRiceContentsClear(o *PartitionedRiceContents) {
	o.Parameters = nil
	o.RawBits = nil
	PartitionedRiceContentsInit(o)
}

// PartitionedRiceContentsEnsureSize — port of
// FLAC__format_entropy_coding_method_partitioned_rice_contents_ensure_size
// (format.c:590). Grows the parameter / raw-bit slices to hold
// 1<<maxPartitionOrder entries when the current capacity is too small or
// either slice is nil; raw_bits is zeroed on (re)allocation. Returns false
// only on the alloc paths libFLAC guards (unreachable in Go).
func PartitionedRiceContentsEnsureSize(o *PartitionedRiceContents, maxPartitionOrder uint32) bool {
	if o.CapacityByOrder < maxPartitionOrder || o.Parameters == nil || o.RawBits == nil {
		n := uint32(1) << maxPartitionOrder
		params := make([]uint32, n)
		copy(params, o.Parameters)
		o.Parameters = params
		// raw_bits is freshly zeroed (memset) on every (re)allocation.
		o.RawBits = make([]uint32, n)
		o.CapacityByOrder = maxPartitionOrder
	}
	return true
}

// initStreamInternal — port of init_stream_internal_ (stream_encoder.c:707),
// single-thread, non-Ogg variant. Validates the protected settings,
// clamps streamable-subset constraints, resolves defaults (blocksize,
// qlp_coeff_precision, residual partition orders), validates metadata,
// resizes the buffers, sets up the verify decoder when requested, and
// writes the fLaC magic + STREAMINFO + (synthetic) VORBIS_COMMENT + user
// metadata blocks.
//
// The metadata-write tail delegates byte emission to family C's
// write_bitbuffer_ and the encode_framing.go AddMetadataBlock; the
// per-block write_bitbuffer_ + tell_callback offset capture is performed by
// C. This function performs the validation/setup and drives that tail in
// the same order as libFLAC so the resolved offsets match.
//
// writeCallback / seekCallback / tellCallback / metadataCallback are the
// client callbacks; isOgg selects the Ogg container (unsupported here —
// returns UNSUPPORTED_CONTAINER for parity with FLAC__HAS_OGG == 0).
func (enc *StreamEncoder) initStreamInternal(
	readCallback StreamEncoderReadCallback,
	writeCallback StreamEncoderWriteCallback,
	seekCallback StreamEncoderSeekCallback,
	tellCallback StreamEncoderTellCallback,
	metadataCallback StreamEncoderMetadataCallback,
	clientData any,
	isOgg bool,
) StreamEncoderInitStatus {
	p := enc.prot
	pr := enc.priv

	if p.state != StreamEncoderUninitialized {
		return StreamEncoderInitStatusAlreadyInitialized
	}

	// FLAC__HAS_OGG == 0 in the vendored single build.
	if isOgg {
		return StreamEncoderInitStatusUnsupportedContainer
	}

	if writeCallback == nil || (seekCallback != nil && tellCallback == nil) {
		return StreamEncoderInitStatusInvalidCallbacks
	}

	if p.channels == 0 || p.channels > MaxChannels {
		return StreamEncoderInitStatusInvalidNumberOfChannels
	}

	if p.channels != 2 {
		p.doMidSideStereo = false
		p.looseMidSideStereo = false
	} else if !p.doMidSideStereo {
		p.looseMidSideStereo = false
	}

	if p.bitsPerSample < MinBitsPerSample || p.bitsPerSample > MaxBitsPerSample {
		return StreamEncoderInitStatusInvalidBitsPerSample
	}

	if !FormatSampleRateIsValid(p.sampleRate) {
		return StreamEncoderInitStatusInvalidSampleRate
	}

	if p.blocksize == 0 {
		if p.maxLPCOrder == 0 {
			p.blocksize = 1152
		} else {
			p.blocksize = 4096
		}
	}

	if p.blocksize < MinBlockSize || p.blocksize > MaxBlockSize {
		return StreamEncoderInitStatusInvalidBlockSize
	}

	if p.maxLPCOrder > MaxLPCOrder {
		return StreamEncoderInitStatusInvalidMaxLPCOrder
	}

	if p.blocksize < p.maxLPCOrder {
		return StreamEncoderInitStatusBlockSizeTooSmallForLPCOrder
	}

	if p.qlpCoeffPrecision == 0 {
		switch {
		case p.bitsPerSample < 16:
			// @@@ guess (stream_encoder.c:768)
			v := uint32(2 + p.bitsPerSample/2)
			if v < MinQLPCoeffPrecision {
				v = MinQLPCoeffPrecision
			}
			p.qlpCoeffPrecision = v
		case p.bitsPerSample == 16:
			switch {
			case p.blocksize <= 192:
				p.qlpCoeffPrecision = 7
			case p.blocksize <= 384:
				p.qlpCoeffPrecision = 8
			case p.blocksize <= 576:
				p.qlpCoeffPrecision = 9
			case p.blocksize <= 1152:
				p.qlpCoeffPrecision = 10
			case p.blocksize <= 2304:
				p.qlpCoeffPrecision = 11
			case p.blocksize <= 4608:
				p.qlpCoeffPrecision = 12
			default:
				p.qlpCoeffPrecision = 13
			}
		default:
			switch {
			case p.blocksize <= 384:
				p.qlpCoeffPrecision = MaxQLPCoeffPrecision - 2
			case p.blocksize <= 1152:
				p.qlpCoeffPrecision = MaxQLPCoeffPrecision - 1
			default:
				p.qlpCoeffPrecision = MaxQLPCoeffPrecision
			}
		}
	} else if p.qlpCoeffPrecision < MinQLPCoeffPrecision || p.qlpCoeffPrecision > MaxQLPCoeffPrecision {
		return StreamEncoderInitStatusInvalidQLPCoeffPrecision
	}

	if p.streamableSubset {
		if !FormatBlocksizeIsSubset(p.blocksize, p.sampleRate) {
			return StreamEncoderInitStatusNotStreamable
		}
		if !FormatSampleRateIsSubset(p.sampleRate) {
			return StreamEncoderInitStatusNotStreamable
		}
		switch p.bitsPerSample {
		case 8, 12, 16, 20, 24, 32:
		default:
			return StreamEncoderInitStatusNotStreamable
		}
		if p.maxResidualPartitionOrder > SubsetMaxRicePartitionOrder {
			return StreamEncoderInitStatusNotStreamable
		}
		if p.sampleRate <= 48000 &&
			(p.blocksize > SubsetMaxBlockSize48000Hz || p.maxLPCOrder > SubsetMaxLPCOrder48000Hz) {
			return StreamEncoderInitStatusNotStreamable
		}
	}

	if p.maxResidualPartitionOrder >= (1 << EntropyCodingMethodPartitionedRiceOrderLen) {
		p.maxResidualPartitionOrder = (1 << EntropyCodingMethodPartitionedRiceOrderLen) - 1
	}
	if p.minResidualPartitionOrder >= p.maxResidualPartitionOrder {
		p.minResidualPartitionOrder = p.maxResidualPartitionOrder
	}

	// keep track of any SEEKTABLE block (stream_encoder.c:858).
	if p.metadata != nil && p.numMetadataBlocks > 0 {
		for i2 := uint32(0); i2 < p.numMetadataBlocks; i2++ {
			if p.metadata[i2] != nil && p.metadata[i2].Type == MetadataTypeSeekTable {
				pr.seekTable = &p.metadata[i2].SeekTable
				break
			}
		}
	}

	// validate metadata (stream_encoder.c:869).
	if p.metadata == nil && p.numMetadataBlocks > 0 {
		return StreamEncoderInitStatusInvalidMetadata
	}
	var metadataHasSeektable, metadataHasVorbisComment bool
	var metadataPictureHasType1, metadataPictureHasType2 bool
	for i := uint32(0); i < p.numMetadataBlocks; i++ {
		m := p.metadata[i]
		switch m.Type {
		case MetadataTypeStreamInfo:
			return StreamEncoderInitStatusInvalidMetadata
		case MetadataTypeSeekTable:
			if metadataHasSeektable {
				return StreamEncoderInitStatusInvalidMetadata
			}
			metadataHasSeektable = true
			if !FormatSeektableIsLegal(&m.SeekTable) {
				return StreamEncoderInitStatusInvalidMetadata
			}
		case MetadataTypeVorbisComment:
			if metadataHasVorbisComment {
				return StreamEncoderInitStatusInvalidMetadata
			}
			metadataHasVorbisComment = true
		case MetadataTypeCuesheet:
			if !FormatCuesheetIsLegal(&m.CueSheet, m.CueSheet.IsCD) {
				return StreamEncoderInitStatusInvalidMetadata
			}
		case MetadataTypePicture:
			if !FormatPictureIsLegal(&m.Picture) {
				return StreamEncoderInitStatusInvalidMetadata
			}
			if PictureType(m.Picture.Type) == pictureTypeFileIconStandard {
				if metadataPictureHasType1 {
					return StreamEncoderInitStatusInvalidMetadata
				}
				metadataPictureHasType1 = true
				// standard icon must be 32x32 PNG ("image/png" or "-->").
				mime := string(m.Picture.MimeType)
				if (mime != "image/png" && mime != "-->") ||
					m.Picture.Width != 32 || m.Picture.Height != 32 {
					return StreamEncoderInitStatusInvalidMetadata
				}
			} else if PictureType(m.Picture.Type) == pictureTypeFileIcon {
				if metadataPictureHasType2 {
					return StreamEncoderInitStatusInvalidMetadata
				}
				metadataPictureHasType2 = true
			}
		}
	}

	pr.inputCapacity = 0
	pr.currentSampleNumber = 0
	pr.currentFrameNumber = 0

	// cpuinfo / local_* function-pointer resolution collapses to direct
	// calls in pure Go (stream_encoder.c:926-1114): the disable_* SIMD
	// bools have no effect on the canonical-func path.

	// from here on, errors are fatal and override the state.
	p.state = StreamEncoderOK

	pr.isOgg = isOgg
	pr.readCallback = readCallback
	pr.writeCallback = writeCallback
	pr.seekCallback = seekCallback
	pr.tellCallback = tellCallback
	pr.metadataCallback = metadataCallback
	pr.clientData = clientData

	// num_threads <= 1: only threadtask[0]; the HAVE_PTHREAD spin-up is
	// omitted.
	pr.numThreadtasks = 1

	for i := uint32(0); i < p.numApodizations; i++ {
		pr.window[i] = nil
	}
	tt := pr.threadtask[0]
	for i := uint32(0); i < p.channels; i++ {
		tt.integerSignal[i] = nil
		tt.integerSignalBacking[i] = nil
	}
	for i := 0; i < 2; i++ {
		tt.integerSignalMidSide[i] = nil
		tt.midSideBacking[i] = nil
	}
	tt.integerSignal33bitSide = nil
	tt.windowedSignal = nil
	for i := uint32(0); i < p.channels; i++ {
		tt.residualWorkspace[i][0] = nil
		tt.residualWorkspace[i][1] = nil
		tt.bestSubframe[i] = 0
	}
	for i := 0; i < 2; i++ {
		tt.residualWorkspaceMidSide[i][0] = nil
		tt.residualWorkspaceMidSide[i][1] = nil
		tt.bestSubframeMidSide[i] = 0
	}
	tt.absResidualPartitionSums = nil
	tt.rawBitsPerPartition = nil

	if !enc.resizeBuffers(p.blocksize) {
		// resizeBuffers set the state on error.
		return StreamEncoderInitStatusEncoderError
	}

	if !tt.frame.Init() {
		p.state = StreamEncoderMemoryAllocationError
		return StreamEncoderInitStatusEncoderError
	}

	// Set up the verify decoder if requested.
	if p.verify {
		pr.verify.inputFifo.size = (p.blocksize + overread) * pr.numThreadtasks
		for i := uint32(0); i < p.channels; i++ {
			pr.verify.inputFifo.data[i] = make([]int32, pr.verify.inputFifo.size)
		}
		pr.verify.inputFifo.tail = 0

		if pr.verify.decoder == nil {
			pr.verify.decoder = NewDecoder()
			if pr.verify.decoder == nil {
				p.state = StreamEncoderVerifyDecoderError
				return StreamEncoderInitStatusEncoderError
			}
		}
		// Wire the verify decoder to the encoder's verify read/write/error
		// callbacks (family D: verifyReadCallback / verifyWriteCallback /
		// verifyErrorCallback). The native Decoder takes an io.Reader for
		// its read side; the encoder's verifyReadCallback has the
		// io.Reader.Read shape, so it is adapted directly. Mirrors the
		// FLAC__stream_decoder_init_stream call at stream_encoder.c:1314.
		if pr.verify.decoder.InitStream(
			encoderVerifyReader{enc},
			enc.verifyWriteCallback,
			enc.verifyErrorCallback,
			false,
		) != DecoderSearchForMetadata {
			p.state = StreamEncoderVerifyDecoderError
			return StreamEncoderInitStatusEncoderError
		}
	}
	pr.verify.errorStats = verifyErrorStats{}

	// These must be set before any metadata is written.
	pr.firstSeekpointToCheck = 0
	pr.samplesWritten = 0
	p.streaminfoOffset = 0
	p.seektableOffset = 0
	p.audioOffset = 0

	// write the stream header (fLaC magic).
	if p.verify {
		pr.verify.stateHint = encoderInMagic
	}
	if !tt.frame.WriteRawUint32(StreamSync, StreamSyncLen) {
		p.state = StreamEncoderFramingError
		return StreamEncoderInitStatusEncoderError
	}
	if !enc.writeBitbuffer_(tt, 0, false) {
		return StreamEncoderInitStatusEncoderError
	}

	// write the STREAMINFO metadata block.
	if p.verify {
		pr.verify.stateHint = encoderInMetadata
	}
	si := &pr.streaminfo
	si.Type = MetadataTypeStreamInfo
	si.IsLast = false // at minimum a VORBIS_COMMENT follows
	si.Length = StreamMetadataStreamInfoLength
	si.StreamInfo.MinBlockSize = p.blocksize // same blocksize for whole stream
	si.StreamInfo.MaxBlockSize = p.blocksize
	si.StreamInfo.MinFrameSize = 0 // filled later
	si.StreamInfo.MaxFrameSize = 0 // filled later
	si.StreamInfo.SampleRate = p.sampleRate
	si.StreamInfo.Channels = p.channels
	si.StreamInfo.BitsPerSample = p.bitsPerSample
	si.StreamInfo.TotalSamples = p.totalSamplesEstimate // replaced later
	si.StreamInfo.MD5Sum = [16]byte{}
	if p.doMD5 {
		pr.md5context.Init()
	}
	if !AddMetadataBlock(si, tt.frame, true) {
		p.state = StreamEncoderFramingError
		return StreamEncoderInitStatusEncoderError
	}
	if !enc.writeBitbuffer_(tt, 0, false) {
		return StreamEncoderInitStatusEncoderError
	}

	// Now that STREAMINFO is written, set min_framesize to the max and
	// clear total_samples (stream_encoder.c:1382).
	si.StreamInfo.MinFrameSize = (1 << StreamInfoMinFrameSizeLen) - 1
	si.StreamInfo.TotalSamples = 0

	// If no VORBIS_COMMENT supplied, write an empty one.
	if !metadataHasVorbisComment {
		var vc StreamMetadata
		vc.Type = MetadataTypeVorbisComment
		vc.IsLast = p.numMetadataBlocks == 0
		vc.Length = 4 + 4 // MAGIC NUMBER
		vc.VorbisComment.VendorString.Length = 0
		vc.VorbisComment.VendorString.Entry = nil
		vc.VorbisComment.NumComments = 0
		vc.VorbisComment.Comments = nil
		if !AddMetadataBlock(&vc, tt.frame, true) {
			p.state = StreamEncoderFramingError
			return StreamEncoderInitStatusEncoderError
		}
		if !enc.writeBitbuffer_(tt, 0, false) {
			return StreamEncoderInitStatusEncoderError
		}
	}

	// write the user's metadata blocks.
	for i := uint32(0); i < p.numMetadataBlocks; i++ {
		p.metadata[i].IsLast = i == p.numMetadataBlocks-1
		if !AddMetadataBlock(p.metadata[i], tt.frame, true) {
			p.state = StreamEncoderFramingError
			return StreamEncoderInitStatusEncoderError
		}
		if !enc.writeBitbuffer_(tt, 0, false) {
			return StreamEncoderInitStatusEncoderError
		}
	}

	// save the stream offset for the start of audio.
	if pr.tellCallback != nil {
		off, st := pr.tellCallback(enc, pr.clientData)
		if st == StreamEncoderTellStatusError {
			p.state = StreamEncoderClientError
			return StreamEncoderInitStatusEncoderError
		}
		if st == StreamEncoderTellStatusOK {
			p.audioOffset = off
		}
	}

	if p.verify {
		pr.verify.stateHint = encoderInAudio
	}

	return StreamEncoderInitStatusOK
}

// disable instruction-set / subframe-type setters
// (stream_encoder.c:2249-2298). The disable_instruction_set bits have no
// effect on the pure-Go path but are stored so the get_state UNINITIALIZED
// gate behaves identically.

// DisableInstructionSet — port of
// FLAC__stream_encoder_disable_instruction_set (stream_encoder.c:2249).
func (enc *StreamEncoder) DisableInstructionSet(value int) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	pr := enc.priv
	pr.disableMMX = value&1 != 0
	pr.disableSSE2 = value&2 != 0
	pr.disableSSSE3 = value&4 != 0
	pr.disableSSE41 = value&8 != 0
	pr.disableAVX2 = value&16 != 0
	pr.disableFMA = value&32 != 0
	pr.disableSSE42 = value&64 != 0
	return true
}

// DisableConstantSubframes — port of
// FLAC__stream_encoder_disable_constant_subframes (stream_encoder.c:2266).
func (enc *StreamEncoder) DisableConstantSubframes(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.priv.disableConstantSubframes = value
	return true
}

// DisableFixedSubframes — port of
// FLAC__stream_encoder_disable_fixed_subframes (stream_encoder.c:2277).
func (enc *StreamEncoder) DisableFixedSubframes(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.priv.disableFixedSubframes = value
	return true
}

// DisableVerbatimSubframes — port of
// FLAC__stream_encoder_disable_verbatim_subframes (stream_encoder.c:2288).
func (enc *StreamEncoder) DisableVerbatimSubframes(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.priv.disableVerbatimSubframes = value
	return true
}

// streamDecoderStateString — port of FLAC__StreamDecoderStateString
// (stream_decoder.c:187). Indexed by DecoderState; used by the encoder's
// resolved-state-string accessor when the verify decoder errored.
var streamDecoderStateString = [...]string{
	"FLAC__STREAM_DECODER_SEARCH_FOR_METADATA",
	"FLAC__STREAM_DECODER_READ_METADATA",
	"FLAC__STREAM_DECODER_SEARCH_FOR_FRAME_SYNC",
	"FLAC__STREAM_DECODER_READ_FRAME",
	"FLAC__STREAM_DECODER_END_OF_STREAM",
	"FLAC__STREAM_DECODER_OGG_ERROR",
	"FLAC__STREAM_DECODER_SEEK_ERROR",
	"FLAC__STREAM_DECODER_ABORTED",
	"FLAC__STREAM_DECODER_MEMORY_ALLOCATION_ERROR",
	"FLAC__STREAM_DECODER_UNINITIALIZED",
	"FLAC__STREAM_DECODER_END_OF_LINK",
}

// decoderResolvedStateString — port of
// FLAC__stream_decoder_get_resolved_state_string for the non-Ogg build:
// indexes the decoder state-string table directly.
func decoderResolvedStateString(d *Decoder) string {
	st := d.State()
	if int(st) < len(streamDecoderStateString) {
		return streamDecoderStateString[st]
	}
	return ""
}

// State — port of FLAC__stream_encoder_get_state (stream_encoder.c:2299).
func (enc *StreamEncoder) State() StreamEncoderState { return enc.prot.state }

// VerifyDecoderState — port of
// FLAC__stream_encoder_get_verify_decoder_state (stream_encoder.c:2307).
func (enc *StreamEncoder) VerifyDecoderState() DecoderState {
	if enc.prot.verify {
		if enc.priv.verify.decoder == nil {
			return DecoderMemoryAllocationError
		}
		return enc.priv.verify.decoder.State()
	}
	return DecoderUninitialized
}

// ResolvedStateString — port of
// FLAC__stream_encoder_get_resolved_state_string (stream_encoder.c:2321).
func (enc *StreamEncoder) ResolvedStateString() string {
	if enc.prot.state != StreamEncoderVerifyDecoderError {
		return streamEncoderStateString[enc.prot.state]
	}
	if enc.priv.verify.decoder == nil {
		return streamEncoderStateString[StreamEncoderMemoryAllocationError]
	}
	return decoderResolvedStateString(enc.priv.verify.decoder)
}

// VerifyDecoderErrorStats — port of
// FLAC__stream_encoder_get_verify_decoder_error_stats
// (stream_encoder.c:2334). Returns the captured mismatch location/values.
func (enc *StreamEncoder) VerifyDecoderErrorStats() (absoluteSample uint64, frameNumber, channel, sample uint32, expected, got int32) {
	es := &enc.priv.verify.errorStats
	return es.absoluteSample, es.frameNumber, es.channel, es.sample, es.expected, es.got
}

// get_* accessors (stream_encoder.c:2353-2511). Each returns the
// corresponding protected setting.

// GetVerify — FLAC__stream_encoder_get_verify (stream_encoder.c:2353).
func (enc *StreamEncoder) GetVerify() bool { return enc.prot.verify }

// GetStreamableSubset — stream_encoder.c:2361.
func (enc *StreamEncoder) GetStreamableSubset() bool { return enc.prot.streamableSubset }

// GetDoMD5 — stream_encoder.c:2369.
func (enc *StreamEncoder) GetDoMD5() bool { return enc.prot.doMD5 }

// GetChannels — stream_encoder.c:2377.
func (enc *StreamEncoder) GetChannels() uint32 { return enc.prot.channels }

// GetBitsPerSample — stream_encoder.c:2385.
func (enc *StreamEncoder) GetBitsPerSample() uint32 { return enc.prot.bitsPerSample }

// GetSampleRate — stream_encoder.c:2393.
func (enc *StreamEncoder) GetSampleRate() uint32 { return enc.prot.sampleRate }

// GetBlocksize — stream_encoder.c:2401.
func (enc *StreamEncoder) GetBlocksize() uint32 { return enc.prot.blocksize }

// GetDoMidSideStereo — stream_encoder.c:2409.
func (enc *StreamEncoder) GetDoMidSideStereo() bool { return enc.prot.doMidSideStereo }

// GetLooseMidSideStereo — stream_encoder.c:2417.
func (enc *StreamEncoder) GetLooseMidSideStereo() bool { return enc.prot.looseMidSideStereo }

// GetMaxLPCOrder — stream_encoder.c:2425.
func (enc *StreamEncoder) GetMaxLPCOrder() uint32 { return enc.prot.maxLPCOrder }

// GetQLPCoeffPrecision — stream_encoder.c:2433.
func (enc *StreamEncoder) GetQLPCoeffPrecision() uint32 { return enc.prot.qlpCoeffPrecision }

// GetDoQLPCoeffPrecSearch — stream_encoder.c:2441.
func (enc *StreamEncoder) GetDoQLPCoeffPrecSearch() bool { return enc.prot.doQLPCoeffPrecSearch }

// GetDoEscapeCoding — stream_encoder.c:2449.
func (enc *StreamEncoder) GetDoEscapeCoding() bool { return enc.prot.doEscapeCoding }

// GetDoExhaustiveModelSearch — stream_encoder.c:2457.
func (enc *StreamEncoder) GetDoExhaustiveModelSearch() bool {
	return enc.prot.doExhaustiveModelSearch
}

// GetMinResidualPartitionOrder — stream_encoder.c:2465.
func (enc *StreamEncoder) GetMinResidualPartitionOrder() uint32 {
	return enc.prot.minResidualPartitionOrder
}

// GetNumThreads — stream_encoder.c:2473.
func (enc *StreamEncoder) GetNumThreads() uint32 { return enc.prot.numThreads }

// GetMaxResidualPartitionOrder — stream_encoder.c:2481.
func (enc *StreamEncoder) GetMaxResidualPartitionOrder() uint32 {
	return enc.prot.maxResidualPartitionOrder
}

// GetRiceParameterSearchDist — stream_encoder.c:2489.
func (enc *StreamEncoder) GetRiceParameterSearchDist() uint32 {
	return enc.prot.riceParameterSearchDist
}

// GetTotalSamplesEstimate — stream_encoder.c:2497.
func (enc *StreamEncoder) GetTotalSamplesEstimate() uint64 {
	return enc.prot.totalSamplesEstimate
}

// GetLimitMinBitrate — stream_encoder.c:2505.
func (enc *StreamEncoder) GetLimitMinBitrate() bool { return enc.prot.limitMinBitrate }

// appendToVerifyFifo — port of append_to_verify_fifo_ (stream_encoder.c:5114).
// Copies wideSamples of each channel's planar input (starting at
// inputOffset) onto the verify FIFO tail.
func appendToVerifyFifo(fifo *verifyInputFifo, input [][]int32, inputOffset, channels, wideSamples uint32) {
	for ch := uint32(0); ch < channels; ch++ {
		copy(fifo.data[ch][fifo.tail:fifo.tail+wideSamples], input[ch][inputOffset:inputOffset+wideSamples])
	}
	fifo.tail += wideSamples
	// FLAC__ASSERT(fifo.tail <= fifo.size)
}

// appendToVerifyFifoInterleaved — port of
// append_to_verify_fifo_interleaved_ (stream_encoder.c:5126). De-interleaves
// wideSamples from the channel-interleaved input onto the FIFO tail.
func appendToVerifyFifoInterleaved(fifo *verifyInputFifo, input []int32, inputOffset, channels, wideSamples uint32) {
	tail := fifo.tail
	sample := inputOffset * channels
	for ws := uint32(0); ws < wideSamples; ws++ {
		for ch := uint32(0); ch < channels; ch++ {
			fifo.data[ch][tail] = input[sample]
			sample++
		}
		tail++
	}
	fifo.tail = tail
	// FLAC__ASSERT(fifo.tail <= fifo.size)
}

// encoderVerifyReader adapts the encoder's verifyReadCallback (family D,
// stream_encoder.c:5143) to the io.Reader the native Decoder consumes for
// its read side. The C path wires verify_read_callback_ via
// FLAC__stream_decoder_init_stream; the native Decoder takes an io.Reader,
// so this thin shim forwards Read to the encoder's verifyReadCallback.
// verify_metadata_callback_ (stream_encoder.c:5221) is a no-op in libFLAC
// and the native Decoder has no metadata callback, so it is not wired.
type encoderVerifyReader struct {
	enc *StreamEncoder
}

func (r encoderVerifyReader) Read(buffer []byte) (int, error) {
	return r.enc.verifyReadCallback(buffer)
}

// File-mode I/O callbacks (stream_encoder.c:5233-5315). These back the
// FLAC__stream_encoder_init_file path; they operate through the fileSink
// the init_file wrapper (family D) installs on private_.file.

// fileReadCallback — port of file_read_callback_ (stream_encoder.c:5233).
// Ogg-only in libFLAC; provided for signature parity.
func (enc *StreamEncoder) fileReadCallback(buffer []byte) (uint32, StreamEncoderReadStatus) {
	f := enc.priv.file
	if f == nil {
		return 0, StreamEncoderReadStatusAbort
	}
	rs, ok := f.w.(io.Reader)
	if !ok {
		return 0, StreamEncoderReadStatusUnsupported
	}
	n, err := rs.Read(buffer)
	if n == 0 {
		if err == io.EOF {
			return 0, StreamEncoderReadStatusEndOfStream
		}
		if err != nil {
			return 0, StreamEncoderReadStatusAbort
		}
	}
	return uint32(n), StreamEncoderReadStatusContinue
}

// fileSeekCallback — port of file_seek_callback_ (stream_encoder.c:5247).
func (enc *StreamEncoder) fileSeekCallback(absoluteByteOffset uint64) StreamEncoderSeekStatus {
	f := enc.priv.file
	if _, err := f.w.Seek(int64(absoluteByteOffset), io.SeekStart); err != nil {
		return StreamEncoderSeekStatusError
	}
	f.pos = absoluteByteOffset
	return StreamEncoderSeekStatusOK
}

// fileTellCallback — port of file_tell_callback_ (stream_encoder.c:5257).
func (enc *StreamEncoder) fileTellCallback() (uint64, StreamEncoderTellStatus) {
	f := enc.priv.file
	return f.pos, StreamEncoderTellStatusOK
}

// fileWriteCallback — port of file_write_callback_ (stream_encoder.c:5286)
// (and local__fwrite, stream_encoder.c:5275, which is a plain fwrite in the
// non-valgrind build). Writes the framed bytes to the sink. The progress
// callback is not modelled (CLI-only in libFLAC).
func (enc *StreamEncoder) fileWriteCallback(buffer []byte, samples, currentFrame uint32) StreamEncoderWriteStatus {
	_ = samples
	_ = currentFrame
	f := enc.priv.file
	n, err := f.w.Write(buffer)
	if err != nil || n != len(buffer) {
		return StreamEncoderWriteStatusFatalError
	}
	f.pos += uint64(n)
	return StreamEncoderWriteStatusOK
}
