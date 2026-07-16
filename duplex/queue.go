package duplex

// fifo is the head-indexed queue both the render jitter buffer and
// the capture queue are built on: push at the tail, pop at the head,
// with periodic re-anchoring so the dead prefix left by pops cannot
// grow the backing array without bound.
type fifo[T any] struct {
	items []T
	head  int
}

// push appends v at the tail.
func (q *fifo[T]) push(v T) {
	q.items = append(q.items, v)
}

// len reports the queued item count.
func (q *fifo[T]) len() int {
	return len(q.items) - q.head
}

// pop removes and returns the head item, zeroing its slot so popped
// payloads are not retained. Callers must have checked len() > 0.
func (q *fifo[T]) pop() T {
	v := q.items[q.head]
	var zero T
	q.items[q.head] = zero
	q.head++
	q.compact()
	return v
}

// reset discards every queued item, keeping the backing array.
func (q *fifo[T]) reset() {
	clear(q.items)
	q.items = q.items[:0]
	q.head = 0
}

// compact re-anchors the queue at index 0 once the dead prefix
// dominates the slice.
func (q *fifo[T]) compact() {
	if q.head > 0 && q.head >= len(q.items)/2 {
		n := copy(q.items, q.items[q.head:])
		clear(q.items[n:])
		q.items = q.items[:n]
		q.head = 0
	}
}

// slab recycles sample buffers across a queue's push/pop cycles so a
// steady stream stops allocating once warm.
type slab struct {
	free [][]float64
}

// take returns an empty buffer, recycled when one is available.
func (s *slab) take() []float64 {
	if n := len(s.free); n > 0 {
		buf := s.free[n-1][:0]
		s.free[n-1] = nil
		s.free = s.free[:n-1]
		return buf
	}
	return nil
}

// put recycles buf for a later take.
func (s *slab) put(buf []float64) {
	s.free = append(s.free, buf)
}
