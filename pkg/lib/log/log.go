// Package log provides the logging interface for the sbx SDK.
//
// The SDK accepts any implementation of [Logger]. Use [Noop] to disable
// logging (this is the default when no logger is configured).
//
// To integrate with your application's logger, implement the [Logger] interface:
//
//	type myLogger struct{}
//
//	func (l myLogger) Infof(format string, args ...any)    { slog.Info(fmt.Sprintf(format, args...)) }
//	func (l myLogger) Warningf(format string, args ...any) { slog.Warn(fmt.Sprintf(format, args...)) }
//	func (l myLogger) Errorf(format string, args ...any)   { slog.Error(fmt.Sprintf(format, args...)) }
//	func (l myLogger) Debugf(format string, args ...any)   { slog.Debug(fmt.Sprintf(format, args...)) }
//	// ... remaining methods
package log

import "github.com/slok/sbx/internal/log"

// Logger is the interface that loggers must implement for the SDK.
//
// It supports structured logging through [Kv] values and context propagation.
// For most use cases, only the format methods (Infof, Warningf, Errorf, Debugf)
// need meaningful implementations.
type Logger = log.Logger

// Kv is a helper type for structured logging key-value pairs.
type Kv = log.Kv

// Noop is a logger that discards all log output. This is the default logger
// when none is provided in [lib.Config].
var Noop = log.Noop
