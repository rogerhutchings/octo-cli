package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rogerhutchings/octo-cli/internal/config"
	"github.com/rogerhutchings/octo-cli/internal/octopus"
	"github.com/rogerhutchings/octo-cli/internal/savingsessions"
	"github.com/rogerhutchings/octo-cli/internal/wheeloffortune"
)

type Dependencies struct {
	LoadConfig   func() (config.Config, error)
	NewClient    func(*http.Client) *octopus.Client
	Authenticate func(context.Context, *octopus.Client, string) error
	RunJoin      func(context.Context, *octopus.Client, string, bool, io.Writer) error
	RunSpin      func(context.Context, *octopus.Client, string, bool, io.Writer) error
	RunHistory   func(context.Context, *octopus.Client, string, wheeloffortune.HistoryFilter, io.Writer) error
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		LoadConfig: config.Load,
		NewClient:  octopus.NewClient,
		Authenticate: func(ctx context.Context, client *octopus.Client, key string) error {
			return client.Authenticate(ctx, key)
		},
		RunJoin:    savingsessions.Run,
		RunSpin:    wheeloffortune.Run,
		RunHistory: wheeloffortune.RunHistory,
	}
}

type command struct {
	path     string
	execute  bool
	fromDate string
	toDate   string
	fuel     string
}

// Run parses and executes one CLI invocation. It returns a process exit code.
func Run(args []string, stdout, stderr io.Writer, version string, deps Dependencies) int {
	selected, help, showVersion, err := parse(args, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if help {
		return 0
	}
	if showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	if err := execute(selected, logger, stdout, deps); err != nil {
		logger.Error("Octopus command failed", "command", selected.path, "error", err)
		return 1
	}
	return 0
}

func parse(args []string, output io.Writer) (command, bool, bool, error) {
	if len(args) == 0 {
		printRootHelp(output)
		return command{}, true, false, nil
	}
	if isHelp(args[0]) {
		if len(args) != 1 {
			return command{}, false, false, unexpected(args[1:])
		}
		printRootHelp(output)
		return command{}, true, false, nil
	}
	if args[0] == "--version" || args[0] == "-version" {
		if len(args) != 1 {
			return command{}, false, false, unexpected(args[1:])
		}
		return command{}, false, true, nil
	}

	if len(args) < 2 {
		if args[0] == "saving-sessions" {
			printSavingHelp(output)
			return command{}, true, false, nil
		}
		if args[0] == "wheel" {
			printWheelHelp(output)
			return command{}, true, false, nil
		}
		return command{}, false, false, fmt.Errorf("unknown command %q; use saving-sessions or wheel", args[0])
	}

	group, leaf := args[0], args[1]
	if group != "saving-sessions" && group != "wheel" {
		return command{}, false, false, fmt.Errorf("unknown command %q; use saving-sessions or wheel", group)
	}
	if isHelp(leaf) {
		if len(args) != 2 {
			return command{}, false, false, unexpected(args[2:])
		}
		if group == "saving-sessions" {
			printSavingHelp(output)
		} else {
			printWheelHelp(output)
		}
		return command{}, true, false, nil
	}
	if (group == "saving-sessions" && leaf != "join") || (group == "wheel" && leaf != "spin" && leaf != "history") {
		return command{}, false, false, fmt.Errorf("unknown command %q under %q", leaf, group)
	}

	selected := command{path: group + " " + leaf}
	flags := flag.NewFlagSet("octo-cli "+selected.path, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() { printLeafHelp(output, selected.path) }
	var showHelp bool
	flags.BoolVar(&showHelp, "help", false, "Show command help")
	if leaf == "join" || leaf == "spin" {
		flags.BoolVar(&selected.execute, "execute", false, "Perform account changes (default: dry run)")
	}
	if leaf == "history" {
		flags.StringVar(&selected.fromDate, "from", "", "Filter history from YYYY-MM-DD")
		flags.StringVar(&selected.toDate, "to", "", "Filter history through YYYY-MM-DD")
		flags.StringVar(&selected.fuel, "fuel", "", "Filter fuel: electricity or gas")
	}
	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return command{}, true, false, nil
		}
		return command{}, false, false, err
	}
	if showHelp {
		printLeafHelp(output, selected.path)
		return command{}, true, false, nil
	}
	if flags.NArg() != 0 {
		return command{}, false, false, unexpected(flags.Args())
	}
	if leaf == "history" {
		if selected.fuel != "" && selected.fuel != "electricity" && selected.fuel != "gas" {
			return command{}, false, false, fmt.Errorf("invalid --fuel %q; use electricity or gas", selected.fuel)
		}
		var from, to time.Time
		var err error
		if selected.fromDate != "" {
			from, err = time.Parse("2006-01-02", selected.fromDate)
			if err != nil {
				return command{}, false, false, fmt.Errorf("invalid --from date %q; use YYYY-MM-DD", selected.fromDate)
			}
		}
		if selected.toDate != "" {
			to, err = time.Parse("2006-01-02", selected.toDate)
			if err != nil {
				return command{}, false, false, fmt.Errorf("invalid --to date %q; use YYYY-MM-DD", selected.toDate)
			}
		}
		if !from.IsZero() && !to.IsZero() && from.After(to) {
			return command{}, false, false, errors.New("--from must be on or before --to")
		}
	}
	return selected, false, false, nil
}

func execute(selected command, logger *slog.Logger, stdout io.Writer, deps Dependencies) error {
	appConfig, err := deps.LoadConfig()
	if err != nil {
		return err
	}
	client := deps.NewClient(&http.Client{Timeout: 15 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := deps.Authenticate(ctx, client, appConfig.APIKey); err != nil {
		return fmt.Errorf("authenticate with Octopus: %w", err)
	}
	logger.Info("authenticated with Octopus")
	switch selected.path {
	case "saving-sessions join":
		return deps.RunJoin(ctx, client, appConfig.AccountNumber, selected.execute, stdout)
	case "wheel spin":
		return deps.RunSpin(ctx, client, appConfig.AccountNumber, selected.execute, stdout)
	case "wheel history":
		return deps.RunHistory(ctx, client, appConfig.AccountNumber, wheeloffortune.HistoryFilter{
			From: selected.fromDate,
			To:   selected.toDate,
			Fuel: strings.ToUpper(selected.fuel),
		}, stdout)
	default:
		return fmt.Errorf("unsupported command %q", selected.path)
	}
}

func isHelp(value string) bool { return value == "--help" || value == "-h" }

func unexpected(args []string) error {
	return fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
}

func printRootHelp(output io.Writer) {
	fmt.Fprintln(output, "Usage: octo-cli <command> [command] [flags]")
	fmt.Fprintln(output, "\nCommands:")
	fmt.Fprintln(output, "  saving-sessions  Saving Sessions commands")
	fmt.Fprintln(output, "  wheel            Wheel of Fortune commands")
	fmt.Fprintln(output, "\nUse octo-cli <command> --help for command help.")
	fmt.Fprintln(output, "Global flags: --help, --version")
}

func printSavingHelp(output io.Writer) {
	fmt.Fprintln(output, "Usage: octo-cli saving-sessions <command> [flags]")
	fmt.Fprintln(output, "\nCommands:\n  join  Join eligible Saving Sessions")
}

func printWheelHelp(output io.Writer) {
	fmt.Fprintln(output, "Usage: octo-cli wheel <command> [flags]")
	fmt.Fprintln(output, "\nCommands:")
	fmt.Fprintln(output, "  spin     Check or use available spins")
	fmt.Fprintln(output, "  history  Show Wheel of Fortune history")
}

func printLeafHelp(output io.Writer, path string) {
	switch path {
	case "saving-sessions join":
		fmt.Fprintln(output, "Usage: octo-cli saving-sessions join [--execute]")
		fmt.Fprintln(output, "\n--execute  Join eligible sessions (default: dry run)")
	case "wheel spin":
		fmt.Fprintln(output, "Usage: octo-cli wheel spin [--execute]")
		fmt.Fprintln(output, "\n--execute  Use available spins (default: dry run)")
	case "wheel history":
		fmt.Fprintln(output, "Usage: octo-cli wheel history [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--fuel electricity|gas]")
		fmt.Fprintln(output, "\n--from  Filter history from this date")
		fmt.Fprintln(output, "--to    Filter history through this date")
		fmt.Fprintln(output, "--fuel  Filter history by fuel")
	}
}
