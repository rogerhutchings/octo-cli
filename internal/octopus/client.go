package octopus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	octopusGraphQLEndpoint = "https://api.octopus.energy/v1/graphql/"
	octopusBackendEndpoint = "https://api.backend.octopus.energy/v1/graphql/"
)

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

// Client shares authentication and GraphQL transport across commands.
// A Client is intended for one sequential command invocation.
type Client struct {
	httpClient *http.Client
	token      string
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

func (client *Client) Backend(ctx context.Context, query string, variables map[string]any, responseData any) error {
	if client.token == "" {
		return errors.New("Octopus client is not authenticated")
	}
	return client.postGraphQL(ctx, octopusBackendEndpoint, client.token, query, variables, responseData)
}

func (client *Client) Authenticate(ctx context.Context, apiKey string) error {
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("validate API key: OCTOPUS_API_KEY is required")
	}
	const query = `
		mutation ObtainToken($input: ObtainJSONWebTokenInput!) {
			obtainKrakenToken(input: $input) {
				token
			}
		}
	`

	var responseData struct {
		ObtainKrakenToken struct {
			Token string `json:"token"`
		} `json:"obtainKrakenToken"`
	}

	err := client.postGraphQL(
		ctx,
		octopusGraphQLEndpoint,
		"",
		query,
		map[string]any{
			"input": map[string]any{
				"APIKey": apiKey,
			},
		},
		&responseData,
	)
	if err != nil {
		safeError := errors.New(strings.ReplaceAll(err.Error(), apiKey, "[redacted]"))
		if strings.Contains(strings.ToLower(safeError.Error()), "invalid data") {
			return fmt.Errorf("request Kraken token: %w; check that OCTOPUS_API_KEY contains the API key rather than the account number", safeError)
		}
		return fmt.Errorf("request Kraken token: %w", safeError)
	}

	if responseData.ObtainKrakenToken.Token == "" {
		return errors.New("validate Kraken token response: Octopus returned an empty Kraken token")
	}

	client.token = responseData.ObtainKrakenToken.Token
	return nil
}
func (client *Client) postGraphQL(
	ctx context.Context,
	endpoint string,
	authorization string,
	query string,
	variables map[string]any,
	responseData any,
) error {
	requestBody, err := json.Marshal(graphQLRequest{
		Query:     query,
		Variables: variables,
	})
	if err != nil {
		return fmt.Errorf("encode GraphQL request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return fmt.Errorf("create GraphQL request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send GraphQL request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read GraphQL response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf(
			"GraphQL request returned %s",
			response.Status,
		)
	}

	var graphQLResponse graphQLResponse
	if err := json.Unmarshal(responseBody, &graphQLResponse); err != nil {
		return fmt.Errorf("decode GraphQL response: %w", err)
	}

	if len(graphQLResponse.Errors) > 0 {
		errorMessages := make([]string, 0, len(graphQLResponse.Errors))

		for _, graphQLError := range graphQLResponse.Errors {
			message := graphQLError.Message
			if authorization != "" {
				message = strings.ReplaceAll(message, authorization, "[redacted]")
			}
			errorMessages = append(errorMessages, message)
		}

		return fmt.Errorf(
			"GraphQL returned errors: %s",
			strings.Join(errorMessages, "; "),
		)
	}

	if len(graphQLResponse.Data) == 0 || bytes.Equal(bytes.TrimSpace(graphQLResponse.Data), []byte("null")) {
		return errors.New("GraphQL returned no data")
	}

	if err := json.Unmarshal(graphQLResponse.Data, responseData); err != nil {
		return fmt.Errorf("decode GraphQL data: %w", err)
	}

	return nil
}
