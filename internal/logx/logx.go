package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	once   sync.Once
	mu     sync.Mutex
	logf   *os.File
	level  = "info"
	inited bool
)

func Init() {
	once.Do(func() {
		level = strings.ToLower(strings.TrimSpace(os.Getenv("BRINCO_LOG_LEVEL")))
		if level == "" {
			level = "info"
		}

		dir, err := os.UserCacheDir()
		if err != nil {
			return
		}
		path := filepath.Join(dir, "brinco-cli", "logs")
		if err := os.MkdirAll(path, 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(path, "brinco.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		logf = f
		inited = true
		Info("logger iniciado")
	})
}

func Path() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "brinco-cli", "logs", "brinco.log")
}

func Debug(msg string, kv ...any) { write("debug", msg, kv...) }
func Info(msg string, kv ...any)  { write("info", msg, kv...) }
func Warn(msg string, kv ...any)  { write("warn", msg, kv...) }
func Error(msg string, kv ...any) { write("error", msg, kv...) }

func write(lvl, msg string, kv ...any) {
	if !shouldLog(lvl) {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if !inited || logf == nil {
		return
	}
	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("%s level=%s msg=%q", ts, lvl, msg)
	for i := 0; i+1 < len(kv); i += 2 {
		line += fmt.Sprintf(" %v=%v", kv[i], kv[i+1])
	}
	_, _ = logf.WriteString(line + "\n")
}

func shouldLog(lvl string) bool {
	rank := map[string]int{"debug": 10, "info": 20, "warn": 30, "error": 40}
	cur := rank[level]
	if cur == 0 {
		cur = 20
	}
	want := rank[lvl]
	return want >= cur
}
