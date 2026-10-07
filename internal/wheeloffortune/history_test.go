package wheeloffortune

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rogerhutchings/octopus-autojoin/internal/octopus"
)

func TestRunHistory(t *testing.T) {
	for _, test := range []struct {
		name       string
		filter     HistoryFilter
		pages      map[string]string
		wantOutput []string
		wantError  string
	}{
		{
			name:   "follows all pages and sorts newest first",
			filter: HistoryFilter{From: "2026-01-01", To: "2026-01-31", Fuel: "ELECTRICITY"},
			pages: map[string]string{
				"":   `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c1","node":{"reference":"one","spunAt":"2026-01-03T10:00:00Z","prizeType":"OCTOPOINTS","incentiveType":"WHEEL","prize":{"value":5,"display":"5 points"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`,
				"c1": `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c2","node":{"reference":"two","spunAt":"2026-01-04T12:00:00Z","prizeType":"CREDIT","prize":{"value":100,"display":"£1 credit"}}},{"cursor":"c3","node":{"reference":"three","spunAt":null,"prizeType":null,"prize":null}}],"pageInfo":{"hasNextPage":false,"endCursor":"c3"}}}}`,
			},
			wantOutput: []string{"2026-01-04 12:00:00 UTC", "£1 credit", "2026-01-03 10:00:00 UTC", "5 points", "(unknown)", "(no prize)"},
		},
		{
			name:       "empty history",
			pages:      map[string]string{"": `{"data":{"wheelOfFortuneSpinHistory":{"edges":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`},
			wantOutput: []string{"No Wheel of Fortune history found."},
		},
		{
			name: "page error discards incomplete output",
			pages: map[string]string{
				"":   `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c1","node":{"reference":"one","spunAt":"2026-01-03T10:00:00Z","prizeType":"CREDIT","prize":{"display":"£1"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`,
				"c1": `{"data":{"wheelOfFortuneSpinHistory":null},"errors":[{"message":"page failed"}]}`,
			},
			wantError: "page failed",
		},
		{
			name: "missing hasNextPage after first page is an error",
			pages: map[string]string{
				"":   `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c1","node":{"reference":"one","spunAt":"2026-01-03T10:00:00Z","prizeType":"CREDIT","prize":{"display":"£1"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`,
				"c1": `{"data":{"wheelOfFortuneSpinHistory":{"edges":[],"pageInfo":{"endCursor":null}}}}`,
			},
			wantError: "incomplete history page",
		},
		{
			name: "null hasNextPage after first page is an error",
			pages: map[string]string{
				"":   `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c1","node":{"reference":"one","spunAt":"2026-01-03T10:00:00Z","prizeType":"CREDIT","prize":{"display":"£1"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`,
				"c1": `{"data":{"wheelOfFortuneSpinHistory":{"edges":[],"pageInfo":{"hasNextPage":null,"endCursor":null}}}}`,
			},
			wantError: "incomplete history page",
		},
		{
			name:      "missing next cursor is an error",
			pages:     map[string]string{"": `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c1","node":{"reference":"one"}}],"pageInfo":{"hasNextPage":true,"endCursor":null}}}}`},
			wantError: "without an end cursor",
		},
		{
			name: "repeated pagination cursor is an error",
			pages: map[string]string{
				"":   `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c1","node":{"reference":"one"}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`,
				"c1": `{"data":{"wheelOfFortuneSpinHistory":{"edges":[{"cursor":"c2","node":{"reference":"two"}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`,
			},
			wantError: "repeated a history page cursor",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests, authentications := 0, 0
			client := octopus.NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var payload struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if request.URL.Host == "api.octopus.energy" {
					authentications++
					body := `{"data":{"obtainKrakenToken":{"token":"history-token"}}}`
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				}
				requests++
				if request.URL.Host != "api.backend.octopus.energy" || request.Header.Get("Authorization") != "history-token" {
					t.Fatalf("unexpected endpoint or authorization: %s %q", request.URL, request.Header.Get("Authorization"))
				}
				if strings.Contains(strings.ToLower(payload.Query), "mutation") || strings.Contains(payload.Query, "spinWheelOfFortune(") {
					t.Fatalf("history made a mutation: %s", payload.Query)
				}
				if payload.Variables["accountNumber"] != "A-TEST" {
					t.Fatalf("account number missing: %#v", payload.Variables)
				}
				if test.filter.Fuel != "" {
					if payload.Variables["fuelType"] != test.filter.Fuel || payload.Variables["dateFrom"] != test.filter.From || payload.Variables["dateTo"] != test.filter.To {
						t.Fatalf("filters missing: %#v", payload.Variables)
					}
				} else if _, exists := payload.Variables["fuelType"]; exists {
					t.Fatalf("default request should omit fuel filter: %#v", payload.Variables)
				}
				cursor, _ := payload.Variables["after"].(string)
				body, exists := test.pages[cursor]
				if !exists {
					t.Fatalf("unexpected page cursor %q", cursor)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})})
			if err := client.Authenticate(context.Background(), "test-api-key"); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := RunHistory(context.Background(), client, "A-TEST", test.filter, &output)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want %q", err, test.wantError)
				}
				if output.Len() != 0 {
					t.Fatalf("printed incomplete history: %s", output.String())
				}
			} else {
				if err != nil {
					t.Fatalf("RunHistory() error: %v", err)
				}
				for _, expected := range test.wantOutput {
					if !strings.Contains(output.String(), expected) {
						t.Errorf("output %q does not contain %q", output.String(), expected)
					}
				}
				if test.name == "follows all pages and sorts newest first" && strings.Index(output.String(), "2026-01-04") > strings.Index(output.String(), "2026-01-03") {
					t.Fatalf("history is not newest first: %s", output.String())
				}
			}
			if expectedRequests := len(test.pages); requests != expectedRequests {
				t.Fatalf("made %d requests, want %d", requests, expectedRequests)
			}
			if authentications != 1 {
				t.Fatalf("authenticated %d times, want 1", authentications)
			}
		})
	}
}
