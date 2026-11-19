package container

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"

	"dmanager/internal/auth"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const (
	roleViewer       = "viewer"
	streamTypeStdout = "stdout"
	streamTypeStderr = "stderr"
)

// maxLogLineBytes caps how much of a single raw (TTY) log line is kept in
// memory and sent to the client. The rest of an overlong line is discarded and
// replaced by a marker, so one huge line neither ends the stream nor grows
// memory without bound.
const maxLogLineBytes = 1 << 20

// readBoundedLine returns the next line (without its newline), keeping at most
// max bytes. Bytes past the cap are consumed and dropped, and the returned
// line ends with a "… [truncated N bytes]" marker. It returns io.EOF only when
// no more data is available.
func readBoundedLine(br *bufio.Reader, max int) (string, error) {
	var buf []byte
	dropped := 0
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			if len(buf) > 0 || dropped > 0 {
				// Partial line at the end of a failed read: surface the error
				// on the next call rather than lose what was read.
				break
			}
			return "", err
		}
		if room := max - len(buf); room >= len(chunk) {
			buf = append(buf, chunk...)
		} else {
			if room > 0 {
				buf = append(buf, chunk[:room]...)
				chunk = chunk[room:]
			}
			dropped += len(chunk)
		}
		if !isPrefix {
			break
		}
	}
	line := string(buf)
	if dropped > 0 {
		// The cut may split a multi-byte rune; protobuf strings must be UTF-8.
		line = strings.ToValidUTF8(line, "") + fmt.Sprintf("… [truncated %d bytes]", dropped)
	}
	return line, nil
}

// GetContainerLogs streams live console outputs (stdout/stderr) from a container.
func (s *Service) GetContainerLogs(
	ctx context.Context,
	req *connect.Request[v1.GetContainerLogsRequest],
	stream *connect.ServerStream[v1.GetContainerLogsResponse],
) error {
	// 1. Verify authorization rules (Authenticated, at least viewer)
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}
	if user.Role != roleAdmin && user.Role != roleViewer {
		return connect.NewError(connect.CodePermissionDenied, errors.New("unauthorized role"))
	}

	containerID := req.Msg.Id
	if containerID == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("container ID is required"))
	}

	// 2. Check if the container exists in database
	queries := db.New(s.db)
	_, err := queries.GetContainer(ctx, containerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return connect.NewError(connect.CodeNotFound, errors.New("container not found"))
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query container from DB: %w", err))
	}

	// 3. Inspect container state on Docker host to check TTY config
	if s.dockerClient == nil {
		return connect.NewError(connect.CodeInternal, errors.New("docker client not initialized"))
	}

	inspect, err := s.dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return connect.NewError(connect.CodeNotFound, errors.New("container not found on Docker host"))
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to inspect container: %w", err))
	}

	// 4. Resolve log tail options
	tail := "100"
	if req.Msg.TailLines > 0 {
		tail = strconv.Itoa(int(req.Msg.TailLines))
	} else if req.Msg.TailLines < 0 {
		tail = "all"
	}

	options := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     req.Msg.Follow,
		Tail:       tail,
	}

	reader, err := s.dockerClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get container logs: %w", err))
	}
	defer func() {
		_ = reader.Close()
	}()

	// 5. Stream logs to the client based on TTY configuration
	if inspect.Container.Config.Tty {
		// Raw stream - read line by line, bounded per line.
		br := bufio.NewReaderSize(reader, 64*1024)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			line, err := readBoundedLine(br, maxLogLineBytes)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return connect.NewError(connect.CodeInternal, fmt.Errorf("error reading raw logs stream: %w", err))
			}

			timestamp := ""
			logLine := line

			if idx := strings.Index(line, " "); idx != -1 {
				potentialTS := line[:idx]
				if _, tErr := time.Parse(time.RFC3339Nano, potentialTS); tErr == nil {
					timestamp = potentialTS
					logLine = line[idx+1:]
				}
			}

			resp := &v1.GetContainerLogsResponse{
				LogLine:    logLine,
				Timestamp:  timestamp,
				StreamType: streamTypeStdout, // TTY only has a single stdout stream
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	} else {
		// Multiplexed stream - read frame by frame
		header := make([]byte, 8)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			_, err := io.ReadFull(reader, header)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) {
					break
				}
				return connect.NewError(connect.CodeInternal, fmt.Errorf("error reading logs stream header: %w", err))
			}

			streamTypeVal := header[0]
			frameSize := binary.BigEndian.Uint32(header[4:8])

			payload := make([]byte, frameSize)
			_, err = io.ReadFull(reader, payload)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) {
					break
				}
				return connect.NewError(connect.CodeInternal, fmt.Errorf("error reading logs stream payload: %w", err))
			}

			lines := strings.Split(string(payload), "\n")
			for i, line := range lines {
				if i == len(lines)-1 && line == "" {
					break
				}
				line = strings.TrimSuffix(line, "\r")

				timestamp := ""
				logLine := line
				if idx := strings.Index(line, " "); idx != -1 {
					potentialTS := line[:idx]
					if _, tErr := time.Parse(time.RFC3339Nano, potentialTS); tErr == nil {
						timestamp = potentialTS
						logLine = line[idx+1:]
					}
				}

				streamType := streamTypeStdout
				if streamTypeVal == 2 {
					streamType = streamTypeStderr
				}

				resp := &v1.GetContainerLogsResponse{
					LogLine:    logLine,
					Timestamp:  timestamp,
					StreamType: streamType,
				}
				if err := stream.Send(resp); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
