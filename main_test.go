package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	got, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() returned unexpected error: %v", err)
	}
	if got.apiKey != "environment-key" {
		t.Errorf("apiKey = %q, want environment value", got.apiKey)
	}
	if got.accountNumber != "environment-account" {
		t.Errorf("accountNumber = %q, want environment value", got.accountNumber)
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

	got, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() returned unexpected error: %v", err)
	}
	if got.apiKey != "dotenv-key" || got.accountNumber != "dotenv-account" {
		t.Fatalf("loadConfig() = %+v, want values from .env", got)
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

			_, err := loadConfig()
			if err == nil || !strings.Contains(err.Error(), test.missing) {
				t.Fatalf(
					"loadConfig() error = %v, want error mentioning %s",
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

func TestFindCandidateSavingSessions(t *testing.T) {
	now := time.Date(
		2026,
		time.September,
		30,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	const accountRegionID int64 = 10

	tests := []struct {
		name              string
		event             savingSessionEvent
		joinedEventIDs    []int64
		expectedCandidate bool
	}{
		{
			name: "future session in matching region",
			event: savingSessionEvent{
				ID:        1,
				Code:      "MATCHING_REGION",
				StartAt:   now.Add(time.Hour),
				EndAt:     now.Add(2 * time.Hour),
				EventType: savingSessionEventType,
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			expectedCandidate: true,
		},
		{
			name: "future unrestricted session",
			event: savingSessionEvent{
				ID:           2,
				Code:         "UNRESTRICTED",
				StartAt:      now.Add(time.Hour),
				EndAt:        now.Add(2 * time.Hour),
				EventType:    savingSessionEventType,
				TargetRegion: nil,
			},
			expectedCandidate: true,
		},
		{
			name: "past session",
			event: savingSessionEvent{
				ID:        3,
				Code:      "PAST",
				StartAt:   now.Add(-2 * time.Hour),
				EndAt:     now.Add(-time.Hour),
				EventType: savingSessionEventType,
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			expectedCandidate: false,
		},
		{
			name: "already joined session",
			event: savingSessionEvent{
				ID:        4,
				Code:      "ALREADY_JOINED",
				StartAt:   now.Add(time.Hour),
				EndAt:     now.Add(2 * time.Hour),
				EventType: savingSessionEventType,
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			joinedEventIDs:    []int64{4},
			expectedCandidate: false,
		},
		{
			name: "session in different region",
			event: savingSessionEvent{
				ID:        5,
				Code:      "WRONG_REGION",
				StartAt:   now.Add(time.Hour),
				EndAt:     now.Add(2 * time.Hour),
				EventType: savingSessionEventType,
				TargetRegion: []targetRegion{
					{RegionID: 99},
				},
			},
			expectedCandidate: false,
		},
		{
			name: "non turn down event",
			event: savingSessionEvent{
				ID:        6,
				Code:      "HAPPY_HOUR",
				StartAt:   now.Add(time.Hour),
				EndAt:     now.Add(2 * time.Hour),
				EventType: "WEEKEND_HAPPY_HOUR",
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			expectedCandidate: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			savingSessions := savingSessionsData{}

			savingSessions.SavingSessions.Account.SignedUpMeterPoint = &struct {
				RegionID int64 `json:"regionId"`
			}{
				RegionID: accountRegionID,
			}

			savingSessions.SavingSessions.Events = []savingSessionEvent{
				test.event,
			}

			for _, joinedEventID := range test.joinedEventIDs {
				savingSessions.SavingSessions.Account.JoinedEvents = append(
					savingSessions.SavingSessions.Account.JoinedEvents,
					joinedSavingSession{
						EventID: joinedEventID,
					},
				)
			}

			candidates, err := findCandidateSavingSessions(
				savingSessions,
				now,
			)
			if err != nil {
				t.Fatalf(
					"findCandidateSavingSessions() returned unexpected error: %v",
					err,
				)
			}

			if test.expectedCandidate && len(candidates) != 1 {
				t.Fatalf(
					"findCandidateSavingSessions() returned %d candidates, want 1",
					len(candidates),
				)
			}

			if !test.expectedCandidate && len(candidates) != 0 {
				t.Fatalf(
					"findCandidateSavingSessions() returned %d candidates, want 0",
					len(candidates),
				)
			}
		})
	}
}

func TestFindCandidateSavingSessionsWithoutSignedUpMeterPoint(t *testing.T) {
	savingSessions := savingSessionsData{}

	_, err := findCandidateSavingSessions(
		savingSessions,
		time.Now(),
	)

	if err == nil {
		t.Fatal(
			"findCandidateSavingSessions() returned no error, want missing meter point error",
		)
	}
}
