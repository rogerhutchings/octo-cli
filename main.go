package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	octopusGraphQLEndpoint = "https://api.octopus.energy/v1/graphql/"
	octopusBackendEndpoint = "https://api.backend.octopus.energy/v1/graphql/"
)

const savingSessionEventType = "TURN_DOWN"

type config struct {
	apiKey        string
	accountNumber string
}

type targetRegion struct {
	RegionID int64 `json:"regionId"`
}

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

type savingSessionEvent struct {
	ID             int64          `json:"id"`
	Code           string         `json:"code"`
	StartAt        time.Time      `json:"startAt"`
	EndAt          time.Time      `json:"endAt"`
	EventType      string         `json:"eventType"`
	CapacityStatus string         `json:"capacityStatus"`
	TargetRegion   []targetRegion `json:"targetRegion"`
}

type joinedSavingSession struct {
	EventID   int64     `json:"eventId"`
	StartAt   time.Time `json:"startAt"`
	EndAt     time.Time `json:"endAt"`
	EventType string    `json:"eventType"`
}

type savingSessionsData struct {
	SavingSessions struct {
		Events  []savingSessionEvent `json:"events"`
		Account struct {
			HasJoinedCampaign  bool `json:"hasJoinedCampaign"`
			SignedUpMeterPoint *struct {
				RegionID int64 `json:"regionId"`
			} `json:"signedUpMeterPoint"`
			JoinedEvents []joinedSavingSession `json:"joinedEvents"`
		} `json:"account"`
	} `json:"savingSessions"`
}

func main() {
	execute := flag.Bool(
		"execute",
		false,
		"Join candidate Saving Sessions",
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := run(logger, *execute); err != nil {
		logger.Error("octopus autojoin failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, execute bool) error {
	appConfig, err := loadConfig()
	if err != nil {
		return err
	}

	httpClient := &http.Client{
		Timeout: 15 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	krakenToken, err := obtainKrakenToken(
		ctx,
		httpClient,
		appConfig.apiKey,
	)
	if err != nil {
		return fmt.Errorf("authenticate with Octopus: %w", err)
	}

	logger.Info("authenticated with Octopus")

	savingSessions, err := fetchSavingSessions(
		ctx,
		httpClient,
		krakenToken,
		appConfig.accountNumber,
	)
	if err != nil {
		return fmt.Errorf("fetch saving sessions: %w", err)
	}

	candidateSessions, err := findCandidateSavingSessions(
		savingSessions,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("find candidate saving sessions: %w", err)
	}

	logCandidateSavingSessions(logger, candidateSessions)

	if !execute {
		if len(candidateSessions) > 0 {
			logger.Info(
				"dry run; candidate sessions will not be joined",
			)
		}

		return nil
	}

	for _, event := range candidateSessions {
		if err := joinSavingSession(
			ctx,
			httpClient,
			krakenToken,
			appConfig.accountNumber,
			event,
		); err != nil {
			return fmt.Errorf(
				"join saving session %d: %w",
				event.ID,
				err,
			)
		}

		logger.Info(
			"saving session joined",
			"event_id",
			event.ID,
			"event_code",
			event.Code,
			"start_at",
			event.StartAt,
		)
	}

	return nil
}

func loadConfig() (config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return config{}, fmt.Errorf("load .env: %w", err)
	}

	apiKey := strings.TrimSpace(os.Getenv("OCTOPUS_API_KEY"))
	if apiKey == "" {
		return config{}, errors.New("OCTOPUS_API_KEY is required")
	}

	accountNumber := strings.TrimSpace(os.Getenv("OCTOPUS_ACCOUNT_NUMBER"))
	if accountNumber == "" {
		return config{}, errors.New("OCTOPUS_ACCOUNT_NUMBER is required")
	}

	return config{
		apiKey:        apiKey,
		accountNumber: accountNumber,
	}, nil
}

func obtainKrakenToken(
	ctx context.Context,
	httpClient *http.Client,
	apiKey string,
) (string, error) {
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

	err := postGraphQL(
		ctx,
		httpClient,
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
		return "", err
	}

	if responseData.ObtainKrakenToken.Token == "" {
		return "", errors.New("Octopus returned an empty Kraken token")
	}

	return responseData.ObtainKrakenToken.Token, nil
}

func fetchSavingSessions(
	ctx context.Context,
	httpClient *http.Client,
	krakenToken string,
	accountNumber string,
) (savingSessionsData, error) {
	query := fmt.Sprintf(`
		query {
			savingSessions {
				events(includeDev: false) {
					id
					code
					startAt
					endAt
					eventType
					capacityStatus
					targetRegion {
						regionId
					}
				}
				account(accountNumber: %q) {
					signedUpMeterPoint {
						regionId
					}
					hasJoinedCampaign
					joinedEvents {
						eventId
						startAt
						endAt
						eventType
					}
				}
			}
		}
	`, accountNumber)

	var responseData savingSessionsData

	err := postGraphQL(
		ctx,
		httpClient,
		octopusBackendEndpoint,
		krakenToken,
		query,
		nil,
		&responseData,
	)
	if err != nil {
		return savingSessionsData{}, err
	}

	return responseData, nil
}

func postGraphQL(
	ctx context.Context,
	httpClient *http.Client,
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

	response, err := httpClient.Do(request)
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
			"GraphQL request returned %s: %s",
			response.Status,
			strings.TrimSpace(string(responseBody)),
		)
	}

	var graphQLResponse graphQLResponse
	if err := json.Unmarshal(responseBody, &graphQLResponse); err != nil {
		return fmt.Errorf("decode GraphQL response: %w", err)
	}

	if len(graphQLResponse.Errors) > 0 {
		errorMessages := make([]string, 0, len(graphQLResponse.Errors))

		for _, graphQLError := range graphQLResponse.Errors {
			errorMessages = append(errorMessages, graphQLError.Message)
		}

		return fmt.Errorf(
			"GraphQL returned errors: %s",
			strings.Join(errorMessages, "; "),
		)
	}

	if err := json.Unmarshal(graphQLResponse.Data, responseData); err != nil {
		return fmt.Errorf("decode GraphQL data: %w", err)
	}

	return nil
}

func logCandidateSavingSessions(
	logger *slog.Logger,
	candidateSessions []savingSessionEvent,
) {
	logger.Info(
		"saving sessions checked",
		"candidate_count",
		len(candidateSessions),
	)

	for _, event := range candidateSessions {
		logger.Info(
			"saving session candidate",
			"event_id",
			event.ID,
			"event_code",
			event.Code,
			"start_at",
			event.StartAt,
			"end_at",
			event.EndAt,
		)
	}
}

func findCandidateSavingSessions(
	savingSessions savingSessionsData,
	now time.Time,
) ([]savingSessionEvent, error) {
	account := savingSessions.SavingSessions.Account

	if account.SignedUpMeterPoint == nil {
		return nil, errors.New("account has no signed-up meter point")
	}

	joinedEventIDs := make(map[int64]struct{}, len(account.JoinedEvents))

	for _, joinedEvent := range account.JoinedEvents {
		joinedEventIDs[joinedEvent.EventID] = struct{}{}
	}

	candidates := make([]savingSessionEvent, 0)

	for _, event := range savingSessions.SavingSessions.Events {
		if event.EventType != savingSessionEventType {
			continue
		}

		if !event.StartAt.After(now) {
			continue
		}

		if _, alreadyJoined := joinedEventIDs[event.ID]; alreadyJoined {
			continue
		}

		if !eventAppliesToRegion(
			event,
			account.SignedUpMeterPoint.RegionID,
		) {
			continue
		}

		candidates = append(candidates, event)
	}

	return candidates, nil
}

func eventAppliesToRegion(
	event savingSessionEvent,
	regionID int64,
) bool {
	if len(event.TargetRegion) == 0 {
		return true
	}

	for _, targetRegion := range event.TargetRegion {
		if targetRegion.RegionID == regionID {
			return true
		}
	}

	return false
}

func joinSavingSession(
	ctx context.Context,
	httpClient *http.Client,
	krakenToken string,
	accountNumber string,
	event savingSessionEvent,
) error {
	query := fmt.Sprintf(`
        mutation {
            joinSavingSessionsEvent(
                input: {
                    accountNumber: %q
                    eventCode: %q
                }
            ) {
                joinedEventCodes
            }
        }
    `, accountNumber, event.Code)

	var responseData struct {
		JoinSavingSessionsEvent struct {
			JoinedEventCodes []string `json:"joinedEventCodes"`
		} `json:"joinSavingSessionsEvent"`
	}

	err := postGraphQL(
		ctx,
		httpClient,
		octopusBackendEndpoint,
		krakenToken,
		query,
		nil,
		&responseData,
	)
	if err != nil {
		return err
	}

	for _, joinedEventCode := range responseData.JoinSavingSessionsEvent.JoinedEventCodes {
		if joinedEventCode == event.Code {
			return nil
		}
	}

	return fmt.Errorf(
		"Octopus did not confirm event %s as joined",
		event.Code,
	)
}
