// This file ports modules/audio_processing/aec3/echo_canceller3.{h,cc}:
// EchoCanceller3, the top-level AEC3 driver. It turns arbitrary
// 10ms-multiple full-band audio frames into the 4ms blocks
// BlockProcessor (block_processor.go) operates on, using
// FrameBlocker/BlockFramer (block.go) to bridge the frame and block
// domains, and AudioBuffer (audio_buffer.go) to bridge the full-band
// and frequency-band domains.
//
// Authorized drops (numbered per this port's Phase 6 spec):
//
//	(1) AdjustConfig and every field_trial::IsEnabled/FindFullName call
//	    in the real constructor: config is used verbatim, exactly as
//	    supplied by the caller. There is no field-trial mechanism in
//	    this port.
//	(2) ConfigSelector, MultiChannelContentDetector, and the runtime
//	    stereo-content-detection/Initialize()-triggered reconfiguration
//	    they drive: render/capture channel counts are fixed for the
//	    lifetime of an EchoCanceller3 and never change (see
//	    NewEchoCanceller3), so num_render_channels_to_aec_ is always
//	    equal to numRenderChannels, Initialize() effectively runs
//	    exactly once (inline in NewEchoCanceller3), and the downmix
//	    path in FillSubFrameView/BufferRenderFrameContent
//	    (proper_downmix_needed) is unreachable and omitted.
//	(3) The SwapQueue-based RenderWriter/render_transfer_queue_: this
//	    port replaces it with a plain, unsynchronized FIFO of split-band
//	    render frames (the renderQueue field), under an explicit
//	    single-goroutine-caller concurrency contract -- AnalyzeRender,
//	    AnalyzeCapture, and ProcessCapture/ProcessCaptureWithLinearOutput
//	    must all be called from one goroutine, serialized by the
//	    caller. (The real class instead permits AnalyzeRender to run
//	    concurrently with the other methods, via a lock-free SwapQueue;
//	    this port deliberately narrows that to full external
//	    serialization, matching the Phase 6 spec's stricter contract for
//	    the public aec API.) On overflow (more than
//	    RenderTransferQueueSizeFrames frames buffered), new frames are
//	    silently dropped -- counted via renderFramesDropped/
//	    RenderFramesDropped -- matching SwapQueue::Insert's fail-
//	    silently-on-overflow behavior (RenderWriter::Insert discards its
//	    return value).
//	(4) RenderWriter's HighPassFilter on the render path, gated upstream
//	    by config.Filter.HighPassFilterEchoReference: skipped
//	    unconditionally. The config field is not read anywhere in this
//	    port.
//	(5) ApiCallJitterMetrics (api_call_metrics_) and its
//	    ReportCaptureCall/ReportRenderCall calls: omitted, pure
//	    instrumentation with no effect on processing.
//	(6) BlockDelayBuffer and config.Delay.FixedCaptureDelaySamples: left
//	    unwired. Not reachable from the public aec API (which never
//	    constructs a non-default config.Config), and DelaySignal is a no-op
//	    whenever fixed_capture_delay_samples == 0, the only value this
//	    port can produce.
//	(7) ApmDataDumper, RTC_DCHECK/RTC_DCHECK_NOTREACHED,
//	    RTC_LOG(_V), and rtc::RaceChecker: debug-only instrumentation
//	    and assertions, omitted uniformly per this port's conventions.
//
// Splitting/merging into frequency bands is owned by EchoCanceller3
// itself in this port (see audio_buffer.go's file header):
// AnalyzeRender, AnalyzeCapture, and ProcessCapture all accept
// full-band frames and call AudioBuffer.SplitIntoFrequencyBands/
// MergeFrequencyBands internally, where upstream instead relies on
// AudioProcessingImpl having already split the AudioBuffer before
// calling in (there is no separate AudioProcessingImpl class in this
// port; EchoCanceller3 is the entire pipeline).
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// detectSaturation reports whether any sample in y (FloatS16 domain)
// is at or beyond the saturation threshold. C: echo_canceller3.cc's
// (anonymous namespace) DetectSaturation.
func detectSaturation(y []float32) bool {
	for _, v := range y {
		if v >= 32700.0 || v <= -32700.0 {
			return true
		}
	}
	return false
}

// makeSubFrameViewShape allocates the outer/middle bookkeeping arrays
// (shape [numBands][numChannels], nil leaf slices) for a reusable
// fillSubFrameView/fillSubFrameView1Band destination. Called once per
// EchoCanceller3 (per fixed band/channel shape); fillSubFrameView*
// only ever rewrites the leaf slice headers afterward, so the
// dst returned here needs no further allocation for the lifetime of
// the owning EchoCanceller3.
func makeSubFrameViewShape(numBands, numChannels int) [][][]float32 {
	view := make([][][]float32, numBands)
	for band := range view {
		view[band] = make([][]float32, numChannels)
	}
	return view
}

// fillSubFrameView populates dst (previously shaped by
// makeSubFrameViewShape with matching band/channel counts) with a view
// into split's subFrameIndex-th (0 or 1) SubFrameLength-sample
// sub-frame, aliasing split's underlying storage -- no allocation, and
// writes through the resulting leaf slices are visible in split. Safe
// to call repeatedly with a different split argument each time (e.g.
// once per queued render frame drained by emptyRenderQueue): dst is a
// reusable scratch view, never itself retained past the call that
// populates it, so only the leaf slice HEADERS are overwritten --  the
// underlying sample data always still belongs to whichever split was
// passed in that call. C: echo_canceller3.cc's (anonymous namespace)
// FillSubFrameView(AudioBuffer*, size_t,
// vector<vector<ArrayView<float>>>*); Go slices already alias their
// backing storage, so this replaces the whole ArrayView/
// FillSubFrameView machinery with direct re-slicing into a reused
// wrapper.
func fillSubFrameView(dst, split [][][]float32, subFrameIndex int) {
	start := subFrameIndex * SubFrameLength
	for band := range split {
		for ch := range split[band] {
			dst[band][ch] = split[band][ch][start : start+SubFrameLength : start+SubFrameLength]
		}
	}
}

// fillSubFrameView1Band is fillSubFrameView specialized for the linear
// AEC output path's single-band [channel][sample] representation (the
// linear output is always single-band, regardless of the canceller's
// actual number of bands -- see ProcessCaptureWithLinearOutput). dst
// must have been shaped by makeSubFrameViewShape(1, len(data)).
func fillSubFrameView1Band(dst [][][]float32, data [][]float32, subFrameIndex int) {
	start := subFrameIndex * SubFrameLength
	for ch := range data {
		dst[0][ch] = data[ch][start : start+SubFrameLength : start+SubFrameLength]
	}
}

// EchoCanceller3 is the top-level AEC3 echo canceller: it analyzes a
// render (far-end/loudspeaker) signal buffered via AnalyzeRender, and
// removes the resulting echo from a capture (near-end/microphone)
// signal via ProcessCapture. C: EchoCanceller3.
//
// Concurrency contract: see the file header's drop (3). All exported
// methods must be called from a single goroutine, serialized by the
// caller.
type EchoCanceller3 struct {
	config             config.Config
	sampleRateHz       int
	numBands           int
	numFrames          int // full-band frame length: sampleRateHz / 100
	numRenderChannels  int
	numCaptureChannels int

	renderAudioBuffer  *AudioBuffer
	captureAudioBuffer *AudioBuffer

	renderBlocker  *FrameBlocker
	captureBlocker *FrameBlocker
	outputFramer   *BlockFramer

	linearOutputFramer *BlockFramer // non-nil iff config.Filter.ExportLinearAecOutput
	linearOutputBlock  *Block       // non-nil iff config.Filter.ExportLinearAecOutput

	blockProcessor *BlockProcessor

	// renderQueue holds split-band render frames
	// ([band][channel][FrameSize]) awaiting BufferRender, in arrival
	// order. C: render_transfer_queue_ (see drop (3)).
	renderQueue [][][][]float32

	// renderFramesDropped counts AnalyzeRender calls discarded because
	// renderQueue was already at RenderTransferQueueSizeFrames capacity
	// -- see AnalyzeRender and RenderFramesDropped.
	renderFramesDropped uint64

	renderBlock  *Block
	captureBlock *Block

	// renderSubFrameView/captureSubFrameView are reusable
	// fillSubFrameView destinations (shape [numBands][numRenderChannels]
	// / [numBands][numCaptureChannels]), owned by this EchoCanceller3
	// and refilled on every bufferRenderFrameContent/
	// processCaptureFrameContent call -- see fillSubFrameView's doc
	// comment for why reuse here is safe. linearSubFrameView is the
	// equivalent for the (always single-band) linear AEC output path,
	// non-nil iff config.Filter.ExportLinearAecOutput.
	renderSubFrameView  [][][]float32
	captureSubFrameView [][][]float32
	linearSubFrameView  [][][]float32

	saturatedMicrophoneSignal bool
}

// NewEchoCanceller3 constructs an EchoCanceller3 for sampleRateHz
// (16000, 32000, or 48000), with numRenderChannels render channels and
// numCaptureChannels capture channels, fixed for the object's
// lifetime. C: EchoCanceller3's constructor followed by Initialize()
// (collapsed into one step per drop (2): num_render_channels_to_aec_
// is always numRenderChannels).
func NewEchoCanceller3(config config.Config, sampleRateHz, numRenderChannels, numCaptureChannels int) *EchoCanceller3 {
	numBands := NumBandsForRate(sampleRateHz)
	ec := &EchoCanceller3{
		config:             config,
		sampleRateHz:       sampleRateHz,
		numBands:           numBands,
		numFrames:          sampleRateHz / 100,
		numRenderChannels:  numRenderChannels,
		numCaptureChannels: numCaptureChannels,
		// renderAudioBuffer's split results are queued (renderQueue)
		// and can have many simultaneously outstanding under the ~1s
		// feed-ahead pacing contract, so it must NOT reuse scratch
		// across calls. captureAudioBuffer's split/merge results never
		// outlive a single processCapture call, so scratch reuse there
		// is safe and saves an allocation every 10ms frame. See
		// AudioBuffer.reuseScratch's doc comment.
		renderAudioBuffer:   NewAudioBuffer(sampleRateHz, numRenderChannels, false),
		captureAudioBuffer:  NewAudioBuffer(sampleRateHz, numCaptureChannels, true),
		renderBlocker:       NewFrameBlocker(numBands, numRenderChannels),
		captureBlocker:      NewFrameBlocker(numBands, numCaptureChannels),
		outputFramer:        NewBlockFramer(numBands, numCaptureChannels),
		blockProcessor:      NewBlockProcessor(config, sampleRateHz, numRenderChannels, numCaptureChannels),
		renderBlock:         NewBlock(numBands, numRenderChannels),
		captureBlock:        NewBlock(numBands, numCaptureChannels),
		renderSubFrameView:  makeSubFrameViewShape(numBands, numRenderChannels),
		captureSubFrameView: makeSubFrameViewShape(numBands, numCaptureChannels),
	}
	if config.Filter.ExportLinearAecOutput {
		ec.linearOutputFramer = NewBlockFramer(1, numCaptureChannels)
		ec.linearOutputBlock = NewBlock(1, numCaptureChannels)
		ec.linearSubFrameView = makeSubFrameViewShape(1, numCaptureChannels)
	}
	return ec
}

// AnalyzeRender buffers render (one 10ms full-band frame,
// render[channel] of length sampleRateHz/100 each, FloatS16 domain)
// for later application to the capture signal via ProcessCapture.
//
// Render must not be fed more than RenderTransferQueueSizeFrames 10ms
// frames (~1s) ahead of the paired capture stream's ProcessCapture
// calls: once that many split-band frames are buffered awaiting
// drainage (see emptyRenderQueue), further AnalyzeRender calls are
// dropped -- incrementing renderFramesDropped, readable via
// RenderFramesDropped -- rather than growing the queue unboundedly.
// This mirrors upstream's RenderTransferQueue/SwapQueue, which fails
// silently on overflow in the same way (see the file header's drop
// (3)). C: EchoCanceller3::AnalyzeRender(const AudioBuffer&) (=>
// RenderWriter::Insert).
func (ec *EchoCanceller3) AnalyzeRender(render [][]float32) {
	split := ec.renderAudioBuffer.SplitIntoFrequencyBands(render)
	if len(ec.renderQueue) >= RenderTransferQueueSizeFrames {
		ec.renderFramesDropped++
		return
	}
	ec.renderQueue = append(ec.renderQueue, split)
}

// RenderFramesDropped returns the cumulative number of render frames
// AnalyzeRender has discarded because the render queue was already at
// RenderTransferQueueSizeFrames capacity -- i.e. how many times the
// caller fed render more than ~1s ahead of the paired capture stream.
// Like every other exported EchoCanceller3 method, must only be
// called from the single goroutine serializing all calls into this
// EchoCanceller3 (see the type doc comment).
func (ec *EchoCanceller3) RenderFramesDropped() uint64 {
	return ec.renderFramesDropped
}

// AnalyzeCapture updates the microphone-saturation state from capture
// (one 10ms full-band frame, capture[channel] of length
// sampleRateHz/100 each, FloatS16 domain). C:
// EchoCanceller3::AnalyzeCapture.
func (ec *EchoCanceller3) AnalyzeCapture(capture [][]float32) {
	ec.saturatedMicrophoneSignal = false
	for _, ch := range capture {
		if detectSaturation(ch) {
			ec.saturatedMicrophoneSignal = true
			break
		}
	}
}

// emptyRenderQueue drains renderQueue, buffering every queued frame
// into the block processor. Called at the start of ProcessCapture. C:
// EchoCanceller3::EmptyRenderQueue.
func (ec *EchoCanceller3) emptyRenderQueue() {
	for len(ec.renderQueue) > 0 {
		frame := ec.renderQueue[0]
		ec.renderQueue = ec.renderQueue[1:]
		ec.bufferRenderFrameContent(frame, 0)
		ec.bufferRenderFrameContent(frame, 1)
		ec.bufferRemainingRenderFrameContent()
	}
}

// bufferRenderFrameContent buffers one SubFrameLength-sample
// sub-frame of frame into the block processor's render buffer. C:
// echo_canceller3.cc's (anonymous namespace) BufferRenderFrameContent
// (with proper_downmix_needed always false; see drop (2)).
func (ec *EchoCanceller3) bufferRenderFrameContent(frame [][][]float32, subFrameIndex int) {
	fillSubFrameView(ec.renderSubFrameView, frame, subFrameIndex)
	ec.renderBlocker.InsertSubFrameAndExtractBlock(ec.renderSubFrameView, ec.renderBlock)
	ec.blockProcessor.BufferRender(ec.renderBlock)
}

// bufferRemainingRenderFrameContent buffers a residual render block
// left over from the two sub-frame insertions, if a full block has
// accumulated. C: echo_canceller3.cc's (anonymous namespace)
// BufferRemainingRenderFrameContent.
func (ec *EchoCanceller3) bufferRemainingRenderFrameContent() {
	if !ec.renderBlocker.IsBlockAvailable() {
		return
	}
	ec.renderBlocker.ExtractBlock(ec.renderBlock)
	ec.blockProcessor.BufferRender(ec.renderBlock)
}

// ProcessCapture removes echo from capture (one 10ms full-band frame,
// capture[channel] of length sampleRateHz/100 each, FloatS16 domain),
// in place. levelChange should be true on calls where the caller
// knows the capture signal's gain changed since the previous call
// (e.g. an AGC level step), so the adaptive filter can react promptly
// instead of interpreting the level jump as echo path change noise. C:
// EchoCanceller3::ProcessCapture(AudioBuffer*, bool).
func (ec *EchoCanceller3) ProcessCapture(capture [][]float32, levelChange bool) {
	ec.processCapture(capture, nil, levelChange)
}

// ProcessCaptureWithLinearOutput is ProcessCapture, additionally
// populating linearOutput (linearOutput[channel] of length
// SubFrameLength*2 (== FrameSize) each, FloatS16 domain, always
// single-band regardless of the canceller's actual band count) with
// the pre-suppression-gain linear AEC output. If
// config.Filter.ExportLinearAecOutput was false at construction,
// linearOutput is left unmodified (upstream instead asserts; see drop
// (7)). C: EchoCanceller3::ProcessCapture(AudioBuffer*, AudioBuffer*,
// bool).
func (ec *EchoCanceller3) ProcessCaptureWithLinearOutput(capture, linearOutput [][]float32, levelChange bool) {
	ec.processCapture(capture, linearOutput, levelChange)
}

func (ec *EchoCanceller3) processCapture(capture, linearOutput [][]float32, levelChange bool) {
	ec.emptyRenderQueue()

	captureSplit := ec.captureAudioBuffer.SplitIntoFrequencyBands(capture)

	var linearOutputSplit [][]float32
	if linearOutput != nil {
		linearOutputSplit = make([][]float32, ec.numCaptureChannels)
		for ch := range linearOutputSplit {
			linearOutputSplit[ch] = make([]float32, FrameSize)
		}
	}

	ec.processCaptureFrameContent(captureSplit, linearOutputSplit, levelChange, 0)
	ec.processCaptureFrameContent(captureSplit, linearOutputSplit, levelChange, 1)
	ec.processRemainingCaptureFrameContent(levelChange)

	full := ec.captureAudioBuffer.MergeFrequencyBands(captureSplit)
	for ch := range capture {
		copy(capture[ch], full[ch])
	}
	if linearOutput != nil {
		for ch := range linearOutput {
			copy(linearOutput[ch], linearOutputSplit[ch])
		}
	}
}

// processCaptureFrameContent processes one SubFrameLength-sample
// sub-frame (subFrameIndex 0 or 1) of captureSplit through the block
// processor, in place, optionally also extracting the linear AEC
// output sub-frame into linearOutputSplit. C: echo_canceller3.cc's
// (anonymous namespace) ProcessCaptureFrameContent (with
// aec_reference_is_downmixed_stereo always false; see drop (2)).
func (ec *EchoCanceller3) processCaptureFrameContent(captureSplit [][][]float32, linearOutputSplit [][]float32, levelChange bool, subFrameIndex int) {
	fillSubFrameView(ec.captureSubFrameView, captureSplit, subFrameIndex)
	captureView := ec.captureSubFrameView

	ec.captureBlocker.InsertSubFrameAndExtractBlock(captureView, ec.captureBlock)

	// ec.linearOutputBlock is populated whenever the canceller was
	// constructed with ExportLinearAecOutput, regardless of whether
	// THIS call requested a copy of it via linearOutputSplit --
	// matching upstream, which always threads linear_output_block_
	// through to BlockProcessor::ProcessCapture and only conditionally
	// copies its content out to the caller-supplied linear_output
	// AudioBuffer.
	ec.blockProcessor.ProcessCapture(levelChange, ec.saturatedMicrophoneSignal, ec.linearOutputBlock, ec.captureBlock)

	ec.outputFramer.InsertBlockAndExtractSubFrame(ec.captureBlock, captureView)

	if linearOutputSplit != nil && ec.linearOutputFramer != nil {
		fillSubFrameView1Band(ec.linearSubFrameView, linearOutputSplit, subFrameIndex)
		ec.linearOutputFramer.InsertBlockAndExtractSubFrame(ec.linearOutputBlock, ec.linearSubFrameView)
	}
}

// processRemainingCaptureFrameContent processes a residual capture
// block left over from the two sub-frame insertions, if a full block
// has accumulated. Note that, matching upstream, the linear AEC output
// is pushed into linearOutputFramer whenever it exists, independent of
// whether the current ProcessCapture[WithLinearOutput] call requested
// a copy via linearOutputSplit -- the framer's internal carry buffer
// must stay consistent across calls regardless of per-call requests.
// C: echo_canceller3.cc's (anonymous namespace)
// ProcessRemainingCaptureFrameContent.
func (ec *EchoCanceller3) processRemainingCaptureFrameContent(levelChange bool) {
	if !ec.captureBlocker.IsBlockAvailable() {
		return
	}
	ec.captureBlocker.ExtractBlock(ec.captureBlock)

	ec.blockProcessor.ProcessCapture(levelChange, ec.saturatedMicrophoneSignal, ec.linearOutputBlock, ec.captureBlock)

	ec.outputFramer.InsertBlock(ec.captureBlock)

	if ec.linearOutputFramer != nil {
		ec.linearOutputFramer.InsertBlock(ec.linearOutputBlock)
	}
}

// GetMetrics collects current metrics from the echo canceller. C:
// EchoCanceller3::GetMetrics.
func (ec *EchoCanceller3) GetMetrics() Metrics {
	var m Metrics
	ec.blockProcessor.GetMetrics(&m)
	return m
}

// SetAudioBufferDelay reports an external estimate of the audio
// buffer delay. C: EchoCanceller3::SetAudioBufferDelay.
func (ec *EchoCanceller3) SetAudioBufferDelay(delayMs int) {
	ec.blockProcessor.SetAudioBufferDelay(delayMs)
}

// SetCaptureOutputUsage specifies whether the capture output will be
// used. C: EchoCanceller3::SetCaptureOutputUsage.
func (ec *EchoCanceller3) SetCaptureOutputUsage(captureOutputUsed bool) {
	ec.blockProcessor.SetCaptureOutputUsage(captureOutputUsed)
}

// UpdateEchoLeakageStatus reports whether echo leakage has been
// detected in the echo canceller output. C:
// EchoCanceller3::UpdateEchoLeakageStatus.
func (ec *EchoCanceller3) UpdateEchoLeakageStatus(leakageDetected bool) {
	ec.blockProcessor.UpdateEchoLeakageStatus(leakageDetected)
}

// SetSuppressorGating selects whether the suppressor is gated on
// demonstrated convergence. Go-port-only, beyond upstream; see
// suppressor_gate.go. The default (SuppressorGatingDisabled) reproduces
// upstream AEC3 exactly.
func (ec *EchoCanceller3) SetSuppressorGating(mode SuppressorGating) {
	ec.blockProcessor.SetSuppressorGating(mode)
}

// SuppressorGate reports whether the suppressor is currently allowed to
// attenuate the capture path. Go-port-only, beyond upstream; see
// suppressor_gate.go.
func (ec *EchoCanceller3) SuppressorGate() SuppressorGateState {
	return ec.blockProcessor.SuppressorGate()
}

// Clockdrift reports the clockdrift level currently detected by the
// render delay controller. Go-port-only addition; see
// BlockProcessor.Clockdrift's doc comment (block_processor.go).
func (ec *EchoCanceller3) Clockdrift() ClockdriftLevel {
	return ec.blockProcessor.Clockdrift()
}

// ActiveProcessing always returns true: this port has no
// "suspended"/passthrough mode. C: EchoCanceller3::ActiveProcessing.
func (ec *EchoCanceller3) ActiveProcessing() bool {
	return true
}

// CreateDefaultMultichannelConfig returns a config.Config tuned for
// multichannel (proper multi-channel, not upmixed-mono) operation: a
// shorter, more rapidly adapting coarse filter (to compensate for the
// increased number of total filter parameters to adapt) and a more
// conservative suppressor for non-nearend speech. C:
// EchoCanceller3::CreateDefaultMultichannelConfig.
func CreateDefaultMultichannelConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Filter.Coarse.LengthBlocks = 11
	cfg.Filter.Coarse.Rate = 0.95
	cfg.Filter.CoarseInitial.LengthBlocks = 11
	cfg.Filter.CoarseInitial.Rate = 0.95

	cfg.Suppressor.NormalTuning.MaxDecFactorLF = 0.35
	cfg.Suppressor.NormalTuning.MaxIncFactor = 1.5
	return cfg
}
