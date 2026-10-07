package savingsessions

import (
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

func TestRunList(t *testing.T) {
	now := time.Now().UTC()
	date := func(offset time.Duration) string { return now.Add(offset).Format(time.RFC3339) }
	for _, test := range []struct {
		name, data, want string
		wantError        bool
	}{
		{
			name: "upcoming statuses and chronological order",
			data: fmt.Sprintf(`{"data":{"savingSessions":{"events":[
				{"id":1,"code":"LATER","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","capacityStatus":"AVAILABLE","targetRegion":[{"regionId":10}]},
				{"id":2,"code":"EARLIER","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","capacityStatus":"AVAILABLE","targetRegion":[{"regionId":10}]},
				{"id":3,"code":"OTHER_TYPE","startAt":%q,"endAt":%q,"eventType":"WEEKEND_HAPPY_HOUR","capacityStatus":"AVAILABLE","targetRegion":[]},
				{"id":4,"code":"FULL","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","capacityStatus":"FULL","targetRegion":[]},
				{"id":5,"code":"WRONG_REGION","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","capacityStatus":"AVAILABLE","targetRegion":[{"regionId":99}]},
				{"id":6,"code":"PAST","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","capacityStatus":"AVAILABLE","targetRegion":[]},
				{"id":7,"code":"JOINED","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","capacityStatus":"AVAILABLE","targetRegion":[]}
			],"account":{"signedUpMeterPoint":{"regionId":10},"hasJoinedCampaign":true,"joinedEvents":[{"eventId":7}]}}}}`,
				date(3*time.Hour), date(4*time.Hour), date(time.Hour), date(2*time.Hour), date(time.Hour), date(2*time.Hour), date(time.Hour), date(2*time.Hour), date(time.Hour), date(2*time.Hour), date(-2*time.Hour), date(-time.Hour), date(time.Hour), date(2*time.Hour)),
			want: "EARLIER",
		},
		{
			name: "unknown account information is not eligible",
			data: fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":10,"code":"UNKNOWN_ACCOUNT","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":{"signedUpMeterPoint":null,"joinedEvents":[]}}}}`, date(time.Hour), date(2*time.Hour)),
			want: "account region is unavailable",
		},
		{
			name: "null account has unknown eligibility",
			data: fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":15,"code":"NULL_ACCOUNT","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":null}}}`, date(time.Hour), date(2*time.Hour)),
			want: "joined status is unknown",
		},
		{
			name: "capacity and campaign are not join filters",
			data: fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":11,"code":"CURRENT_RULES","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","capacityStatus":"FULL","targetRegion":[]}],"account":{"signedUpMeterPoint":{"regionId":10},"hasJoinedCampaign":false,"joinedEvents":[]}}}}`, date(time.Hour), date(2*time.Hour)),
			want: "CURRENT_RULES",
		},
		{
			name: "missing joined events are unknown and ineligible",
			data: fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":12,"code":"UNKNOWN_JOINED","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":{"signedUpMeterPoint":{"regionId":10}}}}}`, date(time.Hour), date(2*time.Hour)),
			want: "joined status is unknown",
		},
		{
			name: "null joined events are unknown and ineligible",
			data: fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":13,"code":"NULL_JOINED","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":{"signedUpMeterPoint":{"regionId":10},"joinedEvents":null}}}}`, date(time.Hour), date(2*time.Hour)),
			want: "joined status is unknown",
		},
		{
			name: "empty joined events confirm not joined",
			data: fmt.Sprintf(`{"data":{"savingSessions":{"events":[{"id":14,"code":"EMPTY_JOINED","startAt":%q,"endAt":%q,"eventType":"TURN_DOWN","targetRegion":[]}],"account":{"signedUpMeterPoint":{"regionId":10},"joinedEvents":[]}}}}`, date(time.Hour), date(2*time.Hour)),
			want: "EMPTY_JOINED",
		},
		{name: "empty", data: `{"data":{"savingSessions":{"events":[],"account":{"signedUpMeterPoint":{"regionId":10},"joinedEvents":[]}}}}`, want: "No upcoming Saving Sessions found."},
		{name: "missing savingSessions", data: `{"data":{}}`, wantError: true},
		{name: "null savingSessions", data: `{"data":{"savingSessions":null}}`, wantError: true},
		{name: "missing events", data: `{"data":{"savingSessions":{}}}`, wantError: true},
		{name: "null events", data: `{"data":{"savingSessions":{"events":null}}}`, wantError: true},
		{name: "API failure", data: `{"errors":[{"message":"query failed"}]}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutations := 0
			client := octopus.NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var payload struct{ Query string }
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				body := `{"data":{"obtainKrakenToken":{"token":"test-token"}}}`
				if !strings.Contains(payload.Query, "obtainKrakenToken") {
					if strings.Contains(payload.Query, "mutation") {
						mutations++
					}
					body = test.data
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})})
			if err := client.Authenticate(context.Background(), "test-key"); err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			err := RunList(context.Background(), client, "A-TEST", &output)
			if (err != nil) != test.wantError {
				t.Fatalf("RunList() error = %v", err)
			}
			if test.want != "" && !strings.Contains(output.String(), test.want) {
				t.Fatalf("output = %q, want %q", output.String(), test.want)
			}
			if mutations != 0 {
				t.Fatalf("list sent %d mutations", mutations)
			}
			if test.name == "upcoming statuses and chronological order" {
				out := output.String()
				for _, want := range []string{"eligible", "ineligible: event type", "outside the account region", "JOINED", "already joined", "PAST"} {
					if strings.Contains(out, want) != (want != "PAST") {
						t.Fatalf("output %q has unexpected presence for %q", out, want)
					}
				}
				if strings.Index(out, "EARLIER") > strings.Index(out, "LATER") {
					t.Fatalf("sessions are not chronological: %q", out)
				}
			}
			if test.name == "missing joined events are unknown and ineligible" || test.name == "null joined events are unknown and ineligible" {
				if !strings.Contains(output.String(), "unknown") || !strings.Contains(output.String(), "joined status is unknown") {
					t.Fatalf("unknown joined status was not explained: %q", output.String())
				}
			}
			if test.name == "capacity and campaign are not join filters" && !strings.Contains(output.String(), "eligible") {
				t.Fatalf("list broadened join eligibility: %q", output.String())
			}
			if test.name == "capacity and campaign are not join filters" && strings.Contains(output.String(), "ineligible") {
				t.Fatalf("capacity or campaign changed current join eligibility: %q", output.String())
			}
			if test.name == "null account has unknown eligibility" {
				lines := strings.Split(strings.TrimSpace(output.String()), "\n")
				if len(lines) != 2 {
					t.Fatalf("output = %q, want header and one upcoming session", output.String())
				}
				fields := strings.Fields(lines[1])
				if len(fields) < 10 || fields[0] != "NULL_ACCOUNT" || fields[7] != "TURN_DOWN" || fields[8] != "unknown" || fields[9] != "ineligible:" {
					t.Fatalf("null account row = %q, want unknown joined status and ineligible", lines[1])
				}
				if !strings.Contains(lines[1], "account region is unavailable") {
					t.Fatalf("null account row did not explain unavailable region or was marked eligible: %q", lines[1])
				}
			}
		})
	}
}
