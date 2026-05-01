package notify

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	LevelAll      = "all"
	LevelDirect   = "direct"
	LevelMentions = "mentions"
	LevelNone     = "none"
)

const (
	EventTypePrivate   = "private"
	EventTypeFile      = "file"
	EventTypeFileChunk = "file_chunk"
)

type Event struct {
	Type   string
	From   string
	To     string
	Text   string
	MyNick string
}

type Options struct {
	Sound          bool
	Level          string
	Throttle       time.Duration
	NativeCooldown time.Duration
}

var (
	mu         sync.Mutex
	options    = Options{Sound: true, Level: LevelAll, Throttle: 250 * time.Millisecond, NativeCooldown: 1200 * time.Millisecond}
	lastNotify time.Time
	lastNative time.Time
)

func Configure(next Options) {
	mu.Lock()
	defer mu.Unlock()
	options.Sound = next.Sound
	options.Level = normalizeLevel(next.Level)
	if next.Throttle > 0 {
		options.Throttle = next.Throttle
	}
	if next.NativeCooldown > 0 {
		options.NativeCooldown = next.NativeCooldown
	}
}

func Current() Options {
	mu.Lock()
	defer mu.Unlock()
	return options
}

func Trigger(event Event) {
	mu.Lock()
	opts := options
	if !opts.Sound || !shouldNotify(opts.Level, event) {
		mu.Unlock()
		return
	}
	if !lastNotify.IsZero() && time.Since(lastNotify) < opts.Throttle {
		mu.Unlock()
		return
	}
	lastNotify = time.Now()
	playNativeNow := lastNative.IsZero() || time.Since(lastNative) >= opts.NativeCooldown
	if playNativeNow {
		lastNative = time.Now()
	}
	mu.Unlock()

	_, _ = fmt.Fprint(os.Stderr, "\a")
	if playNativeNow {
		playNative()
	}
}

func normalizeLevel(level string) string {
	normalized := strings.ToLower(strings.TrimSpace(level))
	switch normalized {
	case LevelAll, LevelDirect, LevelMentions, LevelNone:
		return normalized
	default:
		return LevelAll
	}
}

func shouldNotify(level string, event Event) bool {
	from := strings.TrimSpace(event.From)
	mine := strings.TrimSpace(event.MyNick)
	if from != "" && mine != "" && strings.EqualFold(from, mine) {
		return false
	}
	switch normalizeLevel(level) {
	case LevelNone:
		return false
	case LevelAll:
		return true
	case LevelDirect:
		return strings.TrimSpace(event.To) != "" || event.Type == EventTypePrivate || event.Type == EventTypeFile || event.Type == EventTypeFileChunk
	case LevelMentions:
		if strings.TrimSpace(event.To) != "" || event.Type == EventTypePrivate {
			return true
		}
		if mine == "" {
			return false
		}
		return containsMention(event.Text, mine)
	default:
		return true
	}
}

func containsMention(text, nick string) bool {
	nick = strings.TrimSpace(nick)
	if nick == "" {
		return false
	}
	pattern := `(?i)(^|[^\pL\pN_])@` + regexp.QuoteMeta(nick) + `($|[^\pL\pN_])`
	matched, err := regexp.MatchString(pattern, text)
	if err != nil {
		debugLog("notify: mention regex error: %v", err)
		return strings.Contains(strings.ToLower(text), "@"+strings.ToLower(nick))
	}
	return matched
}

func playNative() {
	switch runtime.GOOS {
	case "windows":
		playWindows()
	case "darwin":
		if _, err := exec.LookPath("afplay"); err == nil {
			if err := exec.Command("afplay", "/System/Library/Sounds/Pop.aiff").Start(); err != nil {
				debugLog("notify: afplay failed: %v", err)
			}
		} else {
			debugLog("notify: afplay not found for native sound")
		}
	case "linux":
		playLinux()
	}
}

func playWindows() {
	script := `[System.Media.SystemSounds]::Exclamation.Play(); Start-Sleep -Milliseconds 350`
	for _, bin := range []string{"powershell.exe", "powershell", "pwsh.exe", "pwsh"} {
		if _, err := exec.LookPath(bin); err == nil {
			if err := exec.Command(bin, "-NoProfile", "-NonInteractive", "-Command", script).Start(); err != nil {
				debugLog("notify: windows sound command failed with %s: %v", bin, err)
			}
			return
		}
	}
	debugLog("notify: no powershell binary found for native sound")
}

func playLinux() {
	if _, err := exec.LookPath("paplay"); err == nil {
		if err := exec.Command("paplay", "/usr/share/sounds/freedesktop/stereo/message.oga").Start(); err != nil {
			debugLog("notify: paplay failed: %v", err)
		}
		return
	}
	if _, err := exec.LookPath("canberra-gtk-play"); err == nil {
		if err := exec.Command("canberra-gtk-play", "-i", "message").Start(); err != nil {
			debugLog("notify: canberra-gtk-play failed: %v", err)
		}
		return
	}
	if _, err := exec.LookPath("aplay"); err == nil {
		if err := exec.Command("aplay", "/usr/share/sounds/alsa/Front_Center.wav").Start(); err != nil {
			debugLog("notify: aplay failed: %v", err)
		}
		return
	}
	debugLog("notify: no linux native sound binary found")
}

func debugLog(format string, args ...any) {
	if os.Getenv("BRINCO_NOTIFY_DEBUG") != "1" {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}
