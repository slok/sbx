package log

import "github.com/slok/sbx/internal/log"

// Logger is the interface that loggers must implement for the SDK.
type Logger = log.Logger

// Kv is a helper type for structured logging fields.
type Kv = log.Kv

// Noop is a logger that discards all log output.
var Noop = log.Noop
