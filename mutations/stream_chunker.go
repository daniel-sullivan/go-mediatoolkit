package mutations

// StreamChunker accumulates float64 samples across calls and emits
// fixed-size chunks as they fill up. It is the stateful complement to
// [Chunk] and [ChunkFunc]: callers can Write arbitrary-sized buffers and
// receive chunks of exactly size whenever enough samples have arrived.
//
// For multi-channel interleaved audio, size should be a multiple of the
// channel count to preserve frame alignment.
//
// A StreamChunker is not safe for concurrent use.
type StreamChunker struct {
	size       int
	pending    []float64
	pendingLen int
}

// NewStreamChunker returns a chunker that emits chunks of the given size.
// A size <= 0 produces a chunker whose Write and Flush are no-ops.
func NewStreamChunker(size int) *StreamChunker {
	if size <= 0 {
		return &StreamChunker{}
	}
	return &StreamChunker{
		size:    size,
		pending: make([]float64, size),
	}
}

// Write ingests buf and invokes fn once for every complete chunk produced.
// Samples that do not fill a chunk remain buffered for the next call.
// Returns the number of samples from buf that were consumed (always
// len(buf) on success; on error, the count up to and including the samples
// passed to the failing fn call).
//
// The slice passed to fn must not be retained across calls — it is either a
// view into buf or the chunker's internal buffer, both of which are reused.
func (c *StreamChunker) Write(buf []float64, fn func(chunk []float64) error) (int, error) {
	if c.size == 0 {
		return 0, nil
	}
	consumed := 0
	for len(buf) > 0 {
		// Zero-copy fast path: nothing buffered and buf holds at least one
		// whole chunk — emit directly from buf.
		if c.pendingLen == 0 && len(buf) >= c.size {
			if err := fn(buf[:c.size]); err != nil {
				return consumed + c.size, err
			}
			buf = buf[c.size:]
			consumed += c.size
			continue
		}
		space := c.size - c.pendingLen
		n := len(buf)
		if n > space {
			n = space
		}
		copy(c.pending[c.pendingLen:], buf[:n])
		c.pendingLen += n
		buf = buf[n:]
		consumed += n
		if c.pendingLen == c.size {
			if err := fn(c.pending); err != nil {
				return consumed, err
			}
			c.pendingLen = 0
		}
	}
	return consumed, nil
}

// Flush emits any remaining partial chunk, zero-padded to full size, and
// resets the internal buffer. If nothing is pending, fn is not called.
func (c *StreamChunker) Flush(fn func(chunk []float64) error) error {
	if c.pendingLen == 0 {
		return nil
	}
	for i := c.pendingLen; i < c.size; i++ {
		c.pending[i] = 0
	}
	c.pendingLen = 0
	return fn(c.pending)
}

// Pending returns the number of samples currently buffered.
func (c *StreamChunker) Pending() int { return c.pendingLen }

// Size returns the chunk size this chunker emits.
func (c *StreamChunker) Size() int { return c.size }

// Reset discards any buffered samples without emitting a chunk.
func (c *StreamChunker) Reset() { c.pendingLen = 0 }
