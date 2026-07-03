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

// Unsubscribe unregisters a channel from receiving updates.
func (b *Broker) Unsubscribe(ch chan *dmanagerv1.StreamContainersResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, ch)
	close(ch)
}

// Publish broadcasts an update to all active subscribers.
func (b *Broker) Publish(event *dmanagerv1.StreamContainersResponse) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber is slow, skip to prevent blocking execution
		}
	}
}
