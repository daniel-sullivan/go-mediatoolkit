package vad

// hysteresis is the onset/hangover smoothing state machine shared by
// the detector engines. It consumes one raw per-decision-frame voiced
// decision at a time and produces debounced SpeechStart/SpeechEnd
// transitions with back-timestamped positions:
//
//   - A silence→speech transition fires only after onsetFrames
//     CONSECUTIVE raw-voiced frames, and is timestamped to the input
//     position where that run began (the onset debounce delays the
//     announcement, not the reported position).
//   - A speech→silence transition fires only after hangoverFrames
//     consecutive raw-unvoiced frames, and is timestamped to the
//     position where the silence run began — i.e. where speech
//     actually stopped. Any shorter silence run (a stop closure, an
//     inter-word gap) is bridged: the run counter resets and no
//     transition fires.
//
// onsetFrames/hangoverFrames are passed per step (not stored) because
// they are live-tunable: the engine snapshots its atomics once per
// decision frame and hands the current values in. A value < 1 is
// treated as 1 — a transition always needs at least the current frame
// as evidence.
//
// hysteresis is owned by the audio goroutine; it contains no atomics
// and is not safe for concurrent use.
type hysteresis struct {
	active bool

	voicedRun int   // consecutive raw-voiced frames (counted while !active)
	runStart  int64 // input frame where the current voiced run began

	silenceRun   int   // consecutive raw-unvoiced frames (counted while active)
	silenceStart int64 // input frame where the current silence run began
}

// step consumes one decision frame's raw voiced decision. frameStart
// is the input-stream frame position where this decision frame begins
// (already mapped from the engine's internal timeline). It returns
// the transition to emit and its back-timestamped frame position, or
// fired == false when the state is unchanged.
func (h *hysteresis) step(raw bool, frameStart int64, onsetFrames, hangoverFrames int) (kind SpeechEventKind, eventFrame int64, fired bool) {
	if onsetFrames < 1 {
		onsetFrames = 1
	}
	if hangoverFrames < 1 {
		hangoverFrames = 1
	}
	if !h.active {
		if !raw {
			h.voicedRun = 0
			return 0, 0, false
		}
		if h.voicedRun == 0 {
			h.runStart = frameStart
		}
		h.voicedRun++
		if h.voicedRun < onsetFrames {
			return 0, 0, false
		}
		h.active = true
		h.voicedRun = 0
		h.silenceRun = 0
		return SpeechStart, h.runStart, true
	}
	if raw {
		h.silenceRun = 0
		return 0, 0, false
	}
	if h.silenceRun == 0 {
		h.silenceStart = frameStart
	}
	h.silenceRun++
	if h.silenceRun < hangoverFrames {
		return 0, 0, false
	}
	h.active = false
	h.silenceRun = 0
	h.voicedRun = 0
	return SpeechEnd, h.silenceStart, true
}

// reset returns the machine to its just-constructed state (inactive,
// no runs in progress).
func (h *hysteresis) reset() {
	h.active = false
	h.voicedRun = 0
	h.runStart = 0
	h.silenceRun = 0
	h.silenceStart = 0
}
