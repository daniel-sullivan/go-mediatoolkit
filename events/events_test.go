package events_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/events"
)

func TestBus_PublishDeliversToSubscriber(t *testing.T) {
	bus := events.New[int]()
	var got int
	bus.Subscribe(func(n int) { got = n })

	bus.Publish(42)

	assert.Equal(t, 42, got)
}

func TestBus_MultipleSubscribersAllReceive(t *testing.T) {
	bus := events.New[string]()
	var a, b string
	bus.Subscribe(func(s string) { a = s })
	bus.Subscribe(func(s string) { b = s })

	bus.Publish("hello")

	assert.Equal(t, "hello", a)
	assert.Equal(t, "hello", b)
}

func TestBus_DeliversInRegistrationOrder(t *testing.T) {
	bus := events.New[int]()
	var order []int
	bus.Subscribe(func(int) { order = append(order, 1) })
	bus.Subscribe(func(int) { order = append(order, 2) })
	bus.Subscribe(func(int) { order = append(order, 3) })

	bus.Publish(0)

	assert.Equal(t, []int{1, 2, 3}, order)
}

func TestSubscription_CancelStopsFurtherDelivery(t *testing.T) {
	bus := events.New[int]()
	calls := 0
	sub := bus.Subscribe(func(int) { calls++ })

	bus.Publish(0)
	sub.Cancel()
	bus.Publish(0)

	assert.Equal(t, 1, calls)
}

func TestSubscription_CancelIsIdempotent(t *testing.T) {
	bus := events.New[int]()
	sub := bus.Subscribe(func(int) {})

	sub.Cancel()
	assert.NotPanics(t, func() { sub.Cancel() })
}

func TestSubscription_CancelDuringPublishPreventsOwnDelivery(t *testing.T) {
	bus := events.New[int]()
	var subB *events.Subscription[int]
	bCalled := false

	bus.Subscribe(func(int) { subB.Cancel() })
	subB = bus.Subscribe(func(int) { bCalled = true })

	bus.Publish(0)

	assert.False(t, bCalled, "subscriber cancelled mid-publish should not fire")
}

func TestBus_PanicInCallbackIsRecoveredAndLaterSubscribersStillRun(t *testing.T) {
	bus := events.New[int]()
	reached := false
	bus.Subscribe(func(int) { panic("boom") })
	bus.Subscribe(func(int) { reached = true })

	assert.NotPanics(t, func() { bus.Publish(0) })
	assert.True(t, reached)
}

func TestBus_SubscribeDuringPublishDefersToNextPublish(t *testing.T) {
	bus := events.New[int]()
	outerCalls := 0
	innerCalls := 0

	bus.Subscribe(func(int) {
		outerCalls++
		bus.Subscribe(func(int) { innerCalls++ })
	})

	bus.Publish(0)
	assert.Equal(t, 1, outerCalls)
	assert.Equal(t, 0, innerCalls, "subscriber added mid-publish must not receive the in-flight event")

	bus.Publish(0)
	assert.Equal(t, 2, outerCalls)
	assert.Equal(t, 1, innerCalls)
}

func TestBus_ConcurrentSubscribeIsSafe(t *testing.T) {
	bus := events.New[int]()
	var count atomic.Int64
	var wg sync.WaitGroup

	const n = 200
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			bus.Subscribe(func(int) { count.Add(1) })
		}()
	}
	wg.Wait()

	bus.Publish(0)
	assert.Equal(t, int64(n), count.Load())
}

func TestBus_TypedPayload(t *testing.T) {
	type deviceAdded struct {
		ID   string
		Name string
	}

	bus := events.New[deviceAdded]()
	var got deviceAdded
	bus.Subscribe(func(e deviceAdded) { got = e })

	bus.Publish(deviceAdded{ID: "usb-1", Name: "USB Headset"})

	require.Equal(t, "usb-1", got.ID)
	require.Equal(t, "USB Headset", got.Name)
}
