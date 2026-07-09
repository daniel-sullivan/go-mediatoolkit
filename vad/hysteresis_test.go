package vad

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHysteresisOnsetAndBackTimestamp(t *testing.T) {
	var h hysteresis

	// Two voiced frames under a 3-frame onset: nothing fires.
	for i := 0; i < 2; i++ {
		_, _, fired := h.step(true, int64(100+i*10), 3, 2)
		assert.False(t, fired)
	}
	// A dropout resets the run.
	_, _, fired := h.step(false, 120, 3, 2)
	assert.False(t, fired)

	// Three consecutive voiced frames fire, back-timestamped to the
	// run start (frame 130), not the confirming frame (150).
	_, _, fired = h.step(true, 130, 3, 2)
	assert.False(t, fired)
	_, _, fired = h.step(true, 140, 3, 2)
	assert.False(t, fired)
	kind, frame, fired := h.step(true, 150, 3, 2)
	assert.True(t, fired)
	assert.Equal(t, SpeechStart, kind)
	assert.Equal(t, int64(130), frame)
	assert.True(t, h.active)
}

func TestHysteresisHangoverBridgesAndSplits(t *testing.T) {
	var h hysteresis
	// Enter speech with a 1-frame onset.
	_, _, fired := h.step(true, 0, 1, 3)
	assert.True(t, fired)

	// A 2-frame gap under the 3-frame hangover is bridged.
	for i := 0; i < 2; i++ {
		_, _, fired = h.step(false, int64(10+i*10), 1, 3)
		assert.False(t, fired)
	}
	_, _, fired = h.step(true, 30, 1, 3)
	assert.False(t, fired, "speech resuming inside the hangover must not fire anything")

	// Three consecutive silent frames fire SpeechEnd, back-timestamped
	// to where the silence began (frame 40).
	_, _, fired = h.step(false, 40, 1, 3)
	assert.False(t, fired)
	_, _, fired = h.step(false, 50, 1, 3)
	assert.False(t, fired)
	kind, frame, fired := h.step(false, 60, 1, 3)
	assert.True(t, fired)
	assert.Equal(t, SpeechEnd, kind)
	assert.Equal(t, int64(40), frame)
	assert.False(t, h.active)
}

func TestHysteresisSubFrameDebounceClampsToOne(t *testing.T) {
	var h hysteresis
	// onsetFrames/hangoverFrames of 0 (a sub-frame debounce) behave as
	// 1: a single frame flips the state.
	kind, frame, fired := h.step(true, 7, 0, 0)
	assert.True(t, fired)
	assert.Equal(t, SpeechStart, kind)
	assert.Equal(t, int64(7), frame)
	kind, frame, fired = h.step(false, 8, 0, 0)
	assert.True(t, fired)
	assert.Equal(t, SpeechEnd, kind)
	assert.Equal(t, int64(8), frame)
}

func TestHysteresisReset(t *testing.T) {
	var h hysteresis
	h.step(true, 0, 1, 1)
	assert.True(t, h.active)
	h.reset()
	assert.False(t, h.active)
	// After reset a fresh onset run is required again.
	_, _, fired := h.step(true, 100, 2, 1)
	assert.False(t, fired)
	kind, frame, fired := h.step(true, 110, 2, 1)
	assert.True(t, fired)
	assert.Equal(t, SpeechStart, kind)
	assert.Equal(t, int64(100), frame)
}

func TestGainRampSlewAndClamp(t *testing.T) {
	r := gainRamp{gain: 0}

	// Rising: bounded per-step, clamps exactly at the target.
	assert.Equal(t, 0.25, r.step(1, 0.25, 0.5))
	assert.Equal(t, 0.5, r.step(1, 0.25, 0.5))
	assert.Equal(t, 0.75, r.step(1, 0.25, 0.5))
	assert.Equal(t, 1.0, r.step(1, 0.25, 0.5))
	assert.Equal(t, 1.0, r.step(1, 0.25, 0.5), "at the target the ramp must hold exactly")

	// Falling with the independent fall step; exact landing on a
	// non-multiple target.
	assert.Equal(t, 0.5, r.step(0.1, 0.25, 0.5))
	assert.Equal(t, 0.1, r.step(0.1, 0.25, 0.5))
	assert.Equal(t, 0.1, r.step(0.1, 0.25, 0.5))
}
