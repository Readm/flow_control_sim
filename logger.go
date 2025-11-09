package main

import (
	"fmt"
	logpkg "log"
	"os"
)

// LogLevel defines severity for logger output.
type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
)

// Logger provides leveled logging.
type Logger struct {
	level  LogLevel
	logger *logpkg.Logger
}

// NewLogger creates a logger with desired level and prefix.
func NewLogger(level LogLevel, prefix string) *Logger {
	return &Logger{
		level:  level,
		logger: logpkg.New(os.Stdout, prefix, logpkg.LstdFlags|logpkg.Lmicroseconds),
	}
}

// SetLevel adjusts current logging level.
func (l *Logger) SetLevel(level LogLevel) {
	if l == nil {
		return
	}
	l.level = level
}

func (l *Logger) logf(target LogLevel, format string, args ...any) {
	if l == nil || target > l.level {
		return
	}
	l.logger.Output(3, fmt.Sprintf(format, args...))
}

// Debugf prints debug messages.
func (l *Logger) Debugf(format string, args ...any) {
	l.logf(LogLevelDebug, format, args...)
}

// Infof prints info messages.
func (l *Logger) Infof(format string, args ...any) {
	l.logf(LogLevelInfo, format, args...)
}

// Warnf prints warning messages.
func (l *Logger) Warnf(format string, args ...any) {
	l.logf(LogLevelWarn, format, args...)
}

// Errorf prints error messages.
func (l *Logger) Errorf(format string, args ...any) {
	l.logf(LogLevelError, format, args...)
}

var defaultLogger = NewLogger(LogLevelInfo, "[FLOW] ")

// GetLogger returns the global logger.
func GetLogger() *Logger {
	return defaultLogger
}

// SetLogger replaces the global logger (primarily for tests).
func SetLogger(l *Logger) {
	if l == nil {
		return
	}
	defaultLogger = l
}
