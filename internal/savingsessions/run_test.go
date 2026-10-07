package savingsessions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rogerhutchings/octo-cli/internal/octopus"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestRunWithSharedClient(t *testing.T) {
	for _, test := range []struct {
		name               string
		execute, confirmed bool
		unknownJoined      bool
		nullAccount        bool
		wantJoins          int
		wantError          bool
	}{
		{name: "dry run"},
		{name: "execute", execute: true, confirmed: true, wantJoins: 1},
		{name: "missing confirmation", execute: true, wantJoins: 1, wantError: true},
		{name: "unknown joined status", execute: true, unknownJoined: true, wantError: true},
		{name: "null account prevents join", execute: true, nullAccount: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			joins := 0
			client := octopus.NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var payload struct{ Query string }
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				body := ""
				if strings.Contains(payload.Query, "obtainKrakenToken") {
					body = `{"data":{"obtainKrakenToken":{"token":"test-token"}}}`
				} else {
					if request.URL.Host != "api.backend.octopus.energy" || request.Header.Get("Authorization") != "test-token" {
						t.Fatal("incorrect backend endpoint/token")
					}
					if !strings.Contains(payload.Query, `accountNumber: "A-TEST"`) {
						t.Fatal("account missing")
					}
					if strings.Contains(payload.Query, "joinSavingSessionsEvent") {
						joins++
						if !strings.Contains(payload.Query, `eventCode: "EVENT_TEST"`) {
							t.Fatal("event code missing")
						}
						body = `{"data":{"joinSavingSessionsEvent":{"joinedEventCodes":[]}}}`
						if test.confirmed {
							body = `{"data":{"joinSavingSessionsEvent":{"joinedEventCodes":["EVENT_TEST"]}}}`
						}
					} else {
						if test.nullAccount {
							body = fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":1,"code":"EVENT_TEST","startAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":null}}}`, time.Now().Add(time.Hour).Format(time.RFC3339))
						} else {
							joinedEvents := `[]`
							if test.unknownJoined {
								joinedEvents = `null`
							}
							body = fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":1,"code":"EVENT_TEST","startAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":{"signedUpMeterPoint":{"regionId":10},"joinedEvents":%s}}}}`, time.Now().Add(time.Hour).Format(time.RFC3339), joinedEvents)
						}
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			ctx := context.Background()
			if err := client.Authenticate(ctx, "test-api-key"); err != nil {
				t.Fatal(err)
			}
			var results bytes.Buffer
			err := Run(ctx, client, "A-TEST", test.execute, &results)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError = %v", err, test.wantError)
			}
			if joins != test.wantJoins {
				t.Fatalf("joins = %d, want %d", joins, test.wantJoins)
			}
			if !test.unknownJoined && !test.nullAccount && !strings.Contains(results.String(), "Eligible Saving Session:") {
				t.Fatalf("candidate result missing from output: %q", results.String())
			}
			if test.unknownJoined && (joins != 0 || strings.Contains(results.String(), "Eligible Saving Session:")) {
				t.Fatalf("unknown joined status was treated as eligible: joins=%d output=%q", joins, results.String())
			}
			if test.nullAccount {
				if joins != 0 || strings.Contains(results.String(), "Eligible Saving Session:") || err == nil || !strings.Contains(err.Error(), "account data is unavailable") {
					t.Fatalf("null account did not fail safely: joins=%d error=%v output=%q", joins, err, results.String())
				}
			}
			if test.execute && test.confirmed && !strings.Contains(results.String(), "Joined Saving Session:") {
				t.Fatalf("joined result missing from output: %q", results.String())
			}
			if !test.execute && (!strings.Contains(results.String(), "Dry run:") || !strings.Contains(results.String(), "--execute to join eligible sessions")) {
				t.Fatalf("actionable dry-run result missing from output: %q", results.String())
			}
		})
	}
}

func TestSuccessfulJoinResultPreservedOnLaterFailure(t *testing.T) {
	joins := 0
	client := octopus.NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload struct{ Query string }
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		body := ""
		if strings.Contains(payload.Query, "obtainKrakenToken") {
			body = `{"data":{"obtainKrakenToken":{"token":"test-token"}}}`
		} else if strings.Contains(payload.Query, "joinSavingSessionsEvent") {
			joins++
			if strings.Contains(payload.Query, `eventCode: "EVENT_1"`) {
				body = `{"data":{"joinSavingSessionsEvent":{"joinedEventCodes":["EVENT_1"]}}}`
			} else {
				body = `{"data":{"joinSavingSessionsEvent":{"joinedEventCodes":[]}}}`
			}
		} else {
			startAt := time.Now().Add(time.Hour).Format(time.RFC3339)
			body = fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":1,"code":"EVENT_1","startAt":%q,"eventType":"TURN_DOWN","targetRegion":[]},{"id":2,"code":"EVENT_2","startAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":{"signedUpMeterPoint":{"regionId":10},"hasJoinedCampaign":true,"joinedEvents":[]}}}}`, startAt, startAt)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	ctx := context.Background()
	if err := client.Authenticate(ctx, "test-api-key"); err != nil {
		t.Fatal(err)
	}
	var results bytes.Buffer
	err := Run(ctx, client, "A-TEST", true, &results)
	if err == nil || !strings.Contains(err.Error(), "did not confirm") {
		t.Fatalf("error = %v, want join confirmation failure", err)
	}
	if joins != 2 {
		t.Fatalf("attempted %d joins, want 2", joins)
	}
	if !strings.Contains(results.String(), "Joined Saving Session: id=1 code=EVENT_1") {
		t.Fatalf("successful first join missing from output: %q", results.String())
	}
	if strings.Contains(results.String(), "Joined Saving Session: id=2 code=EVENT_2") {
		t.Fatalf("failed second join was reported as successful: %q", results.String())
	}
}

func TestNoEligibleSessionsWritesNoActionResult(t *testing.T) {
	client := octopus.NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload struct{ Query string }
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		body := `{"data":{"obtainKrakenToken":{"token":"test-token"}}}`
		if !strings.Contains(payload.Query, "obtainKrakenToken") {
			body = `{"data":{"savingSessions":{"events":[],"account":{"signedUpMeterPoint":{"regionId":10},"hasJoinedCampaign":true,"joinedEvents":[]}}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	ctx := context.Background()
	if err := client.Authenticate(ctx, "test-api-key"); err != nil {
		t.Fatal(err)
	}
	var results bytes.Buffer
	if err := Run(ctx, client, "A-TEST", true, &results); err != nil {
		t.Fatal(err)
	}
	if got := results.String(); !strings.Contains(got, "No eligible Saving Sessions found.") {
		t.Fatalf("no-action result missing from output: %q", got)
	} else if strings.Contains(got, "--execute") {
		t.Fatalf("empty result suggests an action: %q", got)
	}
}
