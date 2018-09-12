package main

import (
	"bytes"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

// RotationLogger has history rotated logs
type RotationLogger interface {
	Log(keyvals ...interface{}) error
	SetLogger(logger log.Logger)
	GetLogger() log.Logger
	SetBufferLogger(logger log.Logger)
	GetBuferLogger() log.Logger
	GetHistory() []string
	GetMaxMessages() uint
	GerErrorsCount() uint
	LastIsError() bool
}

type rotationLogger struct {
	mu           sync.Mutex
	history      []string
	maxMessages  uint
	next         log.Logger
	buffer       bytes.Buffer
	bufferLogger log.Logger
	lastIsError  bool
	errorCounter uint
}

func newRotationLogger(logger log.Logger, maxMessages uint) RotationLogger {
	rl := &rotationLogger{
		next:        logger,
		maxMessages: maxMessages,
		buffer:      bytes.Buffer{},
	}
	bl := log.NewLogfmtLogger(&rl.buffer)
	rl.bufferLogger = bl
	rl.bufferLogger = log.With(bl, "ts", log.DefaultTimestampUTC)
	return rl
}

func (rl *rotationLogger) Log(keyvals ...interface{}) error {
	for i := 0; i < len(keyvals); i += 2 {
		if keyvals[i] == level.Key() {
			if keyvals[i+1] == level.WarnValue() || keyvals[i+1] == level.ErrorValue() {
				rl.errorCounter++
				rl.lastIsError = true
			} else {
				rl.lastIsError = false
			}
		}
	}
	rl.addToHistory(keyvals...)
	return rl.next.Log(keyvals...)
}

func (rl *rotationLogger) SetBufferLogger(logger log.Logger) {
	rl.bufferLogger = logger
}

func (rl *rotationLogger) GetBuferLogger() log.Logger {
	return rl.bufferLogger
}

func (rl *rotationLogger) GetHistory() []string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.history
}

func (rl *rotationLogger) GetLogger() log.Logger {
	return rl.next
}

func (rl *rotationLogger) SetLogger(logger log.Logger) {
	rl.next = logger
}

func (rl *rotationLogger) addToHistory(keyvals ...interface{}) {
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

func (rl *rotationLogger) GetMaxMessages() uint {
	return rl.maxMessages
}

func (rl *rotationLogger) LastIsError() bool {
	return rl.lastIsError
}

func (rl rotationLogger) GerErrorsCount() uint {
	return rl.errorCounter
}
