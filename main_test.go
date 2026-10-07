package main

import (
	"errors"
	"flag"
	"io"
	"testing"
)

func TestParseOptions(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		expected  options
		wantError bool
	}{
		{name: "legacy dry run", expected: options{command: "saving-sessions"}},
		{name: "legacy execute", arguments: []string{"--execute"}, expected: options{command: "saving-sessions", execute: true}},
		{name: "legacy single dash", arguments: []string{"-execute"}, expected: options{command: "saving-sessions", execute: true}},
		{name: "explicit saving sessions", arguments: []string{"saving-sessions", "--execute"}, expected: options{command: "saving-sessions", execute: true}},
		{name: "wheel dry run", arguments: []string{"wheel-of-fortune"}, expected: options{command: "wheel-of-fortune"}},
		{name: "wheel execute", arguments: []string{"wheel-of-fortune", "--execute"}, expected: options{command: "wheel-of-fortune", execute: true}},
		{name: "history without filters", arguments: []string{"wheel-of-fortune-history"}, expected: options{command: "wheel-of-fortune-history"}},
		{name: "history filters", arguments: []string{"wheel-of-fortune-history", "--from", "2026-01-01", "--to", "2026-01-31", "--fuel", "gas"}, expected: options{command: "wheel-of-fortune-history", fromDate: "2026-01-01", toDate: "2026-01-31", fuel: "gas"}},
		{name: "history execute rejected", arguments: []string{"wheel-of-fortune-history", "--execute"}, wantError: true},
		{name: "history invalid fuel", arguments: []string{"wheel-of-fortune-history", "--fuel", "electric"}, wantError: true},
		{name: "history invalid date", arguments: []string{"wheel-of-fortune-history", "--from", "2026-1-01"}, wantError: true},
		{name: "history reversed dates", arguments: []string{"wheel-of-fortune-history", "--from", "2026-02-01", "--to", "2026-01-31"}, wantError: true},
		{name: "history options rejected for other command", arguments: []string{"wheel-of-fortune", "--fuel", "gas"}, wantError: true},
		{name: "version", arguments: []string{"--version"}, expected: options{command: "saving-sessions", showVersion: true}},
		{name: "unknown command", arguments: []string{"scratchcards"}, wantError: true},
		{name: "unknown flag", arguments: []string{"wheel-of-fortune", "--execut"}, wantError: true},
		{name: "misplaced command", arguments: []string{"--execute", "wheel-of-fortune"}, wantError: true},
		{name: "extra argument", arguments: []string{"wheel-of-fortune", "--execute", "gas"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseOptions(test.arguments, io.Discard)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError = %v", err, test.wantError)
			}
			if !test.wantError && got != test.expected {
				t.Fatalf("got %+v, want %+v", got, test.expected)
			}
		})
	}
}

func TestHelp(t *testing.T) {
	_, err := parseOptions([]string{"--help"}, io.Discard)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error = %v, want help", err)
	}
}
