package savingsessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rogerhutchings/octopus-autojoin/internal/octopus"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestRunWithSharedClient(t *testing.T) {
	for _, test := range []struct {
		name               string
		execute, confirmed bool
		wantJoins          int
		wantError          bool
	}{
		{name: "dry run"},
		{name: "execute", execute: true, confirmed: true, wantJoins: 1},
		{name: "missing confirmation", execute: true, wantJoins: 1, wantError: true},
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
						body = fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":1,"code":"EVENT_TEST","startAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":{"signedUpMeterPoint":{"regionId":10},"hasJoinedCampaign":true,"joinedEvents":[]}}}}`, time.Now().Add(time.Hour).Format(time.RFC3339))
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			ctx := context.Background()
			if err := client.Authenticate(ctx, "test-api-key"); err != nil {
				t.Fatal(err)
			}
			err := Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), client, "A-TEST", test.execute)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError = %v", err, test.wantError)
			}
			if joins != test.wantJoins {
				t.Fatalf("joins = %d, want %d", joins, test.wantJoins)
			}
		})
	}
}
