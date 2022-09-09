package exporter

import (
	"bytes"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

// RotationLogger is logger wrapper for save history logs
type RotationLogger struct {
	mu           sync.Mutex
	history      []string
	maxMessages  uint
	next         log.Logger
	buffer       bytes.Buffer
	bufferLogger log.Logger
	LastIsError  bool
	errorCounter uint
}

func newRotationLogger(logger log.Logger, maxMessages uint) *RotationLogger {
	rl := &RotationLogger{
		next:        logger,
		maxMessages: maxMessages,
		buffer:      bytes.Buffer{},
	}
	bl := log.NewLogfmtLogger(&rl.buffer)
	rl.bufferLogger = bl
	rl.bufferLogger = log.With(bl, "ts", log.DefaultTimestampUTC)
	return rl
}

// Log - save message to histiry, count errors and pass message to next logger
func (rl *RotationLogger) Log(keyvals ...interface{}) error {
	for i := 0; i < len(keyvals); i += 2 {
		if keyvals[i] == level.Key() {
			if keyvals[i+1] == level.WarnValue() || keyvals[i+1] == level.ErrorValue() {
				rl.errorCounter++
				rl.LastIsError = true
			} else {
				rl.LastIsError = false
			}
		}
	}
	rl.addToHistory(keyvals...)
	return rl.next.Log(keyvals...)
}

// GetHistory return last maxMessages messages
func (rl *RotationLogger) GetHistory() []string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.history
}

// GetLogger return next logger
func (rl *RotationLogger) GetLogger() log.Logger {
	return rl.next
}

// SetLogger setup next logger
func (rl *RotationLogger) SetLogger(logger log.Logger) {
	rl.next = logger
}

// addToHistory add log message to history and rotate history
func (rl *RotationLogger) addToHistory(keyvals ...interface{}) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.bufferLogger.Log(keyvals...)
	rl.history = append(rl.history, string(rl.buffer.Bytes()))
	if uint(len(rl.history)) > rl.maxMessages {
		history := make([]string, len(rl.history)-1)
		copy(history, rl.history[1:])
		rl.history = history
	}
	rl.buffer.Reset()
}
