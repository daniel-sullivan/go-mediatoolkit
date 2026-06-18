package mutations

// Processor is a stateful audio effect that modifies interleaved
// samples in place. Implementations typically carry delay buffers,
// filter state, or both; a single Processor is therefore bound to the
// stream it was constructed for and is not safe to share across
// logical streams or goroutines without explicit synchronisation.
//
// Processor is the composable building block for effects chains:
// wrap a Source with timeline.EffectSource to apply one or more
// Processors to every Pull, or call Process directly on bare buffers
// outside the Source/Timeline machinery.
type Processor interface {
	// Process modifies samples in place. samples must be an
	// interleaved buffer with the same channel count the processor
	// was constructed for.
	Process(samples []float64)

	// Reset clears internal state so the processor can be re-used
	// for a fresh stream (e.g. after a seek or loop wrap).
	Reset()
}
