package timeline

import (
	"sync"
	"sync/atomic"
)

// handleImpl is the shared Handle implementation used by both
// Timeline cues and LoopingTimeline templates. Mixer tracks
// have their own handle type (mixer.trackHandle) because it carries
// per-track controls such as gain.
type handleImpl struct {
	id        uint64
	done      chan struct{}
	cancelled atomic.Bool
	closeOnce sync.Once
}

func newHandle(id uint64) *handleImpl {
	return &handleImpl{id: id, done: make(chan struct{})}
}

func (h *handleImpl) ID() uint64            { return h.id }
func (h *handleImpl) Done() <-chan struct{} { return h.done }

func (h *handleImpl) Cancel() {
	if h.cancelled.CompareAndSwap(false, true) {
		h.closeOnce.Do(func() { close(h.done) })
	}
}

// finish closes Done without flipping cancelled — used when a cue
// completes naturally (EOF) or the timeline is closed.
func (h *handleImpl) finish() {
	h.closeOnce.Do(func() { close(h.done) })
}
