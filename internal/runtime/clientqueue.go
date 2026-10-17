package runtime

import (
	"sync"
	"time"

	"keel/internal/inject"
)

// Each attached client gets its events through a queue measured in bytes,
// not events. A burst of small reads (yes | head over SSH) used to fill a
// 512-event channel in a few KB and drop a client that was only a moment
// behind, closing its WebSocket. Now output waits in the queue, adjacent
// chunks are merged so the consumer sends fewer, larger frames, and a
// client is dropped only when its unread output passes the byte budget.
const (
	// clientMaxQueuedBytes is how much unread output a client may hold
	// before it is dropped as a slow consumer (as the share hub's
	// maxBuffered caps a viewer).
	clientMaxQueuedBytes = 4 << 20
	// clientMaxQueuedEvents bounds the non-output events a stalled client
	// can pile up (presence, status, control).
	clientMaxQueuedEvents = 4096
	// clientMergeBytes caps one merged output event at the size of one PTY
	// read (runtime reads 32 KB), so no consumer sees a larger message than
	// before: a share viewer's WebRTC data channel has a message size limit.
	clientMergeBytes = 32 << 10
	// clientChanCap is the hand-off channel between the queue and the
	// consumer; the queue does the buffering.
	clientChanCap = 16
	// clientFinishWait is how long a closed session's last events wait for
	// a consumer before the queue gives up on it.
	clientFinishWait = 5 * time.Second
)

type queuedEvent struct {
	ev Event
	// owned is set once ev.Bytes is a buffer of this queue's own. Chunks
	// arrive shared between clients and are copied before the first merge.
	owned bool
}

type clientConn struct {
	Client  Client
	ch      chan Event
	focused inject.Surface

	qmu      sync.Mutex
	queue    []queuedEvent
	queued   int // bytes of output in queue and in flight to ch
	closing  bool
	wake     chan struct{}
	done     chan struct{}
	doneOnce sync.Once
	fin      chan struct{}
	finOnce  sync.Once
}

func newClientConn(cl Client, focused inject.Surface) *clientConn {
	c := &clientConn{
		Client:  cl,
		ch:      make(chan Event, clientChanCap),
		focused: focused,
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		fin:     make(chan struct{}),
	}
	go c.pump()
	return c
}

// offer queues ev without blocking. It reports false when the client is
// over its budget and should be dropped.
func (c *clientConn) offer(ev Event) bool {
	c.qmu.Lock()
	if c.closing {
		c.qmu.Unlock()
		return true
	}
	if ev.Kind == "bytes" {
		if c.queued+len(ev.Bytes) > clientMaxQueuedBytes {
			c.qmu.Unlock()
			return false
		}
		c.queued += len(ev.Bytes)
		if n := len(c.queue); n > 0 && c.queue[n-1].ev.Kind == "bytes" &&
			len(c.queue[n-1].ev.Bytes)+len(ev.Bytes) <= clientMergeBytes {
			last := &c.queue[n-1]
			if !last.owned {
				buf := make([]byte, len(last.ev.Bytes), max(2*(len(last.ev.Bytes)+len(ev.Bytes)), 4<<10))
				copy(buf, last.ev.Bytes)
				last.ev.Bytes = buf
				last.owned = true
			}
			last.ev.Bytes = append(last.ev.Bytes, ev.Bytes...)
			c.qmu.Unlock()
			c.signal()
			return true
		}
	} else if len(c.queue) >= clientMaxQueuedEvents {
		c.qmu.Unlock()
		return false
	}
	c.queue = append(c.queue, queuedEvent{ev: ev})
	c.qmu.Unlock()
	c.signal()
	return true
}

func (c *clientConn) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// finish stops new events and closes ch once what is queued is delivered:
// the session ended and its last status events still reach the client.
func (c *clientConn) finish() {
	c.qmu.Lock()
	c.closing = true
	c.qmu.Unlock()
	c.finOnce.Do(func() { close(c.fin) })
	c.signal()
}

// stop drops the client now: nothing more is delivered and ch closes.
func (c *clientConn) stop() {
	c.qmu.Lock()
	c.closing = true
	c.queue = nil
	c.queued = 0
	c.qmu.Unlock()
	c.doneOnce.Do(func() { close(c.done) })
}

// send hands ev to the consumer. After finish the session is gone and a
// consumer that already quit never unsubscribes, so a send waits at most
// clientFinishWait rather than leak the pump.
func (c *clientConn) send(ev Event) bool {
	select {
	case c.ch <- ev:
		return true
	case <-c.done:
		return false
	case <-c.fin:
	}
	t := time.NewTimer(clientFinishWait)
	defer t.Stop()
	select {
	case c.ch <- ev:
		return true
	case <-c.done:
		return false
	case <-t.C:
		return false
	}
}

// pump moves queued events to ch in order, blocking on the consumer but
// never on the session: offer only appends under qmu.
func (c *clientConn) pump() {
	defer close(c.ch)
	for {
		select {
		case <-c.wake:
		case <-c.done:
			return
		}
		for {
			c.qmu.Lock()
			batch := c.queue
			c.queue = nil
			closing := c.closing
			c.qmu.Unlock()
			if len(batch) == 0 {
				if closing {
					return
				}
				break
			}
			for _, q := range batch {
				if !c.send(q.ev) {
					return
				}
				if q.ev.Kind == "bytes" {
					c.qmu.Lock()
					c.queued = max(0, c.queued-len(q.ev.Bytes))
					c.qmu.Unlock()
				}
			}
		}
	}
}
