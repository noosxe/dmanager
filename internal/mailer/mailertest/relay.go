// Package mailertest provides a minimal in-process SMTP relay for tests.
// It records the envelope and DATA payload of each delivery and can require
// AUTH PLAIN, letting tests assert exactly what a mail client handed over.
package mailertest

import (
	"bufio"
	"encoding/base64"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Relay is a fake SMTP server listening on a loopback port.
type Relay struct {
	ln net.Listener

	mu        sync.Mutex
	mailFrom  string
	rcptTo    []string
	data      string
	authed    bool

	requireAuth bool
	expectUser  string
	expectPass  string
}

// New starts the relay and registers cleanup with t.
func New(t *testing.T) *Relay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	r := &Relay{ln: ln}
	go r.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return r
}

// RequireAuth makes the relay advertise AUTH PLAIN, enforce it on MAIL FROM,
// and accept exactly the given credentials.
func (r *Relay) RequireAuth(user, pass string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requireAuth = true
	r.expectUser = user
	r.expectPass = pass
}

// Port is the loopback TCP port the relay listens on.
func (r *Relay) Port() string {
	return strconv.Itoa(r.ln.Addr().(*net.TCPAddr).Port)
}

// LastMessage returns the envelope sender, recipients and raw DATA payload
// of the most recent delivery.
func (r *Relay) LastMessage() (mailFrom string, rcptTo []string, data string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mailFrom, append([]string(nil), r.rcptTo...), r.data
}

// AuthSucceeded reports whether a client completed AUTH PLAIN with the
// expected credentials.
func (r *Relay) AuthSucceeded() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.authed
}

func (r *Relay) accept() {
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			return
		}
		go r.serve(conn)
	}
}

func (r *Relay) serve(conn net.Conn) {
	reader := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(s string) {
		_, _ = io.WriteString(w, s+"\r\n")
		_ = w.Flush()
	}

	writeLine("220 fake.relay ESMTP")
	for {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.Fields(line + " ")[0])

		switch verb {
		case "EHLO", "HELO":
			writeLine("250-fake.relay")
			r.mu.Lock()
			requireAuth := r.requireAuth
			r.mu.Unlock()
			if requireAuth {
				writeLine("250 AUTH PLAIN")
			} else {
				writeLine("250 8BITMIME")
			}
		case "AUTH":
			r.mu.Lock()
			requireAuth := r.requireAuth
			expectUser, expectPass := r.expectUser, r.expectPass
			r.mu.Unlock()
			if !requireAuth {
				writeLine("502 auth not offered")
				continue
			}
			// AUTH PLAIN <base64(authzid NUL authcid NUL passwd)>
			parts := strings.Fields(line)
			if len(parts) < 3 || !strings.EqualFold(parts[1], "PLAIN") {
				writeLine("535 auth failed")
				continue
			}
			raw, decErr := base64.StdEncoding.DecodeString(parts[2])
			if decErr != nil {
				writeLine("535 auth failed")
				continue
			}
			fields := strings.Split(string(raw), "\x00")
			r.mu.Lock()
			if len(fields) == 3 && fields[1] == expectUser && fields[2] == expectPass {
				r.authed = true
				writeLine("235 ok")
			} else {
				writeLine("535 auth failed")
			}
			r.mu.Unlock()
		case "MAIL":
			r.mu.Lock()
			authed := r.authed
			r.mu.Unlock()
			if requireAuthLocked(r) && !authed {
				writeLine("530 authentication required")
				continue
			}
			r.mu.Lock()
			r.mailFrom = between(line)
			r.rcptTo = nil
			r.mu.Unlock()
			writeLine("250 ok")
		case "RCPT":
			if addr := between(line); addr != "" {
				r.mu.Lock()
				r.rcptTo = append(r.rcptTo, addr)
				r.mu.Unlock()
			}
			writeLine("250 ok")
		case "DATA":
			writeLine("354 go ahead")
			var sb strings.Builder
			for {
				dl, derr := reader.ReadString('\n')
				if derr != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
				sb.WriteString(dl)
			}
			r.mu.Lock()
			r.data = sb.String()
			r.mu.Unlock()
			writeLine("250 queued")
		case "NOOP", "RSET":
			writeLine("250 ok")
		case "QUIT":
			writeLine("221 bye")
			return
		default:
			writeLine("502 not implemented")
		}
	}
}

func requireAuthLocked(r *Relay) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requireAuth
}

// between extracts the <addr> from a MAIL FROM/RCPT TO line.
func between(line string) string {
	i := strings.Index(line, "<")
	j := strings.LastIndex(line, ">")
	if i == -1 || j == -1 || j < i {
		return ""
	}
	return line[i+1 : j]
}
