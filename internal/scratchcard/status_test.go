package scratchcard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rogerhutchings/octo-cli/internal/octopus"
)

func TestRunStatus(t *testing.T) {
	for _, test := range []struct {
		name, data, want string
		wantError        string
	}{
		{
			name: "no active session",
			data: `{"activeSession":null,"scratchcard":null}`,
			want: "No Scratchcard session is currently active.",
		},
		{
			name: "active session without card",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":null}`,
			want: "No available play is confirmed.",
		},
		{
			name: "did not win",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"DID_NOT_WIN","offer":null,"prize":null}}`,
			want: "Scratchcard status: Did not win.",
		},
		{
			name: "prize not yet claimed",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"PRIZE_NOT_YET_CLAIMED","offer":null,"prize":{"__typename":"OctoplusOfferType","name":"Coffee offer","description":"A sample offer"}}}`,
			want: "Prize not yet claimed",
		},
		{
			name: "prize claimed",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"PRIZE_CLAIMED","offer":null,"prize":{"__typename":"StampsAwardedType"}}}`,
			want: "Prize details: stamps-awarded type.",
		},
		{
			name: "prize rejected",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"PRIZE_REJECTED","offer":null,"prize":null}}`,
			want: "Prize rejected",
		},
		{
			name: "prize not claimed in time",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"PRIZE_NOT_CLAIMED_IN_TIME","offer":null,"prize":null}}`,
			want: "Prize not claimed in time",
		},
		{
			name: "unknown status",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"FUTURE_STATUS","offer":null,"prize":null}}`,
			want: `Unknown status "FUTURE_STATUS" (not interpreted)`,
		},
		{
			name: "null optional prize data and separate offer",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"DID_NOT_WIN","offer":{"name":"Octoplus offer","description":"Offer description"},"prize":null}}`,
			want: "Offer: Octoplus offer — Offer description.",
		},
		{
			name: "missing optional prize data",
			data: `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"DID_NOT_WIN","offer":null}}`,
			want: "Scratchcard status: Did not win.",
		},
		{
			name:      "missing required status",
			data:      `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"offer":null,"prize":null}}`,
			wantError: "omitted required status",
		},
		{
			name:      "missing required root fields",
			data:      `{"activeSession":null}`,
			wantError: "omitted session or Scratchcard fields",
		},
		{
			name:      "missing required session dates",
			data:      `{"activeSession":{},"scratchcard":null}`,
			wantError: "omitted required session dates",
		},
		{
			name:      "missing prize typename",
			data:      `{"activeSession":{"startsAt":"2026-10-05T00:00:00Z","endsAt":"2026-10-12T00:00:00Z"},"scratchcard":{"status":"PRIZE_CLAIMED","offer":null,"prize":{}}}`,
			wantError: "omitted the prize type",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err, requests, mutations := callStatus(t, `{"data":{"octoplusActiveScratchcardData":`+test.data+`}}`)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("RunStatus() error = %v, want %q", err, test.wantError)
				}
			} else if err != nil {
				t.Fatalf("RunStatus() error = %v", err)
			}
			if test.want != "" && !strings.Contains(output, test.want) {
				t.Errorf("output = %q, want substring %q", output, test.want)
			}
			if requests != 1 || mutations != 0 {
				t.Fatalf("requests = %d, mutations = %d; want one read and no mutation", requests, mutations)
			}
		})
	}
}

func TestRunStatusRejectsGraphQLAndMalformedResponses(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"GraphQL errors", `{"errors":[{"message":"status unavailable"}]}`, "GraphQL returned errors: status unavailable"},
		{"missing active data", `{"data":{}}`, "no active Scratchcard data"},
		{"null active data", `{"data":{"octoplusActiveScratchcardData":null}}`, "no active Scratchcard data"},
		{"malformed active data", `{"data":{"octoplusActiveScratchcardData":"bad"}}`, "decode active Scratchcard data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err, requests, mutations := callStatus(t, test.body)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("RunStatus() error = %v, want %q", err, test.want)
			}
			if requests != 1 || mutations != 0 {
				t.Fatalf("requests = %d, mutations = %d; want one read and no mutation", requests, mutations)
			}
		})
	}
}

func TestRunStatusQueryIsReadOnlyAndUsesConfirmedFields(t *testing.T) {
	var query string
	var variables map[string]any
	client := clientWithResponse(t, `{"data":{"octoplusActiveScratchcardData":{"activeSession":null,"scratchcard":null}}}`, func(request *http.Request) {
		if request.Method != http.MethodPost || request.URL.String() != "https://api.backend.octopus.energy/v1/graphql/" {
			t.Errorf("request = %s %s", request.Method, request.URL)
		}
		var payload struct {
			Query string         `json:"query"`
			Vars  map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		query, variables = payload.Query, payload.Vars
	})
	if err := RunStatus(context.Background(), client, "A-TEST", io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(query), "mutation") {
		t.Fatalf("status query contains a mutation: %s", query)
	}
	for _, field := range []string{"octoplusActiveScratchcardData", "activeSession", "scratchcard", "status", "__typename", "startsAt", "endsAt"} {
		if !strings.Contains(query, field) {
			t.Errorf("query does not select %q: %s", field, query)
		}
	}
	if got := variables["accountNumber"]; got != "A-TEST" {
		t.Errorf("accountNumber variable = %v, want A-TEST", got)
	}
}

func callStatus(t *testing.T, body string) (string, error, int, int) {
	t.Helper()
	requests, mutations := 0, 0
	client := clientWithResponse(t, body, func(request *http.Request) {
		requests++
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(payload.Query), "mutation") {
			mutations++
		}
	})
	var output strings.Builder
	err := RunStatus(context.Background(), client, "A-TEST", &output)
	return output.String(), err, requests, mutations
}

func clientWithResponse(t *testing.T, body string, inspect func(*http.Request)) *octopus.Client {
	t.Helper()
	client := octopus.NewClient(&http.Client{Transport: statusRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == "https://api.octopus.energy/v1/graphql/" {
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"data":{"obtainKrakenToken":{"token":"test-token"}}}`)), Header: make(http.Header)}, nil
		}
		inspect(request)
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	if err := client.Authenticate(context.Background(), "test-api-key"); err != nil {
		t.Fatal(err)
	}
	return client
}

type statusRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip statusRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
