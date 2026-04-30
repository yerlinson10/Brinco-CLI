package notify

import (
	"fmt"
	"os"
	"os/exec"
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

type Event struct {
	Type   string
	From   string
	To     string
	Text   string
	MyNick string
}

type Options struct {
	Sound    bool
	Level    string
	Throttle time.Duration
}

var (
	mu         sync.Mutex
	options    = Options{Sound: true, Level: LevelAll, Throttle: 250 * time.Millisecond}
	lastNotify time.Time
)

func Configure(next Options) {
	mu.Lock()
	defer mu.Unlock()
	options.Sound = next.Sound
	options.Level = normalizeLevel(next.Level)
	if next.Throttle > 0 {
		options.Throttle = next.Throttle
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
	mu.Unlock()

	_, _ = fmt.Fprint(os.Stderr, "\a")
	playNative()
}

func normalizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case LevelAll, LevelDirect, LevelMentions, LevelNone:
		return strings.ToLower(strings.TrimSpace(level))
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
		return strings.TrimSpace(event.To) != "" || event.Type == "private" || event.Type == "file" || event.Type == "file_chunk"
	case LevelMentions:
		if strings.TrimSpace(event.To) != "" || event.Type == "private" {
			return true
		}
		if mine == "" {
			return false
		}
		return strings.Contains(strings.ToLower(event.Text), "@"+strings.ToLower(mine))
	default:
		return true
	}
}

func playNative() {
	switch runtime.GOOS {
	case "windows":
		playWindows()
	case "darwin":
		if _, err := exec.LookPath("afplay"); err == nil {
			_ = exec.Command("afplay", "/System/Library/Sounds/Pop.aiff").Start()
		}
	case "linux":
		playLinux()
	}
}

func playWindows() {
	script := `[System.Media.SystemSounds]::Exclamation.Play(); Start-Sleep -Milliseconds 350`
	for _, bin := range []string{"powershell.exe", "powershell", "pwsh.exe", "pwsh"} {
		if _, err := exec.LookPath(bin); err == nil {
			_ = exec.Command(bin, "-NoProfile", "-NonInteractive", "-Command", script).Start()
			return
		}
	}
}

func playLinux() {
	if _, err := exec.LookPath("paplay"); err == nil {
		_ = exec.Command("paplay", "/usr/share/sounds/freedesktop/stereo/message.oga").Start()
		return
	}
	if _, err := exec.LookPath("canberra-gtk-play"); err == nil {
		_ = exec.Command("canberra-gtk-play", "-i", "message").Start()
		return
	}
	if _, err := exec.LookPath("aplay"); err == nil {
		_ = exec.Command("aplay", "/usr/share/sounds/alsa/Front_Center.wav").Start()
	}
}
