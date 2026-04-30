package cli

import (
	"bufio"
	"errors"
	"io"
	"os"
	"testing"
)

func TestLooksLikeRoomCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"@foo", false},
		{"@p2p-abc", false},
		{"p2p-abc", true},
		{"  p2p-abc  ", true},
		{"direct-host:123", true},
		{"relay-xyz", true},
		{"guaranteed-id", true},
		{"unknown-thing", false},
		{"foo-bar", false},
		{"p2p-", true},
		{"not-a-code", false},
	}
	for _, tc := range cases {
		if got := looksLikeRoomCode(tc.in); got != tc.want {
			t.Errorf("looksLikeRoomCode(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestReadLineTrim_EOFEmpty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	origStdin := os.Stdin
	defer func() {
		os.Stdin = origStdin
		stdin = bufio.NewReader(os.Stdin)
	}()
	os.Stdin = r
	stdin = bufio.NewReader(os.Stdin)

	_, err = readLineTrim("")
	if !errors.Is(err, io.EOF) {
		t.Fatalf("readLineTrim: err = %v, want EOF", err)
	}
}

func TestReadLineTrim_EOFPartialLine(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	origStdin := os.Stdin
	defer func() {
		os.Stdin = origStdin
		stdin = bufio.NewReader(os.Stdin)
	}()
	os.Stdin = r
	stdin = bufio.NewReader(os.Stdin)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = w.WriteString("no newline")
		_ = w.Close()
	}()

	s, err := readLineTrim("")
	<-done
	if err != nil {
		t.Fatalf("readLineTrim: err = %v, want nil (partial line before EOF)", err)
	}
	if s != "no newline" {
		t.Fatalf("readLineTrim = %q, want %q", s, "no newline")
	}
}

func TestReadLineTrimOrDefault_usesDefaultOnEmpty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	origStdin := os.Stdin
	defer func() {
		os.Stdin = origStdin
		stdin = bufio.NewReader(os.Stdin)
	}()
	os.Stdin = r
	stdin = bufio.NewReader(os.Stdin)

	go func() {
		_, _ = w.WriteString("\n")
		_ = w.Close()
	}()

	s, err := readLineTrimOrDefault("", "default")
	if err != nil {
		t.Fatal(err)
	}
	if s != "default" {
		t.Fatalf("got %q, want default", s)
	}
}

func TestReadLineTrimOrDefault_keepsNonEmpty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	origStdin := os.Stdin
	defer func() {
		os.Stdin = origStdin
		stdin = bufio.NewReader(os.Stdin)
	}()
	os.Stdin = r
	stdin = bufio.NewReader(os.Stdin)

	go func() {
		_, _ = w.WriteString("hello\n")
		_ = w.Close()
	}()

	s, err := readLineTrimOrDefault("", "default")
	if err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Fatalf("got %q, want hello", s)
	}
}
