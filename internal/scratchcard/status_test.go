package scratchcard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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
	for _, field := range []string{"octoplusActiveScratchcardData", "activeSession", "externalReference", "scratchcard", "status", "__typename", "startsAt", "endsAt"} {
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

const activeSessionJSON = `"activeSession":{"externalReference":"123e4567-e89b-12d3-a456-426614174000","startsAt":"2026-10-12T00:00:00Z","endsAt":"2026-10-13T00:00:00Z"}`
const playTestNow = "2026-10-12T12:00:00Z"

func TestRunPlayDryRunAndSuccess(t *testing.T) {
	status := `{"data":{"octoplusActiveScratchcardData":{` + activeSessionJSON + `,"scratchcard":null}}}`
	output, err, mutations, variables := callPlayAt(t, status, `{"data":{"scratchOctoplusScratchcard":{"scratchcard":{"externalReference":"123e4567-e89b-12d3-a456-426614174001","status":"PRIZE_NOT_YET_CLAIMED","offer":null,"prize":{"__typename":"OctoplusOfferType","name":"Coffee","description":"A free coffee"}}}}}`, false, nil)
	if err != nil || mutations != 0 || !strings.Contains(output, "Dry run: would attempt one Scratchcard play") {
		t.Fatalf("dry run output=%q err=%v mutations=%d", output, err, mutations)
	}
	if variables != nil {
		t.Fatalf("dry run mutation variables = %v", variables)
	}

	output, err, mutations, variables = callPlayAt(t, status, `{"data":{"scratchOctoplusScratchcard":{"scratchcard":{"externalReference":"123e4567-e89b-12d3-a456-426614174001","status":"PRIZE_NOT_YET_CLAIMED","offer":null,"prize":{"__typename":"OctoplusOfferType","name":"Coffee","description":"A free coffee"}}}}}`, true, nil)
	if err != nil || mutations != 1 {
		t.Fatalf("play output=%q err=%v mutations=%d", output, err, mutations)
	}
	for _, want := range []string{"Scratchcard play completed", "Prize not yet claimed", "Coffee — A free coffee", "claim this prize in the Octopus app"} {
		if !strings.Contains(output, want) {
			t.Errorf("output %q missing %q", output, want)
		}
	}
	if variables["input"].(map[string]any)["accountNumber"] != "A-TEST" || variables["input"].(map[string]any)["scratchcardSessionExternalReference"] != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("mutation variables = %v", variables)
	}
}

func TestRunPlayPrechecksNeverMutate(t *testing.T) {
	tests := []struct{ name, session, card, want string }{
		{"no active session", `"activeSession":null`, `"scratchcard":null`, "No Scratchcard session"},
		{"existing card", activeSessionJSON, `"scratchcard":{"status":"DID_NOT_WIN"}`, "already exists"},
		{"existing claimed card", activeSessionJSON, `"scratchcard":{"status":"PRIZE_CLAIMED"}`, "already exists"},
		{"expired", `"activeSession":{"externalReference":"123e4567-e89b-12d3-a456-426614174000","startsAt":"2026-10-10T00:00:00Z","endsAt":"2026-10-11T00:00:00Z"}`, `"scratchcard":null`, "expired"},
		{"future", `"activeSession":{"externalReference":"123e4567-e89b-12d3-a456-426614174000","startsAt":"2026-10-13T00:00:00Z","endsAt":"2026-10-14T00:00:00Z"}`, `"scratchcard":null`, "has not started"},
		{"malformed reference", `"activeSession":{"externalReference":"bad","startsAt":"2026-10-12T00:00:00Z","endsAt":"2026-10-13T00:00:00Z"}`, `"scratchcard":null`, "malformed session reference"},
		{"malformed timestamp", `"activeSession":{"externalReference":"123e4567-e89b-12d3-a456-426614174000","startsAt":"not-date","endsAt":"2026-10-13T00:00:00Z"}`, `"scratchcard":null`, "invalid timestamp"},
		{"missing timestamp", `"activeSession":{"externalReference":"123e4567-e89b-12d3-a456-426614174000","startsAt":null,"endsAt":"2026-10-13T00:00:00Z"}`, `"scratchcard":null`, "omitted required session dates"},
		{"malformed card", activeSessionJSON, `"scratchcard":{"offer":null}`, "omitted required status"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `{"data":{"octoplusActiveScratchcardData":{"` + strings.TrimPrefix(test.session, `"`) + `,"` + strings.TrimPrefix(test.card, `"`) + `}}}`
			out, err, mutations, _ := callPlayAt(t, body, `{}`, true, nil)
			if mutations != 0 {
				t.Fatalf("mutations = %d", mutations)
			}
			if test.name == "malformed card" || strings.Contains(test.name, "malformed") || strings.Contains(test.name, "missing timestamp") {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil || !strings.Contains(out, test.want) {
				t.Fatalf("output=%q err=%v, want %q", out, err, test.want)
			}
		})
	}
}

func TestRunPlayMutationFailuresNeverRetry(t *testing.T) {
	status := `{"data":{"octoplusActiveScratchcardData":{"` + strings.TrimPrefix(activeSessionJSON, `"`) + `,"scratchcard":null}}}`
	for _, test := range []struct {
		name, body, want string
		transport        bool
	}{
		{"GraphQL failure", `{"errors":[{"message":"rejected"}]}`, "may have completed", false},
		{"null mutation result", `{"data":{"scratchOctoplusScratchcard":null}}`, "may have completed", false},
		{"null card", `{"data":{"scratchOctoplusScratchcard":{"scratchcard":null}}}`, "may have completed", false},
		{"missing card field", `{"data":{"scratchOctoplusScratchcard":{"other":true}}}`, "may have completed", false},
		{"missing required card data", `{"data":{"scratchOctoplusScratchcard":{"scratchcard":{"status":"DID_NOT_WIN"}}}}`, "required card reference or status", false},
		{"ambiguous transport", "", "may have completed", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err, mutations, _ := callPlayAt(t, status, test.body, true, func() error {
				if test.transport {
					return errors.New("connection reset")
				}
				return nil
			}())
			if err == nil || !strings.Contains(err.Error(), test.want) || mutations != 1 {
				t.Fatalf("output=%q err=%v mutations=%d", out, err, mutations)
			}
		})
	}
}

func TestRunPlayMalformedDetailsDoNotPrintSuccess(t *testing.T) {
	status := `{"data":{"octoplusActiveScratchcardData":{"` + strings.TrimPrefix(activeSessionJSON, `"`) + `,"scratchcard":null}}}`
	for _, test := range []struct{ name, details string }{
		{"malformed offer", `"offer":"not-an-object","prize":null`},
		{"malformed prize", `"offer":null,"prize":{"__typename":""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"data":{"scratchOctoplusScratchcard":{"scratchcard":{"externalReference":"123e4567-e89b-12d3-a456-426614174001","status":"DID_NOT_WIN",` + test.details + `}}}}`
			out, err, mutations, _ := callPlayAt(t, status, body, true, nil)
			if err == nil || !strings.Contains(err.Error(), "check `octo-cli scratchcard status` before another attempt") {
				t.Fatalf("error = %v, want status-check guidance", err)
			}
			if mutations != 1 {
				t.Fatalf("mutations = %d, want exactly one", mutations)
			}
			if strings.Contains(out, "Scratchcard play completed") {
				t.Fatalf("printed success before response validation: %q", out)
			}
		})
	}
}

func TestRunPlayExistingUnclaimedCardShowsAppGuidanceWithoutMutation(t *testing.T) {
	status := `{"data":{"octoplusActiveScratchcardData":{"` + strings.TrimPrefix(activeSessionJSON, `"`) + `,"scratchcard":{"status":"PRIZE_NOT_YET_CLAIMED","offer":null,"prize":null}}}}`
	out, err, mutations, _ := callPlayAt(t, status, `{}`, true, nil)
	if err != nil || mutations != 0 {
		t.Fatalf("output=%q err=%v mutations=%d", out, err, mutations)
	}
	if !strings.Contains(out, "Inspect and claim this prize in the Octopus app.") {
		t.Fatalf("output = %q, want app claim guidance", out)
	}
}

func TestRunPlayGraphQLFetchFailureDoesNotMutate(t *testing.T) {
	_, err, mutations, _ := callPlayAt(t, `{"errors":[{"message":"status unavailable"}]}`, `{}`, true, nil)
	if err == nil || !strings.Contains(err.Error(), "status unavailable") || mutations != 0 {
		t.Fatalf("error=%v mutations=%d", err, mutations)
	}
}

func callPlayAt(t *testing.T, statusBody, playBody string, execute bool, transportErr error) (string, error, int, map[string]any) {
	t.Helper()
	mutations := 0
	var mutationVars map[string]any
	client := octopus.NewClient(&http.Client{Transport: statusRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == "https://api.octopus.energy/v1/graphql/" {
			return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"data":{"obtainKrakenToken":{"token":"test-token"}}}`)), Header: make(http.Header)}, nil
		}
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload.Query, "mutation ScratchOctoplusScratchcard") {
			mutations++
			mutationVars = payload.Variables
			if transportErr != nil {
				return nil, transportErr
			}
			return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(playBody)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(statusBody)), Header: make(http.Header)}, nil
	})})
	if err := client.Authenticate(context.Background(), "test-api-key"); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	err := runPlayAt(context.Background(), client, "A-TEST", execute, &output, mustTime(playTestNow))
	return output.String(), err, mutations, mutationVars
}

func mustTime(value string) time.Time { result, _ := time.Parse(time.RFC3339, value); return result }
