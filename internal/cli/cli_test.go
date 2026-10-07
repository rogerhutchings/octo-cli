package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rogerhutchings/octo-cli/internal/config"
	"github.com/rogerhutchings/octo-cli/internal/octopus"
	"github.com/rogerhutchings/octo-cli/internal/wheeloffortune"
)

func testDependencies() (Dependencies, *int, *[]string) {
	loads := 0
	var calls []string
	deps := Dependencies{
		LoadConfig: func() (config.Config, error) {
			loads++
			return config.Config{APIKey: "key", AccountNumber: "A-TEST"}, nil
		},
		NewClient: func(*http.Client) *octopus.Client { return octopus.NewClient(nil) },
		Authenticate: func(context.Context, *octopus.Client, string) error {
			calls = append(calls, "authenticate")
			return nil
		},
		RunJoin: func(_ context.Context, _ *octopus.Client, account string, execute bool, results io.Writer) error {
			calls = append(calls, "join:"+account+":"+boolString(execute))
			_, err := fmt.Fprintln(results, "join result")
			return err
		},
		RunSpin: func(_ context.Context, _ *octopus.Client, account string, execute bool, maxSpins int, results io.Writer) error {
			calls = append(calls, fmt.Sprintf("spin:%s:%s:%d", account, boolString(execute), maxSpins))
			_, err := fmt.Fprintln(results, "spin result")
			return err
		},
		RunHistory: func(_ context.Context, _ *octopus.Client, account string, filter wheeloffortune.HistoryFilter, _ io.Writer) error {
			calls = append(calls, "history:"+account+":"+filter.From+":"+filter.To+":"+filter.Fuel)
			return nil
		},
	}
	return deps, &loads, &calls
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestHelpAndVersionDoNotLoadConfiguration(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "root help", args: []string{"--help"}, want: "saving-sessions"},
		{name: "bare root", want: "wheel"},
		{name: "saving group help", args: []string{"saving-sessions", "--help"}, want: "join"},
		{name: "wheel group help", args: []string{"wheel"}, want: "history"},
		{name: "saving leaf help", args: []string{"saving-sessions", "join", "--help"}, want: "[--execute]"},
		{name: "leaf help", args: []string{"wheel", "spin", "--help"}, want: "--max-spins"},
		{name: "history help", args: []string{"wheel", "history", "--help"}, want: "[--fuel electricity|gas]"},
		{name: "version", args: []string{"--version"}, want: "release-test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps, loads, _ := testDependencies()
			var stdout, stderr strings.Builder
			if code := Run(test.args, &stdout, &stderr, "release-test", deps); code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("stdout %q does not contain %q", stdout.String(), test.want)
			}
			if *loads != 0 {
				t.Fatalf("loaded configuration %d times", *loads)
			}
		})
	}
}

func TestInvalidCommandsFailBeforeConfiguration(t *testing.T) {
	for _, args := range [][]string{
		{"saving-sessions", "--execute"},
		{"wheel-of-fortune"},
		{"saving-sessions", "join", "--fuel", "gas"},
		{"wheel", "spin", "--from", "2026-01-01"},
		{"wheel", "history", "--execute"},
		{"wheel", "history", "unexpected"},
		{"wheel", "history", "--from", "2026-1-01"},
		{"wheel", "history", "--to", "not-a-date"},
		{"wheel", "history", "--from", "2026-02-01", "--to", "2026-01-31"},
		{"wheel", "history", "--fuel", "electric"},
		{"wheel", "spin", "--unknown"},
		{"wheel", "spin", "--max-spins", "0"},
		{"wheel", "spin", "--max-spins", "-1"},
		{"wheel", "spin", "--max-spins", "many"},
		{"wheel", "spin", "--execute", "join"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			deps, loads, calls := testDependencies()
			var stdout, stderr strings.Builder
			if code := Run(args, &stdout, &stderr, "dev", deps); code != 2 {
				t.Fatalf("exit code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
			if *loads != 0 {
				t.Fatalf("loaded configuration %d times", *loads)
			}
			if len(*calls) != 0 {
				t.Fatalf("dependency calls before usage error = %v", *calls)
			}
		})
	}
}

func TestLeafRoutingAndDryRunDefaults(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"saving-sessions", "join"}, want: "join:A-TEST:false"},
		{args: []string{"saving-sessions", "join", "--execute"}, want: "join:A-TEST:true"},
		{args: []string{"wheel", "spin"}, want: "spin:A-TEST:false:0"},
		{args: []string{"wheel", "spin", "--execute"}, want: "spin:A-TEST:true:0"},
		{args: []string{"wheel", "spin", "--max-spins", "1"}, want: "spin:A-TEST:false:1"},
		{args: []string{"wheel", "history", "--from", "2026-01-01", "--to", "2026-01-31", "--fuel", "gas"}, want: "history:A-TEST:2026-01-01:2026-01-31:GAS"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			deps, loads, calls := testDependencies()
			var stdout, stderr strings.Builder
			if code := Run(test.args, &stdout, &stderr, "dev", deps); code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if *loads != 1 {
				t.Fatalf("loaded configuration %d times", *loads)
			}
			if len(*calls) < 2 || (*calls)[len(*calls)-1] != test.want {
				t.Fatalf("calls = %v, want final call %q", *calls, test.want)
			}
			if strings.Contains(test.want, "join:") || strings.Contains(test.want, "spin:") {
				if !strings.Contains(stdout.String(), "result") {
					t.Fatalf("stdout = %q, want command result", stdout.String())
				}
				if stderr.Len() != 0 {
					t.Fatalf("stderr = %q, want no routine authentication message", stderr.String())
				}
			}
		})
	}
}

func TestBareGroupsPrintHelpWithoutConfiguration(t *testing.T) {
	for _, args := range [][]string{{"saving-sessions"}, {"wheel"}} {
		deps, loads, _ := testDependencies()
		var stdout, stderr strings.Builder
		if code := Run(args, &stdout, &stderr, "dev", deps); code != 0 {
			t.Fatalf("Run(%v) = %d, stderr = %q", args, code, stderr.String())
		}
		if *loads != 0 || stdout.Len() == 0 {
			t.Fatalf("Run(%v): loads = %d, help = %q", args, *loads, stdout.String())
		}
	}
}

func TestResultsStayOnStdoutWhenCommandFails(t *testing.T) {
	deps, _, _ := testDependencies()
	deps.RunSpin = func(_ context.Context, _ *octopus.Client, _ string, _ bool, _ int, results io.Writer) error {
		if _, err := fmt.Fprintln(results, "Wheel spun: fuel=ELECTRICITY prize_value=8"); err != nil {
			return err
		}
		return errors.New("allowance check failed")
	}
	var stdout, stderr strings.Builder
	if code := Run([]string{"wheel", "spin", "--execute"}, &stdout, &stderr, "dev", deps); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "Wheel spun:") {
		t.Fatalf("successful action result missing from stdout: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "Wheel spun:") || !strings.Contains(stderr.String(), "allowance check failed") {
		t.Fatalf("stderr = %q, want diagnostics without action result", stderr.String())
	}
}
