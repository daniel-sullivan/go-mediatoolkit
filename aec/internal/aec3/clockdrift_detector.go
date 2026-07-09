// This file ports modules/audio_processing/aec3/
// clockdrift_detector.{h,cc}.
package aec3

// ClockdriftLevel is ClockdriftDetector::Level.
type ClockdriftLevel int

const (
	// ClockdriftLevelNone == ClockdriftDetector::Level::kNone.
	ClockdriftLevelNone ClockdriftLevel = iota
	// ClockdriftLevelProbable == ClockdriftDetector::Level::kProbable.
	ClockdriftLevelProbable
	// ClockdriftLevelVerified == ClockdriftDetector::Level::kVerified.
	ClockdriftLevelVerified
)

// ClockdriftDetector detects clockdrift by analyzing the estimated
// delay. Port of ClockdriftDetector.
type ClockdriftDetector struct {
	delayHistory     [3]int
	level            ClockdriftLevel
	stabilityCounter int
}

// NewClockdriftDetector mirrors ClockdriftDetector's C++ constructor.
func NewClockdriftDetector() *ClockdriftDetector {
	return &ClockdriftDetector{level: ClockdriftLevelNone}
}

// ClockdriftLevel returns the currently detected clockdrift level. C:
// ClockdriftDetector::ClockdriftLevel.
func (c *ClockdriftDetector) ClockdriftLevel() ClockdriftLevel { return c.level }

// Update updates the detector with a new delay estimate. C:
// ClockdriftDetector::Update.
func (c *ClockdriftDetector) Update(delayEstimate int) {
	if delayEstimate == c.delayHistory[0] {
		// Reset clockdrift level if delay estimate is stable for 7500
		// blocks (30 seconds).
		c.stabilityCounter++
		if c.stabilityCounter > 7500 {
			c.level = ClockdriftLevelNone
		}
		return
	}

	c.stabilityCounter = 0
	d1 := c.delayHistory[0] - delayEstimate
	d2 := c.delayHistory[1] - delayEstimate
	d3 := c.delayHistory[2] - delayEstimate

	// Patterns recognized as positive clockdrift:
	// [x-3], x-2, x-1, x.
	// [x-3], x-1, x-2, x.
	probableDriftUp := (d1 == -1 && d2 == -2) || (d1 == -2 && d2 == -1)
	driftUp := probableDriftUp && d3 == -3

	// Patterns recognized as negative clockdrift:
	// [x+3], x+2, x+1, x.
	// [x+3], x+1, x+2, x.
	probableDriftDown := (d1 == 1 && d2 == 2) || (d1 == 2 && d2 == 1)
	driftDown := probableDriftDown && d3 == 3

	if driftUp || driftDown {
		c.level = ClockdriftLevelVerified
	} else if (probableDriftUp || probableDriftDown) && c.level == ClockdriftLevelNone {
		c.level = ClockdriftLevelProbable
	}

	c.delayHistory[2] = c.delayHistory[1]
	c.delayHistory[1] = c.delayHistory[0]
	c.delayHistory[0] = delayEstimate
}
