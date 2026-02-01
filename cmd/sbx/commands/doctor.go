package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/docker"
	"github.com/slok/sbx/internal/sandbox/firecracker"
)

type DoctorCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	engine string
}

// NewDoctorCommand returns the doctor command.
func NewDoctorCommand(rootCmd *RootCommand, app *kingpin.Application) *DoctorCommand {
	c := &DoctorCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("doctor", "Run preflight checks for sandbox engines.")
	c.Cmd.Flag("engine", "Engine to check (docker, firecracker, all).").Default("all").EnumVar(&c.engine, "docker", "firecracker", "all")

	return c
}

func (c DoctorCommand) Name() string { return c.Cmd.FullCommand() }

func (c DoctorCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger
	out := c.rootCmd.Stdout

	var allResults []engineCheckResults

	// Check Docker engine
	if c.engine == "docker" || c.engine == "all" {
		dockerEngine, err := docker.NewEngine(docker.EngineConfig{
			Logger: logger,
		})
		if err != nil {
			return fmt.Errorf("could not create docker engine: %w", err)
		}

		results := dockerEngine.Check(ctx)
		allResults = append(allResults, engineCheckResults{
			name:    "docker",
			results: results,
		})
	}

	// Check Firecracker engine
	if c.engine == "firecracker" || c.engine == "all" {
		fcEngine, err := firecracker.NewEngine(firecracker.EngineConfig{
			Logger: logger,
		})
		if err != nil {
			return fmt.Errorf("could not create firecracker engine: %w", err)
		}

		results := fcEngine.Check(ctx)
		allResults = append(allResults, engineCheckResults{
			name:    "firecracker",
			results: results,
		})
	}

	// Print results
	totalErrors := 0
	totalWarnings := 0

	for _, er := range allResults {
		fmt.Fprintf(out, "\nChecking %s engine...\n", er.name)
		for _, r := range er.results {
			icon := getStatusIcon(r.Status)
			fmt.Fprintf(out, "  %s %-20s %s\n", icon, r.ID, r.Message)

			switch r.Status {
			case model.CheckStatusError:
				totalErrors++
			case model.CheckStatusWarning:
				totalWarnings++
			}
		}
	}

	// Summary
	fmt.Fprintln(out)
	if totalErrors == 0 && totalWarnings == 0 {
		fmt.Fprintln(out, "All checks passed!")
	} else {
		var summary []string
		if totalErrors > 0 {
			summary = append(summary, fmt.Sprintf("%d error(s)", totalErrors))
		}
		if totalWarnings > 0 {
			summary = append(summary, fmt.Sprintf("%d warning(s)", totalWarnings))
		}
		fmt.Fprintf(out, "%s\n", joinWithComma(summary))
	}

	// Return error if there are any errors
	if totalErrors > 0 {
		return fmt.Errorf("preflight checks failed with %d error(s)", totalErrors)
	}

	return nil
}

type engineCheckResults struct {
	name    string
	results []model.CheckResult
}

func getStatusIcon(status model.CheckStatus) string {
	switch status {
	case model.CheckStatusOK:
		return "OK"
	case model.CheckStatusWarning:
		return "!!"
	case model.CheckStatusError:
		return "XX"
	default:
		return "??"
	}
}

func joinWithComma(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += ", " + parts[i]
	}
	return result
}
