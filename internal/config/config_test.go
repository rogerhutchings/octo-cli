package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigEnvironmentOverridesDotEnv(t *testing.T) {
	temporaryDirectory := chdirTemporaryDirectory(t)
	if err := os.WriteFile(filepath.Join(temporaryDirectory, ".env"), []byte(
		"OCTOPUS_API_KEY=dotenv-key\nOCTOPUS_ACCOUNT_NUMBER=dotenv-account\n",
	), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OCTOPUS_API_KEY", "environment-key")
	t.Setenv("OCTOPUS_ACCOUNT_NUMBER", "environment-account")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if got.APIKey != "environment-key" {
		t.Errorf("apiKey = %q, want environment value", got.APIKey)
	}
	if got.AccountNumber != "environment-account" {
		t.Errorf("accountNumber = %q, want environment value", got.AccountNumber)
	}
}

func TestLoadConfigUsesDotEnvWhenEnvironmentIsUnset(t *testing.T) {
	temporaryDirectory := chdirTemporaryDirectory(t)
	if err := os.WriteFile(filepath.Join(temporaryDirectory, ".env"), []byte(
		"OCTOPUS_API_KEY=dotenv-key\nOCTOPUS_ACCOUNT_NUMBER=dotenv-account\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	unsetEnvironment(t, "OCTOPUS_API_KEY")
	unsetEnvironment(t, "OCTOPUS_ACCOUNT_NUMBER")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if got.APIKey != "dotenv-key" || got.AccountNumber != "dotenv-account" {
		t.Fatalf("Load() = %+v, want values from .env", got)
	}
}

func TestLoadConfigRequiresBothValues(t *testing.T) {
	tests := []struct {
		name    string
		missing string
		present string
	}{
		{
			name:    "missing API key",
			missing: "OCTOPUS_API_KEY",
			present: "OCTOPUS_ACCOUNT_NUMBER",
		},
		{
			name:    "missing account number",
			missing: "OCTOPUS_ACCOUNT_NUMBER",
			present: "OCTOPUS_API_KEY",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chdirTemporaryDirectory(t)
			unsetEnvironment(t, test.missing)
			t.Setenv(test.present, "present")

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.missing) {
				t.Fatalf(
					"Load() error = %v, want error mentioning %s",
					err,
					test.missing,
				)
			}
		})
	}
}

func chdirTemporaryDirectory(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporaryDirectory := t.TempDir()
	if err := os.Chdir(temporaryDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	return temporaryDirectory
}

func unsetEnvironment(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			if err := os.Setenv(name, previous); err != nil {
				t.Errorf("restore %s: %v", name, err)
			}
		} else if err := os.Unsetenv(name); err != nil {
			t.Errorf("unset %s: %v", name, err)
		}
	})
}
