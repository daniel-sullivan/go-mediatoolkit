package mutations

import "time"

// FramesToDuration converts a frame count at sampleRate to the
// corresponding elapsed time. Integer nanosecond division introduces
// small truncation error at rates that do not divide evenly into a
// second (notably 44.1 kHz and 96 kHz); use DurationToFrames to round
// back cleanly.
func FramesToDuration(frames int64, sampleRate int) time.Duration {
	return time.Duration(frames) * time.Second / time.Duration(sampleRate)
}

// DurationToFrames converts a duration to a frame count at sampleRate,
// rounding to the nearest frame. Rounding (not truncation) is required
// for round-trip stability with FramesToDuration at common rates where
// one frame does not divide evenly into a nanosecond.
func DurationToFrames(d time.Duration, sampleRate int) int64 {
	ns := int64(d)
	rate := int64(sampleRate)
	half := int64(time.Second) / 2
	if ns >= 0 {
		return (ns*rate + half) / int64(time.Second)
	}
	return (ns*rate - half) / int64(time.Second)
}
