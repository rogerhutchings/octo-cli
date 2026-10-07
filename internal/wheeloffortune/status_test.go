package wheeloffortune

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
		name, response, want string
		wantError            bool
	}{
		{name: "available spins", response: `{"data":{"electricity":{"spinsAllowed":2},"gas":{"spinsAllowed":1}}}`, want: "2 electricity spins, 1 gas spin"},
		{name: "zero spins", response: `{"data":{"electricity":{"spinsAllowed":0},"gas":{"spinsAllowed":0}}}`, want: "No available spins."},
		{name: "API failure", response: `{"errors":[{"message":"allowance unavailable"}]}`, want: "", wantError: true},
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
					body = test.response
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})})
			if err := client.Authenticate(context.Background(), "test-key"); err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			err := RunStatus(context.Background(), client, "A-TEST", &output)
			if (err != nil) != test.wantError {
				t.Fatalf("RunStatus() error = %v", err)
			}
			if test.want != "" && !strings.Contains(output.String(), test.want) {
				t.Fatalf("output = %q, want %q", output.String(), test.want)
			}
			if strings.Contains(output.String(), "Dry run") || mutations != 0 {
				t.Fatalf("status suggested/executed a mutation: output=%q mutations=%d", output.String(), mutations)
			}
		})
	}
}
