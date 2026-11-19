package container

import (
	"sync"

	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
)

// Broker handles active client subscriptions and broadcasts container updates.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[chan *dmanagerv1.StreamContainersResponse]struct{}
}

// NewBroker creates and initializes a new Broker.
func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[chan *dmanagerv1.StreamContainersResponse]struct{}),
	}
}

// Subscribe registers a new channel to receive updates.
func (b *Broker) Subscribe() chan *dmanagerv1.StreamContainersResponse {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan *dmanagerv1.StreamContainersResponse, 100)
	b.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe unregisters a channel from receiving updates and closes it.
// It is safe to call for a channel the broker has already closed.
func (b *Broker) Unsubscribe(ch chan *dmanagerv1.StreamContainersResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remove(ch)
}

// CloseAll ends every active subscription so connected clients reconnect and
// refetch their state, e.g. after the database was resynced with Docker.
func (b *Broker) CloseAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		b.remove(ch)
	}
}

// remove closes ch and forgets it. Callers must hold b.mu for writing.
func (b *Broker) remove(ch chan *dmanagerv1.StreamContainersResponse) {
	if _, ok := b.subscribers[ch]; !ok {
		return
	}
	delete(b.subscribers, ch)
	close(ch)
}

// Publish broadcasts an update to all active subscribers. A subscriber whose
// buffer is full would silently miss the event, so its channel is closed
// instead: its stream ends and the client reconnects and refetches.
func (b *Broker) Publish(event *dmanagerv1.StreamContainersResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			b.remove(ch)
		}
	}
}
