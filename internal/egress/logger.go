package egress

// Logger is a minimal logger interface for the egress package.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warningf(format string, args ...any)
	Errorf(format string, args ...any)
}

// noopLogger is a logger that discards all output.
type noopLogger struct{}

func (noopLogger) Debugf(string, ...any)   {}
func (noopLogger) Infof(string, ...any)    {}
func (noopLogger) Warningf(string, ...any) {}
func (noopLogger) Errorf(string, ...any)   {}
