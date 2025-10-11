package aimux

// log.go - Structured logging for AIMUX with configurable levels

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging with levels
type Logger struct {
	level  LogLevel
	output *os.File
	mu     sync.Mutex
}

// DefaultLogger is the global logger instance
var DefaultLogger = &Logger{
	level:  INFO,
	output: os.Stderr,
}

// SetLevel sets the minimum log level
func SetLevel(level LogLevel) {
	DefaultLogger.mu.Lock()
	defer DefaultLogger.mu.Unlock()
	DefaultLogger.level = level
}

// log writes a log message if the level is high enough
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.output, "[%s] %s: %s\n", timestamp, level, message)
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	DefaultLogger.log(DEBUG, format, args...)
}

// Warn logs a warning message
func Warn(format string, args ...interface{}) {
	DefaultLogger.log(WARN, format, args...)
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	DefaultLogger.log(ERROR, format, args...)
}
