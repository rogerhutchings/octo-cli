package savingsessions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rogerhutchings/octo-cli/internal/octopus"
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
	SavingSessions *savingSessionsResult `json:"savingSessions"`
}

type savingSessionsResult struct {
	Events  *[]savingSessionEvent  `json:"events"`
	Account *savingSessionsAccount `json:"account"`
}

type savingSessionsAccount struct {
	SignedUpMeterPoint *struct {
		RegionID int64 `json:"regionId"`
	} `json:"signedUpMeterPoint"`
	JoinedEvents *[]joinedSavingSession `json:"joinedEvents"`
}

type sessionAssessment struct {
	event     savingSessionEvent
	eligible  bool
	joined    bool
	joinKnown bool
	reasons   []string
}

// RunList shows every upcoming event and its join eligibility without joining.
func RunList(ctx context.Context, client *octopus.Client, accountNumber string, results io.Writer) error {
	data, err := fetchSavingSessions(ctx, client, accountNumber)
	if err != nil {
		return fmt.Errorf("fetch saving sessions: %w", err)
	}
	assessments := assessUpcomingSavingSessions(data, time.Now())
	sort.SliceStable(assessments, func(i, j int) bool {
		return assessments[i].event.StartAt.Before(assessments[j].event.StartAt)
	})
	if len(assessments) == 0 {
		if _, err := fmt.Fprintln(results, "No upcoming Saving Sessions found."); err != nil {
			return fmt.Errorf("write Saving Sessions list: %w", err)
		}
		return nil
	}
	table := tabwriter.NewWriter(results, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "CODE\tSTART\tEND\tEVENT TYPE\tJOINED\tELIGIBILITY"); err != nil {
		return fmt.Errorf("write Saving Sessions list: %w", err)
	}
	for _, assessment := range assessments {
		joined := "unknown"
		if assessment.joinKnown {
			joined = "no"
		}
		if assessment.joined {
			joined = "yes"
		}
		eligibility := "eligible"
		if !assessment.eligible {
			eligibility = "ineligible: " + strings.Join(assessment.reasons, ", ")
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n",
			assessment.event.Code,
			assessment.event.StartAt.Format("2006-01-02 15:04 MST"),
			assessment.event.EndAt.Format("2006-01-02 15:04 MST"),
			assessment.event.EventType,
			joined,
			eligibility,
		); err != nil {
			return fmt.Errorf("write Saving Sessions list: %w", err)
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write Saving Sessions list: %w", err)
	}
	return nil
}

func Run(ctx context.Context, client *octopus.Client, accountNumber string, execute bool, results io.Writer) error {
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

	if len(candidateSessions) == 0 {
		if _, err := fmt.Fprintln(results, "No eligible Saving Sessions found."); err != nil {
			return fmt.Errorf("write Saving Sessions result: %w", err)
		}
	} else {
		for _, event := range candidateSessions {
			if _, err := fmt.Fprintf(results,
				"Eligible Saving Session: id=%d code=%s event_type=%s start_at=%s end_at=%s capacity_status=%s\n",
				event.ID,
				event.Code,
				event.EventType,
				event.StartAt.Format(time.RFC3339),
				event.EndAt.Format(time.RFC3339),
				event.CapacityStatus,
			); err != nil {
				return fmt.Errorf("write Saving Sessions candidate: %w", err)
			}
		}
	}

	if !execute && len(candidateSessions) > 0 {
		if _, err := fmt.Fprintln(results, "Dry run: no sessions were joined. Run with --execute to join eligible sessions."); err != nil {
			return fmt.Errorf("write Saving Sessions dry-run result: %w", err)
		}
		return nil
	}
	if !execute {
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

		if _, err := fmt.Fprintf(results, "Joined Saving Session: id=%d code=%s start_at=%s\n",
			event.ID, event.Code, event.StartAt.Format(time.RFC3339)); err != nil {
			return fmt.Errorf("write joined Saving Sessions result: %w", err)
		}
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
	if responseData.SavingSessions == nil || responseData.SavingSessions.Events == nil {
		return savingSessionsData{}, errors.New("Octopus returned incomplete Saving Sessions data")
	}

	return responseData, nil
}

func findCandidateSavingSessions(
	savingSessions savingSessionsData,
	now time.Time,
) ([]savingSessionEvent, error) {
	if savingSessions.SavingSessions == nil || savingSessions.SavingSessions.Events == nil {
		return nil, errors.New("Octopus returned incomplete Saving Sessions data")
	}
	account := savingSessions.SavingSessions.Account
	if account == nil {
		return nil, errors.New("account data is unavailable")
	}
	if account.SignedUpMeterPoint == nil {
		return nil, errors.New("account has no signed-up meter point")
	}
	if account.JoinedEvents == nil {
		return nil, errors.New("account joined-session status is unavailable")
	}
	assessments := assessUpcomingSavingSessions(savingSessions, now)
	candidates := make([]savingSessionEvent, 0)
	for _, assessment := range assessments {
		if assessment.eligible {
			candidates = append(candidates, assessment.event)
		}
	}
	return candidates, nil
}

func assessUpcomingSavingSessions(data savingSessionsData, now time.Time) []sessionAssessment {
	if data.SavingSessions == nil || data.SavingSessions.Events == nil {
		return nil
	}
	account := data.SavingSessions.Account
	var joinedEventIDs map[int64]struct{}
	if account != nil && account.JoinedEvents != nil {
		joinedEventIDs = make(map[int64]struct{}, len(*account.JoinedEvents))
		for _, joinedEvent := range *account.JoinedEvents {
			joinedEventIDs[joinedEvent.EventID] = struct{}{}
		}
	}
	assessments := make([]sessionAssessment, 0)
	for _, event := range *data.SavingSessions.Events {
		if !event.StartAt.After(now) {
			continue
		}
		assessment := sessionAssessment{event: event, eligible: true}
		if event.EventType != savingSessionEventType {
			assessment.reasons = append(assessment.reasons, "event type is not TURN_DOWN")
		}
		if account == nil || account.SignedUpMeterPoint == nil {
			assessment.reasons = append(assessment.reasons, "account region is unavailable")
		} else if !eventAppliesToRegion(event, account.SignedUpMeterPoint.RegionID) {
			assessment.reasons = append(assessment.reasons, "session is outside the account region")
		}
		if account == nil || account.JoinedEvents == nil {
			assessment.reasons = append(assessment.reasons, "joined status is unknown")
		} else {
			assessment.joinKnown = true
			_, assessment.joined = joinedEventIDs[event.ID]
		}
		if assessment.joined {
			assessment.reasons = append(assessment.reasons, "already joined")
		}
		assessment.eligible = len(assessment.reasons) == 0
		assessments = append(assessments, assessment)
	}
	return assessments
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
