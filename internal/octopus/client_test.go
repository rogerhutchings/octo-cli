package octopus

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestAuthenticationRejectsInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name, body, wantError string
		status                int
	}{
		{"empty token", `{"data":{"obtainKrakenToken":{"token":""}}}`, "empty Kraken token", 200},
		{"null data", `{"data":null}`, "no data", 200},
		{"invalid JSON", `<html>failure</html>`, "decode GraphQL response", 200},
		{"auth error with key", `{"errors":[{"message":"invalid test-secret"}]}`, "invalid [redacted]", 200},
		{"HTTP error body is omitted", `test-secret`, "503", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Status: http.StatusText(test.status), Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})})
			err := client.Authenticate(context.Background(), "test-secret")
			expected := test.wantError
			if test.status == 503 {
				expected = "Service Unavailable"
			}
			if err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("error = %v, want %q", err, expected)
			}
			if strings.Contains(err.Error(), "test-secret") {
				t.Fatal("API key leaked")
			}
			if client.token != "" {
				t.Fatal("failed authentication set a token")
			}
		})
	}
}

func TestBackendRequiresAuthentication(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatal("unauthenticated client made a request")
		return nil, nil
	})})
	var response any
	if err := client.Backend(context.Background(), "query {}", nil, &response); err == nil {
		t.Fatal("expected auth error")
	}
}
