package duplex

import "strconv"

// EventKind identifies which variant of the Event tagged union a
// value carries — and therefore which of its fields are meaningful.
type EventKind int

const (
	// EventSpeechStart announces the beginning of an utterance. It is
	// fired once per utterance (redundant detector starts while
	// speech is already in progress are absorbed) and is followed by
	// the utterance's audio as EventAudioFrame events. Fields:
	// Timestamp, OnsetFrame.
	EventSpeechStart EventKind = iota

	// EventAudioFrame carries one 10 ms frame of an in-progress
	// utterance: lead-in silence, replayed pre-roll, or live capture
	// — all post-DSP (echo-cancelled and capture-chain processed).
	// Fields: Frame, Tag.
	EventAudioFrame

	// EventSpeechStop announces the end of the utterance in progress.
	// No further EventAudioFrame events follow until the next
	// EventSpeechStart. Fields: Tag.
	EventSpeechStop
)

// String returns the constant's name (or a decorated integer for an
// out-of-range value).
func (k EventKind) String() string {
	switch k {
	case EventSpeechStart:
		return "EventSpeechStart"
	case EventAudioFrame:
		return "EventAudioFrame"
	case EventSpeechStop:
		return "EventSpeechStop"
	default:
		return "EventKind(" + strconv.Itoa(int(k)) + ")"
	}
}

// Event is one capture-side event, delivered on the Events channel in
// strict FIFO order — for one utterance: EventSpeechStart, the
// lead-in silence frames, the pre-roll replay, the live frames,
// EventSpeechStop. It is a tagged union: Kind selects the variant,
// and only the fields that variant documents are meaningful (the rest
// are zero). One flat struct rather than an interface keeps the
// 100-events-per-second in-speech stream free of per-event interface
// boxing.
type Event struct {
	// Kind selects the variant.
	Kind EventKind

	// Timestamp (EventSpeechStart) is the utterance's start position
	// in tag units: the tag of the oldest replayed pre-roll frame (or
	// of the triggering live frame when the pre-roll is empty or
	// disabled), backdated by Config.LeadIn and clamped to be never
	// negative — it points at the first EventAudioFrame of the
	// utterance, lead-in silence included.
	Timestamp int64

	// OnsetFrame (EventSpeechStart) is the detector's
	// back-timestamped stream position of the speech onset
	// (vad.SpeechEvent.Frame): capture frames fed to the detector
	// since construction, at the engine sample rate. It is the
	// detector's own estimate of where speech began, independent of
	// the tag timeline.
	OnsetFrame int64

	// Frame (EventAudioFrame) is the frame's audio: interleaved
	// float64 in [-1, 1], one engine frame long, a copy owned by the
	// receiver.
	Frame []float64

	// Tag (EventAudioFrame, EventSpeechStop) is the frame's position
	// in tag units: the caller-supplied tag for live and pre-roll
	// frames, a back-dated synthetic tag for lead-in silence (see
	// Config.LeadIn), or — for EventSpeechStop — the tag of the
	// capture frame whose processing confirmed the end of speech.
	Tag int64
}
