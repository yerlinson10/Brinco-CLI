package notify

import (
	"testing"
	"time"
)

func TestNormalizeLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{in: "all", want: LevelAll},
		{in: "  DIRECT  ", want: LevelDirect},
		{in: "mentions", want: LevelMentions},
		{in: "none", want: LevelNone},
		{in: "unknown", want: LevelAll},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got := normalizeLevel(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeLevel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestContainsMention(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		nick string
		want bool
	}{
		{name: "simple mention", text: "hola @demo", nick: "demo", want: true},
		{name: "mention with punctuation", text: "hola, @demo!", nick: "demo", want: true},
		{name: "mention case insensitive", text: "hola @DeMo", nick: "demo", want: true},
		{name: "no mention", text: "hola demo", nick: "demo", want: false},
		{name: "embedded in word", text: "test@demo123", nick: "demo", want: false},
		{name: "empty nick", text: "hola @demo", nick: "", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := containsMention(tt.text, tt.nick)
			if got != tt.want {
				t.Fatalf("containsMention(%q, %q) = %v, want %v", tt.text, tt.nick, got, tt.want)
			}
		})
	}
}

func TestShouldNotify(t *testing.T) {
	t.Parallel()

	if shouldNotify(LevelNone, Event{Text: "hola"}) {
		t.Fatal("LevelNone should never notify")
	}

	if !shouldNotify(LevelAll, Event{Text: "hola"}) {
		t.Fatal("LevelAll should notify")
	}

	if shouldNotify(LevelAll, Event{From: "me", MyNick: "me", Text: "self"}) {
		t.Fatal("self messages should not notify")
	}

	if !shouldNotify(LevelDirect, Event{Type: EventTypePrivate, Text: "hola"}) {
		t.Fatal("private events should notify on direct level")
	}

	if !shouldNotify(LevelDirect, Event{To: "demo", Text: "hola"}) {
		t.Fatal("events with To should notify on direct level")
	}

	if shouldNotify(LevelDirect, Event{Text: "hola"}) {
		t.Fatal("non-direct events should not notify on direct level")
	}

	if !shouldNotify(LevelMentions, Event{Text: "hola @demo", MyNick: "demo"}) {
		t.Fatal("mentions should notify on mentions level")
	}

	if shouldNotify(LevelMentions, Event{Text: "hola demo", MyNick: "demo"}) {
		t.Fatal("non-mentions should not notify on mentions level")
	}
}

func TestConfigureKeepsPositiveDurations(t *testing.T) {
	mu.Lock()
	prevOptions := options
	prevLastNotify := lastNotify
	prevLastNative := lastNative
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		options = prevOptions
		lastNotify = prevLastNotify
		lastNative = prevLastNative
		mu.Unlock()
	})

	Configure(Options{
		Sound:          true,
		Level:          LevelAll,
		Throttle:       500 * time.Millisecond,
		NativeCooldown: 2 * time.Second,
	})
	got := Current()
	if got.Throttle != 500*time.Millisecond {
		t.Fatalf("expected throttle update, got %v", got.Throttle)
	}
	if got.NativeCooldown != 2*time.Second {
		t.Fatalf("expected native cooldown update, got %v", got.NativeCooldown)
	}

	Configure(Options{
		Sound:          got.Sound,
		Level:          got.Level,
		Throttle:       0,
		NativeCooldown: -1 * time.Second,
	})
	got2 := Current()
	if got2.Throttle != 500*time.Millisecond {
		t.Fatalf("expected throttle to remain unchanged, got %v", got2.Throttle)
	}
	if got2.NativeCooldown != 2*time.Second {
		t.Fatalf("expected native cooldown to remain unchanged, got %v", got2.NativeCooldown)
	}
}
