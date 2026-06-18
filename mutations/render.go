package mutations

// RenderBuffer applies a chain of Processors to input, extending the
// output by tailFrames of silence so tail-carrying effects (echo,
// reverb) can decay past the input's end. Returns a newly-allocated
// buffer of length len(input) + tailFrames*channels.
//
// This is the offline counterpart to timeline.EffectSource.WithTail:
// pure function, no Source or Timeline involvement, suitable for
// rendering a clip through effects and handing the result to an
// encoder or file writer.
//
// Processors are stateful — a fresh chain produces a fresh render.
// Reusing a chain from a previous render carries over its internal
// state (e.g. a reverb still echoing); call Reset on each processor
// between renders if independence is required.
func RenderBuffer(input []float64, chain []Processor, tailFrames, channels int) []float64 {
	if channels < 1 {
		channels = 1
	}
	if tailFrames < 0 {
		tailFrames = 0
	}
	out := make([]float64, len(input)+tailFrames*channels)
	copy(out, input)
	for _, p := range chain {
		p.Process(out)
	}
	return out
}
