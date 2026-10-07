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

func TestAuthenticateRejectsBlankAPIKeyBeforeRequest(t *testing.T) {
	for _, apiKey := range []string{"", " \t\n "} {
		client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			t.Fatal("blank API key caused an authentication request")
			return nil, nil
		})})
		err := client.Authenticate(context.Background(), apiKey)
		if err == nil || !strings.Contains(err.Error(), "validate API key: OCTOPUS_API_KEY is required") {
			t.Fatalf("Authenticate() error = %v, want API key validation error", err)
		}
	}
}

func TestAuthenticationErrorIsActionableAndRedactsFullAPIKey(t *testing.T) {
	const apiKey = "octo-APIKEY-secret-987654"
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"errors":[{"message":"Invalid data. Echo: octo-APIKEY-secret-987654"}],"extensions":{"debug":"request-body-marker token-body-marker"}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	err := client.Authenticate(context.Background(), apiKey)
	if err == nil {
		t.Fatal("Authenticate() succeeded, want upstream error")
	}
	message := err.Error()
	for _, expected := range []string{
		"request Kraken token",
		"GraphQL returned errors: Invalid data.",
		"check that OCTOPUS_API_KEY contains the API key rather than the account number",
	} {
		if !strings.Contains(message, expected) {
			t.Errorf("error %q does not contain %q", message, expected)
		}
	}
	for _, secret := range []string{apiKey, "request-body-marker", "token-body-marker"} {
		if strings.Contains(message, secret) {
			t.Errorf("authentication error leaked %q: %s", secret, message)
		}
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
