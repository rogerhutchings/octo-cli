package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rogerhutchings/octopus-autojoin/internal/config"
	"github.com/rogerhutchings/octopus-autojoin/internal/octopus"
	"github.com/rogerhutchings/octopus-autojoin/internal/savingsessions"
	"github.com/rogerhutchings/octopus-autojoin/internal/wheeloffortune"
)

var version = "dev"

type options struct {
	command     string
	execute     bool
	showVersion bool
}

func parseOptions(arguments []string, output io.Writer) (options, error) {
	result := options{command: "saving-sessions"}
	if len(arguments) > 0 && !strings.HasPrefix(arguments[0], "-") {
		result.command = arguments[0]
		arguments = arguments[1:]
	}
	if result.command != "saving-sessions" && result.command != "wheel-of-fortune" {
		return options{}, fmt.Errorf("unknown command %q; use saving-sessions or wheel-of-fortune", result.command)
	}
	flags := flag.NewFlagSet("octopus-autojoin", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVar(&result.execute, "execute", false, "Perform the selected action (default: dry run)")
	flags.BoolVar(&result.showVersion, "version", false, "Print version and exit")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: octopus-autojoin [saving-sessions|wheel-of-fortune] [flags]")
		fmt.Fprintln(output, "Without a command, checks Saving Sessions for backwards compatibility.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s; put the command before its flags", strings.Join(flags.Args(), " "))
	}
	return result, nil
}

func main() {
	commandOptions, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if commandOptions.showVersion {
		fmt.Println(version)
		return
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger, commandOptions); err != nil {
		logger.Error("Octopus command failed", "command", commandOptions.command, "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, commandOptions options) error {
	appConfig, err := config.Load()
	if err != nil {
		return err
	}
	client := octopus.NewClient(&http.Client{Timeout: 15 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := client.Authenticate(ctx, appConfig.APIKey); err != nil {
		return fmt.Errorf("authenticate with Octopus: %w", err)
	}
	logger.Info("authenticated with Octopus")
	if commandOptions.command == "wheel-of-fortune" {
		return wheeloffortune.Run(ctx, logger, client, appConfig.AccountNumber, commandOptions.execute)
	}
	return savingsessions.Run(ctx, logger, client, appConfig.AccountNumber, commandOptions.execute)
}
