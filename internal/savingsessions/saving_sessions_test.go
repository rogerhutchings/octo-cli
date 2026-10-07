package savingsessions

import (
	"testing"
	"time"
)

func TestFindCandidateSavingSessions(t *testing.T) {
	now := time.Date(
		2026,
		time.September,
		30,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	const accountRegionID int64 = 10

	tests := []struct {
		name              string
		event             savingSessionEvent
		joinedEventIDs    []int64
		expectedCandidate bool
	}{
		{
			name: "future session in matching region",
			event: savingSessionEvent{
				ID:             1,
				Code:           "MATCHING_REGION",
				StartAt:        now.Add(time.Hour),
				EndAt:          now.Add(2 * time.Hour),
				EventType:      savingSessionEventType,
				CapacityStatus: "FULL",
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			expectedCandidate: true,
		},
		{
			name: "future unrestricted session",
			event: savingSessionEvent{
				ID:           2,
				Code:         "UNRESTRICTED",
				StartAt:      now.Add(time.Hour),
				EndAt:        now.Add(2 * time.Hour),
				EventType:    savingSessionEventType,
				TargetRegion: nil,
			},
			expectedCandidate: true,
		},
		{
			name: "past session",
			event: savingSessionEvent{
				ID:        3,
				Code:      "PAST",
				StartAt:   now.Add(-2 * time.Hour),
				EndAt:     now.Add(-time.Hour),
				EventType: savingSessionEventType,
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			expectedCandidate: false,
		},
		{
			name: "already joined session",
			event: savingSessionEvent{
				ID:        4,
				Code:      "ALREADY_JOINED",
				StartAt:   now.Add(time.Hour),
				EndAt:     now.Add(2 * time.Hour),
				EventType: savingSessionEventType,
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			joinedEventIDs:    []int64{4},
			expectedCandidate: false,
		},
		{
			name: "session in different region",
			event: savingSessionEvent{
				ID:        5,
				Code:      "WRONG_REGION",
				StartAt:   now.Add(time.Hour),
				EndAt:     now.Add(2 * time.Hour),
				EventType: savingSessionEventType,
				TargetRegion: []targetRegion{
					{RegionID: 99},
				},
			},
			expectedCandidate: false,
		},
		{
			name: "non turn down event",
			event: savingSessionEvent{
				ID:        6,
				Code:      "HAPPY_HOUR",
				StartAt:   now.Add(time.Hour),
				EndAt:     now.Add(2 * time.Hour),
				EventType: "WEEKEND_HAPPY_HOUR",
				TargetRegion: []targetRegion{
					{RegionID: accountRegionID},
				},
			},
			expectedCandidate: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			joinedEvents := make([]joinedSavingSession, 0, len(test.joinedEventIDs))
			account := &savingSessionsAccount{SignedUpMeterPoint: &struct {
				RegionID int64 `json:"regionId"`
			}{
				RegionID: accountRegionID,
			}, JoinedEvents: &joinedEvents}

			for _, joinedEventID := range test.joinedEventIDs {
				joinedEvents = append(
					joinedEvents,
					joinedSavingSession{
						EventID: joinedEventID,
					},
				)
			}
			account.JoinedEvents = &joinedEvents
			events := []savingSessionEvent{test.event}
			savingSessions := savingSessionsData{SavingSessions: &savingSessionsResult{
				Events:  &events,
				Account: account,
			}}

			candidates, err := findCandidateSavingSessions(
				savingSessions,
				now,
			)
			if err != nil {
				t.Fatalf(
					"findCandidateSavingSessions() returned unexpected error: %v",
					err,
				)
			}

			if test.expectedCandidate && len(candidates) != 1 {
				t.Fatalf(
					"findCandidateSavingSessions() returned %d candidates, want 1",
					len(candidates),
				)
			}

			if !test.expectedCandidate && len(candidates) != 0 {
				t.Fatalf(
					"findCandidateSavingSessions() returned %d candidates, want 0",
					len(candidates),
				)
			}
		})
	}
}

func TestFindCandidateSavingSessionsWithoutSignedUpMeterPoint(t *testing.T) {
	joinedEvents := []joinedSavingSession{}
	savingSessions := savingSessionsData{SavingSessions: &savingSessionsResult{
		Events:  ptr([]savingSessionEvent{}),
		Account: &savingSessionsAccount{JoinedEvents: &joinedEvents},
	}}

	candidates, err := findCandidateSavingSessions(
		savingSessions,
		time.Now(),
	)

	if err == nil {
		t.Fatal("findCandidateSavingSessions() returned no error, want missing meter point error")
	}
	if len(candidates) != 0 {
		t.Fatalf("findCandidateSavingSessions() returned %d candidates without account data", len(candidates))
	}
}

func TestFindCandidateSavingSessionsWithUnknownJoinedStatus(t *testing.T) {
	events := []savingSessionEvent{{
		ID: 1, Code: "UPCOMING", StartAt: time.Now().Add(time.Hour), EventType: savingSessionEventType,
	}}
	savingSessions := savingSessionsData{SavingSessions: &savingSessionsResult{
		Events: &events,
		Account: &savingSessionsAccount{SignedUpMeterPoint: &struct {
			RegionID int64 `json:"regionId"`
		}{RegionID: 10}},
	}}

	candidates, err := findCandidateSavingSessions(savingSessions, time.Now())
	if err == nil || len(candidates) != 0 {
		t.Fatalf("findCandidateSavingSessions() = %d candidates, %v; want error and no candidates", len(candidates), err)
	}
}

func ptr[T any](value T) *T { return &value }
