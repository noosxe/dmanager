package container

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadBoundedLine(t *testing.T) {
	br := bufio.NewReaderSize(strings.NewReader("short\r\n"+strings.Repeat("a", 300)+"\nlast"), 16)

	got, err := readBoundedLine(br, 100)
	if err != nil || got != "short" {
		t.Fatalf("first line = %q, %v", got, err)
	}
	got, err = readBoundedLine(br, 100)
	if err != nil || got != strings.Repeat("a", 100)+"… [truncated 200 bytes]" {
		t.Fatalf("second line = %q, %v", got, err)
	}
	got, err = readBoundedLine(br, 100)
	if err != nil || got != "last" {
		t.Fatalf("unterminated last line = %q, %v", got, err)
	}
	if _, err = readBoundedLine(br, 100); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestReadBoundedLineKeepsUTF8Valid(t *testing.T) {
	// "é" is two bytes; a cap of 3 cuts the second one in half.
	br := bufio.NewReader(strings.NewReader("éé\n"))
	got, err := readBoundedLine(br, 3)
	if err != nil || got != "é… [truncated 1 bytes]" {
		t.Fatalf("got %q, %v", got, err)
	}
}
