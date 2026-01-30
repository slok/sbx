package commands

import (
	"context"
	"io"
	"path/filepath"

	"github.com/alecthomas/kingpin/v2"
	"k8s.io/client-go/util/homedir"

	"github.com/slok/sbx/internal/log"
)

const (
	// LoggerTypeDefault is the logger default type.
	LoggerTypeDefault = "default"
	// LoggerTypeJSON is the logger json type.
	LoggerTypeJSON = "json"
)

// Command represents an application command, all commands that want to be executed
// should implement and setup on main.
type Command interface {
	Name() string
	Run(ctx context.Context) error
}

// RootCommand represents the root command configuration and global configuration
// for all the commands.
type RootCommand struct {
	// Global flags.
	Debug      bool
	NoLog      bool
	NoColor    bool
	LoggerType string
	DBPath     string

	// Global instances.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Logger log.Logger
}

// NewRootCommand initializes the main root configuration.
func NewRootCommand(app *kingpin.Application) *RootCommand {
	c := &RootCommand{}

	app.Flag("debug", "Enable debug mode.").BoolVar(&c.Debug)
	app.Flag("no-log", "Disable logger.").BoolVar(&c.NoLog)
	app.Flag("no-color", "Disable logger color.").BoolVar(&c.NoColor)
	app.Flag("logger", "Selects the logger type.").Default(LoggerTypeDefault).EnumVar(&c.LoggerType, LoggerTypeDefault, LoggerTypeJSON)

	defaultDBPath := filepath.Join(homedir.HomeDir(), ".sbx", "sbx.db")
	app.Flag("db-path", "Path to the SQLite database file.").Envar("SBX_DB_PATH").Default(defaultDBPath).StringVar(&c.DBPath)

	return c
}
