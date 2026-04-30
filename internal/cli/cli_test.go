package cli

import (
	"flag"
	"io"
	"testing"
)

func TestRun_helpAndVersion(t *testing.T) {
	if got := Run([]string{"help"}); got != 0 {
		t.Fatalf("help: exit %d", got)
	}
	if got := Run([]string{"-h"}); got != 0 {
		t.Fatalf("-h: exit %d", got)
	}
	if got := Run([]string{"version"}); got != 0 {
		t.Fatalf("version: exit %d", got)
	}
}

func TestRun_unknownCommand(t *testing.T) {
	if got := Run([]string{"no-such-cmd-xyz"}); got != 1 {
		t.Fatalf("unknown: want exit 1, got %d", got)
	}
}

func TestRun_emptyArgs(t *testing.T) {
	if got := Run(nil); got != 0 {
		t.Fatalf("nil args: want 0, got %d", got)
	}
	if got := Run([]string{}); got != 0 {
		t.Fatalf("empty args: want 0, got %d", got)
	}
}

func TestParseByteLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"10MB", 10 * 1024 * 1024, false},
		{"1kb", 1024, false},
		{"-1MB", 0, true},
		{"999999999999GB", 0, true},
	}
	for _, tc := range cases {
		got, err := parseByteLimit(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %d want %d", tc.in, got, tc.want)
		}
	}
}

func TestCheckFlagParse_rejectsExtraPositional(t *testing.T) {
	t.Parallel()
	fs := newTestFlagSet("host")
	registerRoomCreateFlags(fs, docRoomCreateHost())
	if c := checkFlagParse(fs, fs.Parse([]string{"--name", "x", "extra"}), "host"); c == 0 {
		t.Fatal("expected non-zero exit for extra positional")
	}
}

func TestCheckFlagParse_ok(t *testing.T) {
	t.Parallel()
	fs := newTestFlagSet("host")
	registerRoomCreateFlags(fs, docRoomCreateHost())
	if c := checkFlagParse(fs, fs.Parse([]string{"--name", "x"}), "host"); c != 0 {
		t.Fatalf("unexpected exit %d", c)
	}
}

func newTestFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
