//go:build linux

package devices

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"sync"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// newPlatformBackend constructs the Linux backend, which talks to the
// local PulseAudio server over its native protocol socket. The
// PULSE_SERVER environment variable, if set, selects the server
// address; otherwise the default socket path is used.
func newPlatformBackend() (Backend, error) {
	return openPulseBackend("")
}

// pulseBackend implements Backend against a PulseAudio native-protocol
// connection. It is not exported; callers go through newPlatformBackend.
type pulseBackend struct {
	client *proto.Client
	conn   net.Conn

	// rawEvents carries raw SubscribeEvent values from the proto
	// readLoop into our Watch goroutine. The proto library invokes
	// Callback synchronously from its read loop, so we must not do
	// anything blocking there.
	rawEvents chan proto.SubscribeEvent
	// connClosed is closed when the proto library reports the socket
	// dropped.
	connClosed chan struct{}

	mu sync.Mutex
	// dropped is set once the proto layer reports the connection
	// closed, so watchLoop can exit without racing future callbacks.
	dropped bool
	// userClosed is set by Close so we know the socket was torn down
	// intentionally and shouldn't be reported as a drop.
	userClosed bool
	// sinkIndex / sourceIndex map pulse numeric indices to the stable
	// names we use as device IDs. Required because Remove events only
	// carry the index, not the name.
	sinkIndex   map[uint32]string
	sourceIndex map[uint32]string

	// streamClient is a second pulse connection opened lazily for
	// playback/record streams; guarded by mu. Enumeration stays on the
	// proto.Client above so the high-level library's Callback doesn't
	// clobber our subscription handler.
	streamClient *pulse.Client
}

// openPulseBackend dials the pulse server, authenticates, advertises a
// client name, and returns a ready backend. The server string follows
// the documented PulseAudio server-string syntax; empty means "use
// PULSE_SERVER or default".
func openPulseBackend(server string) (*pulseBackend, error) {
	client, conn, err := proto.Connect(server)
	if err != nil {
		return nil, err
	}

	b := &pulseBackend{
		client:      client,
		conn:        conn,
		rawEvents:   make(chan proto.SubscribeEvent, 64),
		connClosed:  make(chan struct{}),
		sinkIndex:   make(map[uint32]string),
		sourceIndex: make(map[uint32]string),
	}

	// Install the callback before anything else so we never miss the
	// ConnectionClosed notification the proto library sends on EOF.
	client.Callback = b.handleCallback

	host, _ := os.Hostname()
	if host == "" {
		host = "go-mediatoolkit"
	}
	props := proto.PropList{
		"application.name":         proto.PropListString("go-mediatoolkit"),
		"application.process.host": proto.PropListString(host),
	}
	if err := client.Request(&proto.SetClientName{Props: props}, &proto.SetClientNameReply{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return b, nil
}

// handleCallback is invoked by the proto read loop whenever an
// asynchronous message arrives. It must return quickly and never call
// back into client.Request, because that would deadlock the read loop.
func (b *pulseBackend) handleCallback(msg interface{}) {
	switch m := msg.(type) {
	case *proto.SubscribeEvent:
		// Copy by value so the channel owns the event.
		select {
		case b.rawEvents <- *m:
		default:
			// Subscriber is slow; drop. Polling fallback would catch
			// up on the next full List() anyway.
			log.Printf("devices: pulse subscribe event dropped, subscriber slow")
		}
	case *proto.ConnectionClosed:
		b.mu.Lock()
		if !b.dropped {
			b.dropped = true
			close(b.connClosed)
		}
		b.mu.Unlock()
	}
}

// List enumerates sinks and sources, tagging the system default for
// each direction. It also refreshes the internal index maps so the
// Watch loop can resolve Remove events to device IDs.
func (b *pulseBackend) List(_ context.Context) ([]Device, error) {
	var serverInfo proto.GetServerInfoReply
	if err := b.client.Request(&proto.GetServerInfo{}, &serverInfo); err != nil {
		return nil, err
	}

	var sinks proto.GetSinkInfoListReply
	if err := b.client.Request(&proto.GetSinkInfoList{}, &sinks); err != nil {
		return nil, err
	}
	var sources proto.GetSourceInfoListReply
	if err := b.client.Request(&proto.GetSourceInfoList{}, &sources); err != nil {
		return nil, err
	}

	out := make([]Device, 0, len(sinks)+len(sources))
	newSinkIdx := make(map[uint32]string, len(sinks))
	newSourceIdx := make(map[uint32]string, len(sources))

	for _, s := range sinks {
		out = append(out, deviceFromSink(s, serverInfo.DefaultSinkName))
		newSinkIdx[s.SinkIndex] = s.SinkName
	}
	for _, s := range sources {
		out = append(out, deviceFromSource(s, serverInfo.DefaultSourceName))
		newSourceIdx[s.SourceIndex] = s.SourceName
	}

	b.mu.Lock()
	b.sinkIndex = newSinkIdx
	b.sourceIndex = newSourceIdx
	b.mu.Unlock()

	return out, nil
}

// Watch subscribes to sink, source and server subscription masks and
// translates each raw pulse event into a devices.Event. The returned
// channel is closed when ctx is cancelled or when the pulse connection
// drops.
func (b *pulseBackend) Watch(ctx context.Context) (<-chan Event, error) {
	if err := b.client.Request(&proto.Subscribe{
		Mask: proto.SubscriptionMaskSink |
			proto.SubscriptionMaskSource |
			proto.SubscriptionMaskServer,
	}, nil); err != nil {
		return nil, err
	}

	out := make(chan Event, 16)
	go b.watchLoop(ctx, out)
	return out, nil
}

func (b *pulseBackend) watchLoop(ctx context.Context, out chan<- Event) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.connClosed:
			return
		case raw := <-b.rawEvents:
			evs := b.translate(raw)
			for _, ev := range evs {
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// translate turns one raw pulse event into zero or more devices.Event
// values, performing any follow-up queries needed to fill out device
// details for new or changed devices.
func (b *pulseBackend) translate(raw proto.SubscribeEvent) []Event {
	switch classifyPulseEvent(raw.Event) {
	case pulseEventSinkNewOrChange:
		return b.translateSinkUpsert(raw.Index, raw.Event.GetType() == proto.EventNew)
	case pulseEventSinkRemove:
		return b.translateSinkRemove(raw.Index)
	case pulseEventSourceNewOrChange:
		return b.translateSourceUpsert(raw.Index, raw.Event.GetType() == proto.EventNew)
	case pulseEventSourceRemove:
		return b.translateSourceRemove(raw.Index)
	case pulseEventServerChange:
		return b.translateServerChange()
	}
	return nil
}

func (b *pulseBackend) translateSinkUpsert(index uint32, isNew bool) []Event {
	info := &proto.GetSinkInfoReply{}
	if err := b.client.Request(&proto.GetSinkInfo{SinkIndex: index}, info); err != nil {
		log.Printf("devices: pulse GetSinkInfo(%d) failed: %v", index, err)
		return nil
	}
	var serverInfo proto.GetServerInfoReply
	if err := b.client.Request(&proto.GetServerInfo{}, &serverInfo); err != nil {
		log.Printf("devices: pulse GetServerInfo failed: %v", err)
	}
	dev := deviceFromSink(info, serverInfo.DefaultSinkName)

	b.mu.Lock()
	b.sinkIndex[info.SinkIndex] = info.SinkName
	b.mu.Unlock()

	kind := PropertyChanged
	if isNew {
		kind = Added
	}
	return []Event{{Kind: kind, Device: dev}}
}

func (b *pulseBackend) translateSinkRemove(index uint32) []Event {
	b.mu.Lock()
	name, ok := b.sinkIndex[index]
	delete(b.sinkIndex, index)
	b.mu.Unlock()
	if !ok {
		return nil
	}
	return []Event{{Kind: Removed, Device: Device{ID: name, Direction: Output}}}
}

func (b *pulseBackend) translateSourceUpsert(index uint32, isNew bool) []Event {
	info := &proto.GetSourceInfoReply{}
	if err := b.client.Request(&proto.GetSourceInfo{SourceIndex: index}, info); err != nil {
		log.Printf("devices: pulse GetSourceInfo(%d) failed: %v", index, err)
		return nil
	}
	var serverInfo proto.GetServerInfoReply
	if err := b.client.Request(&proto.GetServerInfo{}, &serverInfo); err != nil {
		log.Printf("devices: pulse GetServerInfo failed: %v", err)
	}
	dev := deviceFromSource(info, serverInfo.DefaultSourceName)

	b.mu.Lock()
	b.sourceIndex[info.SourceIndex] = info.SourceName
	b.mu.Unlock()

	kind := PropertyChanged
	if isNew {
		kind = Added
	}
	return []Event{{Kind: kind, Device: dev}}
}

func (b *pulseBackend) translateSourceRemove(index uint32) []Event {
	b.mu.Lock()
	name, ok := b.sourceIndex[index]
	delete(b.sourceIndex, index)
	b.mu.Unlock()
	if !ok {
		return nil
	}
	return []Event{{Kind: Removed, Device: Device{ID: name, Direction: Input}}}
}

// translateServerChange emits DefaultChanged events for whichever of
// the sink/source defaults actually moved. We re-query server info and
// the specific sink/source record to materialise a complete Device.
func (b *pulseBackend) translateServerChange() []Event {
	var serverInfo proto.GetServerInfoReply
	if err := b.client.Request(&proto.GetServerInfo{}, &serverInfo); err != nil {
		log.Printf("devices: pulse GetServerInfo failed: %v", err)
		return nil
	}
	var out []Event
	if serverInfo.DefaultSinkName != "" {
		info := &proto.GetSinkInfoReply{SinkName: serverInfo.DefaultSinkName}
		req := &proto.GetSinkInfo{SinkIndex: proto.Undefined, SinkName: serverInfo.DefaultSinkName}
		if err := b.client.Request(req, info); err == nil {
			out = append(out, Event{Kind: DefaultChanged, Device: deviceFromSink(info, serverInfo.DefaultSinkName)})
		} else {
			log.Printf("devices: pulse GetSinkInfo(%s) failed: %v", serverInfo.DefaultSinkName, err)
		}
	}
	if serverInfo.DefaultSourceName != "" {
		info := &proto.GetSourceInfoReply{SourceName: serverInfo.DefaultSourceName}
		req := &proto.GetSourceInfo{SourceIndex: proto.Undefined, SourceName: serverInfo.DefaultSourceName}
		if err := b.client.Request(req, info); err == nil {
			out = append(out, Event{Kind: DefaultChanged, Device: deviceFromSource(info, serverInfo.DefaultSourceName)})
		} else {
			log.Printf("devices: pulse GetSourceInfo(%s) failed: %v", serverInfo.DefaultSourceName, err)
		}
	}
	return out
}

// Close shuts down the pulse connection. Calling Close concurrently
// with an in-flight Watch goroutine is safe: the readLoop inside the
// proto library will error out and fire ConnectionClosed, which the
// watchLoop observes via connClosed.
func (b *pulseBackend) Close() error {
	b.mu.Lock()
	if b.userClosed {
		b.mu.Unlock()
		return nil
	}
	b.userClosed = true
	// Only close connClosed if the proto layer hasn't already signalled
	// a drop; handleCallback owns the other side of that race.
	if !b.dropped {
		b.dropped = true
		close(b.connClosed)
	}
	b.mu.Unlock()

	b.closeStreamClient()

	err := b.conn.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
