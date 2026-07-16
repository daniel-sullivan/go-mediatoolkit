package duplex

// Event is the closed set of capture-side events an Engine delivers
// on its Events channel: SpeechStart, AudioFrame, and SpeechStop.
// Delivery is strictly FIFO in the order the audio goroutine produced
// them — for one utterance: SpeechStart, the lead-in silence frames,
// the pre-roll replay, the live frames, SpeechStop.
type Event interface {
	// duplexEvent marks the closed set of event types; it carries no
	// behaviour.
	duplexEvent()
}

// SpeechStart announces the beginning of an utterance. It is fired
// once per utterance (redundant detector starts while speech is
// already in progress are absorbed) and is followed by the
// utterance's audio as AudioFrame events.
type SpeechStart struct {
	// Timestamp is the utterance's start position in tag units:
	// the tag of the oldest replayed pre-roll frame (or of the
	// triggering live frame when the pre-roll is empty or disabled),
	// backdated by Config.LeadIn and clamped to be never negative —
	// it points at the first AudioFrame of the utterance, lead-in
	// silence included.
	Timestamp int64

	// OnsetFrame is the detector's back-timestamped stream position
	// of the speech onset (vad.SpeechEvent.Frame): capture frames
	// fed to the detector since construction, at the engine sample
	// rate. It is the detector's own estimate of where speech began,
	// independent of the tag timeline.
	OnsetFrame int64
}

func (SpeechStart) duplexEvent() {}

// AudioFrame carries one 10 ms frame of an in-progress utterance:
// lead-in silence, replayed pre-roll, or live capture — all post-DSP
// (echo-cancelled and capture-chain processed). Frame is a copy owned
// by the receiver.
type AudioFrame struct {
	// Frame is interleaved float64 in [-1, 1], one engine frame long.
	Frame []float64

	// Tag is the frame's position in tag units: the caller-supplied
	// tag for live and pre-roll frames, or a back-dated synthetic tag
	// for lead-in silence (see Config.LeadIn).
	Tag int64
}

func (AudioFrame) duplexEvent() {}

// SpeechStop announces the end of the utterance in progress. No
// further AudioFrame events follow until the next SpeechStart.
type SpeechStop struct {
	// Tag is the tag of the capture frame whose processing confirmed
	// the end of speech.
	Tag int64
}

func (SpeechStop) duplexEvent() {}
