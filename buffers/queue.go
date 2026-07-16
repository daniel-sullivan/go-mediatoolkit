package buffers

// Queue is a generic head-indexed FIFO: Push appends at the tail, Pop
// removes at the head, and the dead prefix Pop leaves behind is
// compacted away once it dominates the backing array, so a long-lived
// queue's memory tracks its live contents rather than its history.
// It is the frame-queue building block the toolkit's streaming
// engines share (paired with Slab when the payloads carry sample
// buffers worth recycling).
//
// Unlike Ring, a Queue is not safe for concurrent use — drive it from
// one goroutine or synchronise externally.
type Queue[T any] struct {
	items []T
	head  int
}

// Push appends v at the tail.
func (q *Queue[T]) Push(v T) {
	q.items = append(q.items, v)
}

// Len reports the queued item count.
func (q *Queue[T]) Len() int {
	return len(q.items) - q.head
}

// Pop removes and returns the head item, or a zero value and false if
// the queue is empty. The vacated slot is zeroed so popped payloads
// are not retained by the backing array.
func (q *Queue[T]) Pop() (T, bool) {
	var zero T
	if q.Len() == 0 {
		return zero, false
	}
	v := q.items[q.head]
	q.items[q.head] = zero
	q.head++
	q.compact()
	return v, true
}

// Peek returns a pointer to the head item, or nil if the queue is
// empty. The pointer is valid until the next Push, Pop, or Reset.
func (q *Queue[T]) Peek() *T {
	if q.Len() == 0 {
		return nil
	}
	return &q.items[q.head]
}

// Reset discards every queued item (zeroing the slots), keeping the
// backing array for reuse.
func (q *Queue[T]) Reset() {
	clear(q.items)
	q.items = q.items[:0]
	q.head = 0
}

// compact re-anchors the queue at index 0 once the dead prefix
// dominates the slice.
func (q *Queue[T]) compact() {
	if q.head > 0 && q.head >= len(q.items)/2 {
		n := copy(q.items, q.items[q.head:])
		clear(q.items[n:])
		q.items = q.items[:n]
		q.head = 0
	}
}

// Slab recycles float64 sample buffers across a queue's push/pop
// cycles so a steady stream of copied frames stops allocating once
// the working set has warmed up. Like Queue it is single-goroutine —
// synchronise externally when producer and consumer recycle through
// the same Slab.
type Slab struct {
	free [][]float64
}

// Take returns an empty (zero-length) buffer with whatever capacity a
// recycled buffer brought, or nil when none is available — either way
// the result is ready to append into.
func (s *Slab) Take() []float64 {
	if n := len(s.free); n > 0 {
		buf := s.free[n-1][:0]
		s.free[n-1] = nil
		s.free = s.free[:n-1]
		return buf
	}
	return nil
}

// Put recycles buf for a later Take. The caller must not use buf
// again after Put.
func (s *Slab) Put(buf []float64) {
	s.free = append(s.free, buf)
}
