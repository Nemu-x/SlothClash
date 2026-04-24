package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	debugSessionID = "cb9690"
)

var (
	debugLogPathOnce sync.Once
	debugLogPath     string
)

func resolveDebugLogPath() string {
	debugLogPathOnce.Do(func() {
		base, err := os.UserConfigDir()
		if err != nil || base == "" {
			debugLogPath = "debug-cb9690.log"
			return
		}
		dir := filepath.Join(base, "SlothClash", "runtime", "logs")
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			debugLogPath = filepath.Join(base, "SlothClash", "debug-cb9690.log")
			return
		}
		debugLogPath = filepath.Join(dir, "debug-cb9690.log")
	})
	return debugLogPath
}

func debugLog(runID, hypothesisID, location, message string, data map[string]any) {
	payload := map[string]any{
		"sessionId":    debugSessionID,
		"runId":        runID,
		"hypothesisId": hypothesisID,
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	f, err := os.OpenFile(resolveDebugLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
