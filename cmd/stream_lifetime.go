package cmd

import (
	"context"

	"connectrpc.com/connect"
)

// endStreamsOnShutdown returns an interceptor that ties every server-streaming
// handler to lifetime. Streams are long-lived and would otherwise keep
// http.Server.Shutdown waiting until its deadline. Unary calls are left
// untouched so they can finish during the drain window.
func endStreamsOnShutdown(lifetime context.Context) connect.Interceptor {
	return streamLifetime{lifetime: lifetime}
}

type streamLifetime struct{ lifetime context.Context }

func (streamLifetime) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc { return next }

func (streamLifetime) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (s streamLifetime) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(s.lifetime, cancel)
		defer stop()
		return next(ctx, conn)
	}
}
