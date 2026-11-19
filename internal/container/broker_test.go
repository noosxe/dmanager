package container

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	connect "connectrpc.com/connect"

	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

func isClosed(ch chan *dmanagerv1.StreamContainersResponse) bool {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
		default:
			return false
		}
	}
}

func TestBrokerPublishClosesSlowSubscriber(t *testing.T) {
	b := NewBroker()
	slow := b.Subscribe()
	fast := b.Subscribe()
	ev := &dmanagerv1.StreamContainersResponse{Action: "save"}

	for i := 0; i < cap(slow); i++ {
		b.Publish(ev)
		<-fast // keep the fast subscriber drained
	}
	if len(slow) != cap(slow) {
		t.Fatalf("expected a full buffer, got %d/%d", len(slow), cap(slow))
	}

	b.Publish(ev) // overflows the slow subscriber
	if !isClosed(slow) {
		t.Fatal("slow subscriber was not closed on overflow")
	}
	select {
	case <-fast:
	default:
		t.Fatal("fast subscriber missed the event")
	}

	// Unsubscribing an already closed channel must not panic.
	b.Unsubscribe(slow)
	b.Unsubscribe(fast)
}

func TestBrokerCloseAll(t *testing.T) {
	b := NewBroker()
	a, c := b.Subscribe(), b.Subscribe()

	b.CloseAll()
	if !isClosed(a) || !isClosed(c) {
		t.Fatal("CloseAll did not close every subscriber")
	}
	b.Unsubscribe(a) // must not panic

	// New subscribers still work afterwards.
	d := b.Subscribe()
	b.Publish(&dmanagerv1.StreamContainersResponse{Action: "save"})
	select {
	case <-d:
	default:
		t.Fatal("subscriber after CloseAll did not receive events")
	}
}

func TestStreamContainersEndsWhenSubscriberClosed(t *testing.T) {
	dbConn, _ := newTestDB(t)
	broker := NewBroker()
	svc := NewService(dbConn, broker, nil, slog.Default(), nil, nil)

	mux := http.NewServeMux()
	path, handler := dmanagerv1connect.NewContainerServiceHandler(svc)
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := dmanagerv1connect.NewContainerServiceClient(server.Client(), server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The call blocks until response headers arrive, which for an idle
	// stream is when it ends, so run it in the background.
	type result struct {
		msgs int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		stream, err := client.StreamContainers(ctx, connect.NewRequest(&dmanagerv1.StreamContainersRequest{}))
		if err != nil {
			done <- result{err: err}
			return
		}
		defer func() { _ = stream.Close() }()
		n := 0
		for stream.Receive() {
			n++
		}
		done <- result{msgs: n, err: stream.Err()}
	}()

	// Wait until the handler has subscribed, then end all streams.
	deadline := time.Now().Add(2 * time.Second)
	for {
		broker.mu.RLock()
		n := len(broker.subscribers)
		broker.mu.RUnlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handler never subscribed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	broker.CloseAll()

	res := <-done
	if res.err != nil || res.msgs != 0 {
		t.Fatalf("stream should end cleanly without messages, got msgs=%d err=%v", res.msgs, res.err)
	}
}
