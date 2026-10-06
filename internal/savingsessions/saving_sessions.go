package savingsessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/rogerhutchings/octopus-autojoin/internal/octopus"
)

const savingSessionEventType = "TURN_DOWN"

type targetRegion struct {
	RegionID int64 `json:"regionId"`
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

func Run(ctx context.Context, logger *slog.Logger, client *octopus.Client, accountNumber string, execute bool) error {
	savingSessions, err := fetchSavingSessions(
		ctx,
		client,
		accountNumber,
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
			client,
			accountNumber,
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

func fetchSavingSessions(
	ctx context.Context,
	client *octopus.Client,
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

	err := client.Backend(
		ctx,
		query,
		nil,
		&responseData,
	)
	if err != nil {
		return savingSessionsData{}, err
	}

	return responseData, nil
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
			"event_type",
			event.EventType,
			"start_at",
			event.StartAt,
			"end_at",
			event.EndAt,
			"capacity_status",
			event.CapacityStatus,
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
	client *octopus.Client,
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

	err := client.Backend(
		ctx,
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
