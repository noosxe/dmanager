package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"

	"dmanager/internal/config"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

type fakeStreamConn struct {
	connect.StreamingHandlerConn
	reqHeader  http.Header
	respHeader http.Header
	procedure  string
}

func (c *fakeStreamConn) Spec() connect.Spec           { return connect.Spec{Procedure: c.procedure} }
func (c *fakeStreamConn) RequestHeader() http.Header   { return c.reqHeader }
func (c *fakeStreamConn) ResponseHeader() http.Header  { return c.respHeader }
func (c *fakeStreamConn) ResponseTrailer() http.Header { return http.Header{} }
func (c *fakeStreamConn) Peer() connect.Peer           { return connect.Peer{} }
func (c *fakeStreamConn) Send(any) error               { return nil }
func (c *fakeStreamConn) Receive(any) error            { return io.EOF }

func maxAgeOf(t *testing.T, cookie string) int {
	t.Helper()
	for _, part := range strings.Split(cookie, "; ") {
		if v, ok := strings.CutPrefix(part, "Max-Age="); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatalf("bad Max-Age in %q", cookie)
			}
			return n
		}
	}
	t.Fatalf("no Max-Age in %q", cookie)
	return 0
}

func TestInterceptorRefreshesSessionCookie(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()
	user, err := queries.CreateUser(ctx, db.CreateUserParams{Username: testUsername, PasswordHash: dummyHashValue, Role: viewerRole})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	const remember = 30 * 24 * time.Hour
	interceptor := NewInterceptor(queries, slog.New(slog.NewTextHandler(io.Discard, nil)), 7*24*time.Hour, nil).
		WithSecureCookies(config.SecureCookiesAlways)

	now := time.Now()
	seed := func(id string, idle time.Duration, expires, abs time.Time) {
		createSlideSession(t, queries, user.ID, id, int64(idle.Seconds()), expires.Add(-idle), expires, abs)
	}
	seed("cookie-slide", remember, now.Add(time.Hour), now.Add(365*24*time.Hour))
	seed("cookie-fresh", remember, now.Add(25*24*time.Hour), now.Add(365*24*time.Hour))
	seed("cookie-capped", remember, now.Add(time.Hour), now.Add(2*24*time.Hour))

	unary := func(id string, handler connect.UnaryFunc) (connect.AnyResponse, error) {
		req := &customProcedureRequest{
			AnyRequest: connect.NewRequest(&v1.ListContainersRequest{}),
			procedure:  dmanagerv1connect.ContainerServiceListContainersProcedure,
		}
		req.Header().Set("Cookie", "session_id="+id)
		return interceptor.WrapUnary(handler)(ctx, req)
	}
	ok := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&v1.ListContainersResponse{}), nil
	}

	t.Run("unary re-sends the cookie with the new lifetime and login attributes", func(t *testing.T) {
		resp, err := unary("cookie-slide", ok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cookies := resp.Header().Values("Set-Cookie")
		if len(cookies) != 1 {
			t.Fatalf("expected one Set-Cookie, got %v", cookies)
		}
		c := cookies[0]
		for _, attr := range []string{"session_id=cookie-slide", "Path=/", "HttpOnly", "SameSite=Lax", "Secure"} {
			if !strings.Contains(c, attr) {
				t.Errorf("cookie %q missing %q", c, attr)
			}
		}
		want := int(remember.Seconds())
		if got := maxAgeOf(t, c); got < want-5 || got > want {
			t.Errorf("Max-Age = %d, want about %d", got, want)
		}
	})

	t.Run("no cookie when the session did not slide", func(t *testing.T) {
		resp, err := unary("cookie-fresh", ok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := resp.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Errorf("expected no Set-Cookie, got %v", got)
		}
	})

	t.Run("cookie lifetime is clamped to the absolute expiry", func(t *testing.T) {
		resp, err := unary("cookie-capped", ok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := maxAgeOf(t, resp.Header().Get("Set-Cookie"))
		want := int((2 * 24 * time.Hour).Seconds())
		if got < want-5 || got > want {
			t.Errorf("Max-Age = %d, want about %d (absolute cap)", got, want)
		}
	})

	t.Run("a failing handler still carries the refreshed cookie in error metadata", func(t *testing.T) {
		seed("cookie-err", remember, now.Add(time.Hour), now.Add(365*24*time.Hour))
		_, err := unary("cookie-err", func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("boom"))
		})
		var cerr *connect.Error
		if !errors.As(err, &cerr) || !strings.Contains(cerr.Meta().Get("Set-Cookie"), "session_id=cookie-err") {
			t.Errorf("expected refreshed cookie in error metadata, got %v", err)
		}
	})

	t.Run("a cookie written by the handler is never overridden", func(t *testing.T) {
		seed("cookie-own", remember, now.Add(time.Hour), now.Add(365*24*time.Hour))
		resp, err := unary("cookie-own", func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			r := connect.NewResponse(&v1.ListContainersResponse{})
			r.Header().Set("Set-Cookie", "session_id=; Max-Age=0")
			return r, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := resp.Header().Values("Set-Cookie"); len(got) != 1 || !strings.Contains(got[0], "Max-Age=0") {
			t.Errorf("handler cookie must win, got %v", got)
		}
	})

	t.Run("streaming sets the cookie before the handler runs", func(t *testing.T) {
		seed("cookie-stream", remember, now.Add(time.Hour), now.Add(365*24*time.Hour))
		conn := &fakeStreamConn{
			reqHeader:  http.Header{"Cookie": []string{"session_id=cookie-stream"}},
			respHeader: http.Header{},
			procedure:  dmanagerv1connect.ContainerServiceStreamContainersProcedure,
		}
		var seenInHandler string
		err := interceptor.WrapStreamingHandler(func(_ context.Context, c connect.StreamingHandlerConn) error {
			seenInHandler = c.ResponseHeader().Get("Set-Cookie")
			return nil
		})(ctx, conn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(seenInHandler, "session_id=cookie-stream") || maxAgeOf(t, seenInHandler) < int(remember.Seconds())-5 {
			t.Errorf("expected refreshed cookie before the handler ran, got %q", seenInHandler)
		}
	})

	t.Run("streaming without a slide sends no cookie", func(t *testing.T) {
		conn := &fakeStreamConn{
			reqHeader:  http.Header{"Cookie": []string{"session_id=cookie-fresh"}},
			respHeader: http.Header{},
			procedure:  dmanagerv1connect.ContainerServiceStreamContainersProcedure,
		}
		if err := interceptor.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error { return nil })(ctx, conn); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := conn.respHeader.Values("Set-Cookie"); len(got) != 0 {
			t.Errorf("expected no Set-Cookie, got %v", got)
		}
	})
}
