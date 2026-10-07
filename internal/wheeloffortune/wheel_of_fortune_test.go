package wheeloffortune

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rogerhutchings/octo-cli/internal/octopus"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestRun(t *testing.T) {
	for _, test := range []struct {
		name              string
		execute           bool
		maxSpins          int
		electricity, gas  int
		prizeJSON         string
		spinResponse      string
		allowanceResponse string
		staleCount        bool
		failAfterSpin     bool
		wantSpins         int
		wantError         string
	}{
		{name: "dry run never spins", electricity: 2, gas: 2},
		{name: "no spins", execute: true},
		{name: "both fuels", execute: true, electricity: 2, gas: 2, prizeJSON: `{"value":8}`, wantSpins: 4},
		{name: "limit spans both fuels", execute: true, maxSpins: 1, electricity: 2, gas: 2, prizeJSON: `{"value":8}`, wantSpins: 1},
		{name: "limit above allowance", execute: true, maxSpins: 10, electricity: 1, gas: 1, prizeJSON: `{"value":8}`, wantSpins: 2},
		{name: "limited dry run plans capped total", maxSpins: 1, electricity: 2, gas: 2, wantSpins: 0},
		{name: "only electricity", execute: true, electricity: 2, prizeJSON: `{"value":"8"}`, wantSpins: 2},
		{name: "only gas", execute: true, gas: 2, prizeJSON: `{"value":0}`, wantSpins: 2},
		{name: "null prize is a successful spin", execute: true, electricity: 1, prizeJSON: `null`, wantSpins: 1},
		{name: "null value is a successful spin", execute: true, electricity: 1, prizeJSON: `{"value":null}`, wantSpins: 1},
		{name: "stale count stops loop", execute: true, electricity: 2, prizeJSON: `{"value":8}`, staleCount: true, wantSpins: 1, wantError: "did not decrease"},
		{name: "mutation failure is not retried", execute: true, electricity: 2, gas: 2, spinResponse: `{"errors":[{"message":"spin failed"}]}`, wantSpins: 1, wantError: "not retried"},
		{name: "partial data with errors is not success", execute: true, electricity: 2, spinResponse: `{"data":{"spinWheelOfFortune":{"prize":{"value":8}}},"errors":[{"message":"partial failure"}]}`, wantSpins: 1, wantError: "partial failure"},
		{name: "null mutation result", execute: true, electricity: 2, spinResponse: `{"data":{"spinWheelOfFortune":null}}`, wantSpins: 1, wantError: "no spin result"},
		{name: "read failure after spin stops run", execute: true, electricity: 2, gas: 2, prizeJSON: `{"value":8}`, failAfterSpin: true, wantSpins: 1, wantError: "check allowance after"},
		{name: "missing allowance", execute: true, allowanceResponse: `{"data":{"electricity":{"spinsAllowed":2}}}`, wantError: "invalid spin allowances"},
		{name: "null allowance", execute: true, allowanceResponse: `{"data":{"electricity":null,"gas":{"spinsAllowed":0}}}`, wantError: "invalid spin allowances"},
		{name: "null count", execute: true, allowanceResponse: `{"data":{"electricity":{"spinsAllowed":null},"gas":{"spinsAllowed":0}}}`, wantError: "invalid spin allowances"},
		{name: "negative allowance", execute: true, electricity: -1, wantError: "invalid spin allowances"},
		{name: "missing data", execute: true, allowanceResponse: `{}`, wantError: "no data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			electricity, gas := test.electricity, test.gas
			spinCount, checks, authentications := 0, 0, 0
			client := octopus.NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("unexpected HTTP request: %s, %v", request.Method, request.Header)
				}
				var payload struct {
					Query     string
					Variables map[string]any
				}
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				body := ""
				if strings.Contains(payload.Query, "obtainKrakenToken") {
					authentications++
					if request.URL.Host != "api.octopus.energy" || request.Header.Get("Authorization") != "" {
						t.Fatal("incorrect auth endpoint/headers")
					}
					input := payload.Variables["input"].(map[string]any)
					if input["APIKey"] != "test-api-key" {
						t.Fatalf("incorrect auth variables: %v", input)
					}
					body = `{"data":{"obtainKrakenToken":{"token":"test-token"}}}`
				} else {
					if request.URL.Host != "api.backend.octopus.energy" || request.Header.Get("Authorization") != "test-token" {
						t.Fatal("incorrect backend endpoint/token")
					}
					if !strings.Contains(payload.Query, `accountNumber: "A-TEST"`) {
						t.Fatal("account number missing")
					}
					if strings.Contains(payload.Query, "wheelOfFortuneSpinsAllowed") {
						checks++
						if test.failAfterSpin && spinCount > 0 {
							return nil, fmt.Errorf("connection lost")
						}
						body = fmt.Sprintf(`{"data":{"electricity":{"spinsAllowed":%d},"gas":{"spinsAllowed":%d}}}`, electricity, gas)
						if test.allowanceResponse != "" {
							body = test.allowanceResponse
						}
					} else if strings.Contains(payload.Query, "spinWheelOfFortune") {
						spinCount++
						if !test.staleCount {
							if strings.Contains(payload.Query, "fuelType: ELECTRICITY") {
								if electricity <= 0 {
									t.Fatal("spun electricity without an allowance")
								}
								electricity--
							} else if strings.Contains(payload.Query, "fuelType: GAS") {
								if gas <= 0 {
									t.Fatal("spun gas without an allowance")
								}
								gas--
							} else {
								t.Fatal("missing fuel type")
							}
						}
						body = fmt.Sprintf(`{"data":{"spinWheelOfFortune":{"prize":%s}}}`, test.prizeJSON)
						if test.spinResponse != "" {
							body = test.spinResponse
						}
					} else {
						t.Fatalf("unexpected operation: %s", payload.Query)
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})})
			ctx := context.Background()
			if err := client.Authenticate(ctx, "test-api-key"); err != nil {
				t.Fatal(err)
			}
			var results bytes.Buffer
			err := Run(ctx, client, "A-TEST", test.execute, test.maxSpins, &results)
			if test.wantError == "" && err != nil {
				t.Fatalf("Run() error: %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if spinCount != test.wantSpins {
				t.Fatalf("performed %d spins, want %d", spinCount, test.wantSpins)
			}
			if authentications != 1 {
				t.Fatalf("authenticated %d times", authentications)
			}
			if test.wantError == "" && checks != 1+spinCount {
				t.Fatalf("checked %d times for %d spins", checks, spinCount)
			}
			if test.prizeJSON == `{"value":"8"}` && !strings.Contains(results.String(), "prize_value=8") {
				t.Fatalf("prize missing from result output: %s", results.String())
			}
			if test.name == "dry run never spins" && !strings.Contains(results.String(), "Dry run:") {
				t.Fatalf("dry-run result missing: %s", results.String())
			}
			if test.name == "limited dry run plans capped total" && !strings.Contains(results.String(), "would use at most 1 of the available spins") {
				t.Fatalf("limited dry-run plan missing: %s", results.String())
			}
			if test.name == "no spins" && !strings.Contains(results.String(), "No available spins") {
				t.Fatalf("no-action result missing: %s", results.String())
			}
			if test.name == "read failure after spin stops run" && !strings.Contains(results.String(), "Wheel spun:") {
				t.Fatalf("successful spin missing before later failure: %s", results.String())
			}
		})
	}
}
