package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestShutdownEndsStreamsPromptlyAndDrainsUnary checks that an open server
// stream does not hold up http.Server.Shutdown, while a unary request that is
// still in flight completes.
func TestShutdownEndsStreamsPromptlyAndDrainsUnary(t *testing.T) {
	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()

	opt := connect.WithInterceptors(endStreamsOnShutdown(srvCtx))
	streamStarted := make(chan struct{})
	unaryStarted := make(chan struct{})
	releaseUnary := make(chan struct{})

	mux := http.NewServeMux()
	mux.Handle("/t.S/Stream", connect.NewServerStreamHandler("/t.S/Stream",
		func(ctx context.Context, _ *connect.Request[emptypb.Empty], st *connect.ServerStream[emptypb.Empty]) error {
			if err := st.Send(&emptypb.Empty{}); err != nil {
				return err
			}
			close(streamStarted)
			<-ctx.Done()
			return ctx.Err()
		}, opt))
	mux.Handle("/t.S/Unary", connect.NewUnaryHandler("/t.S/Unary",
		func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			close(unaryStarted)
			<-releaseUnary
			return connect.NewResponse(&emptypb.Empty{}), nil
		}, opt))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	streamClient := connect.NewClient[emptypb.Empty, emptypb.Empty](ts.Client(), ts.URL+"/t.S/Stream", connect.WithGRPC())
	stream, err := streamClient.CallServerStream(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	<-streamStarted

	unaryClient := connect.NewClient[emptypb.Empty, emptypb.Empty](ts.Client(), ts.URL+"/t.S/Unary")
	unaryDone := make(chan error, 1)
	go func() {
		_, err := unaryClient.CallUnary(context.Background(), connect.NewRequest(&emptypb.Empty{}))
		unaryDone <- err
	}()
	<-unaryStarted

	// Mirror the serve.go order: cancel the lifetime, then Shutdown.
	srvCancel()
	time.AfterFunc(300*time.Millisecond, func() { close(releaseUnary) })

	start := time.Now()
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ts.Config.Shutdown(shCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Shutdown took %v, want well under the 10s drain timeout", d)
	}
	if err := <-unaryDone; err != nil {
		t.Errorf("in-flight unary call was cut off: %v", err)
	}
	for stream.Receive() {
	}
	if stream.Err() == nil {
		t.Error("stream ended without an error; expected the server to cancel it")
	}
}
