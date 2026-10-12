package scratchcard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/rogerhutchings/octo-cli/internal/octopus"
)

const activeScratchcardStatusQuery = `query ActiveScratchcardStatus($accountNumber: String!) {
  octoplusActiveScratchcardData(accountNumber: $accountNumber) {
    activeSession {
      externalReference
      startsAt
      endsAt
    }
    scratchcard {
      status
      offer {
        name
        description
      }
      prize {
        __typename
        ... on OctoplusOfferType {
          name
          description
        }
      }
    }
  }
}`

type session struct {
	ExternalReference *string `json:"externalReference"`
	StartsAt          *string `json:"startsAt"`
	EndsAt            *string `json:"endsAt"`
}

type offer struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type prize struct {
	TypeName    *string `json:"__typename"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type card struct {
	Status *string         `json:"status"`
	Offer  json.RawMessage `json:"offer"`
	Prize  json.RawMessage `json:"prize"`
}

type activeData struct {
	ActiveSession json.RawMessage `json:"activeSession"`
	Scratchcard   json.RawMessage `json:"scratchcard"`
}

type response struct {
	Data json.RawMessage `json:"octoplusActiveScratchcardData"`
}

// RunStatus fetches and displays the authenticated account's active Scratchcard status.
func RunStatus(ctx context.Context, client *octopus.Client, accountNumber string, results io.Writer) error {
	data, err := fetchActiveData(ctx, client, accountNumber)
	if err != nil {
		return fmt.Errorf("fetch Scratchcard status: %w", err)
	}
	if isNull(data.ActiveSession) {
		_, err := fmt.Fprintln(results, "No Scratchcard session is currently active.")
		return writeError("write Scratchcard status", err)
	}

	var activeSession session
	if err := json.Unmarshal(data.ActiveSession, &activeSession); err != nil {
		return fmt.Errorf("decode active Scratchcard session: %w", err)
	}
	if activeSession.StartsAt == nil || activeSession.EndsAt == nil {
		return errors.New("decode active Scratchcard session: Octopus omitted required session dates")
	}
	return displayStatus(data, activeSession, results)
}

func fetchActiveData(ctx context.Context, client *octopus.Client, accountNumber string) (activeData, error) {
	var result response
	if err := client.Backend(ctx, activeScratchcardStatusQuery, map[string]any{"accountNumber": accountNumber}, &result); err != nil {
		return activeData{}, err
	}
	if len(result.Data) == 0 || isNull(result.Data) {
		return activeData{}, errors.New("Octopus returned no active Scratchcard data")
	}

	var data activeData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return activeData{}, fmt.Errorf("decode active Scratchcard data: %w", err)
	}
	if len(data.ActiveSession) == 0 || len(data.Scratchcard) == 0 {
		return activeData{}, errors.New("decode active Scratchcard data: Octopus omitted session or Scratchcard fields")
	}
	return data, nil
}

func displayStatus(data activeData, activeSession session, results io.Writer) error {
	if isNull(data.Scratchcard) {
		if _, err := fmt.Fprintln(results, "An active Scratchcard session is present, but Octopus returned no Scratchcard. No available play is confirmed."); err != nil {
			return fmt.Errorf("write Scratchcard status: %w", err)
		}
		if _, err := fmt.Fprintf(results, "Session: %s to %s.\n", *activeSession.StartsAt, *activeSession.EndsAt); err != nil {
			return fmt.Errorf("write Scratchcard session: %w", err)
		}
		return nil
	}

	var activeCard card
	if err := json.Unmarshal(data.Scratchcard, &activeCard); err != nil {
		return fmt.Errorf("decode Scratchcard: %w", err)
	}
	if activeCard.Status == nil || *activeCard.Status == "" {
		return errors.New("decode Scratchcard: Octopus omitted required status")
	}
	details, err := decodeCardDetails(activeCard)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(results, "Scratchcard status: %s.\n", readableStatus(*activeCard.Status)); err != nil {
		return fmt.Errorf("write Scratchcard status: %w", err)
	}
	if _, err := fmt.Fprintf(results, "Session: %s to %s.\n", *activeSession.StartsAt, *activeSession.EndsAt); err != nil {
		return fmt.Errorf("write Scratchcard session: %w", err)
	}
	return displayCardDetails(results, details)
}

func readableStatus(status string) string {
	switch status {
	case "DID_NOT_WIN":
		return "Did not win"
	case "PRIZE_NOT_YET_CLAIMED":
		return "Prize not yet claimed"
	case "PRIZE_CLAIMED":
		return "Prize claimed"
	case "PRIZE_REJECTED":
		return "Prize rejected"
	case "PRIZE_NOT_CLAIMED_IN_TIME":
		return "Prize not claimed in time"
	default:
		return fmt.Sprintf("Unknown status %q (not interpreted)", status)
	}
}

func formatOffer(label string, value offer) (string, bool) {
	if value.Name == nil && value.Description == nil {
		return "", false
	}
	if value.Name != nil && value.Description != nil {
		return fmt.Sprintf("%s: %s — %s.", label, *value.Name, *value.Description), true
	}
	if value.Name != nil {
		return fmt.Sprintf("%s: %s.", label, *value.Name), true
	}
	return fmt.Sprintf("%s: %s.", label, *value.Description), true
}

func isNull(value json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func writeError(stage string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return nil
}

const scratchcardPlayMutation = `mutation ScratchOctoplusScratchcard($input: ScratchOctoplusScratchcardInput!) {
  scratchOctoplusScratchcard(input: $input) {
    scratchcard {
      externalReference
      status
      offer {
        name
        description
      }
      prize {
        __typename
        ... on OctoplusOfferType {
          name
          description
        }
      }
    }
  }
}`

type playResponse struct {
	Result json.RawMessage `json:"scratchOctoplusScratchcard"`
}

type playResult struct {
	Scratchcard json.RawMessage `json:"scratchcard"`
}

type playedCard struct {
	ExternalReference *string         `json:"externalReference"`
	Status            *string         `json:"status"`
	Offer             json.RawMessage `json:"offer"`
	Prize             json.RawMessage `json:"prize"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// RunPlay fetches the active session and previews or attempts one Scratchcard play.
func RunPlay(ctx context.Context, client *octopus.Client, accountNumber string, execute bool, results io.Writer) error {
	return runPlayAt(ctx, client, accountNumber, execute, results, time.Now())
}

func runPlayAt(ctx context.Context, client *octopus.Client, accountNumber string, execute bool, results io.Writer, now time.Time) error {
	data, err := fetchActiveData(ctx, client, accountNumber)
	if err != nil {
		return fmt.Errorf("fetch active Scratchcard session: %w", err)
	}
	if isNull(data.ActiveSession) {
		_, err := fmt.Fprintln(results, "No Scratchcard session is currently active. No play attempted.")
		return writeError("write Scratchcard play", err)
	}
	var activeSession session
	if err := json.Unmarshal(data.ActiveSession, &activeSession); err != nil {
		return fmt.Errorf("decode active Scratchcard session: %w", err)
	}
	start, end, err := validatePlayableSession(activeSession)
	if err != nil {
		return err
	}
	if !now.Before(end) {
		_, err := fmt.Fprintln(results, "Scratchcard session has expired. No play attempted.")
		return writeError("write Scratchcard play", err)
	}
	if now.Before(start) {
		_, err := fmt.Fprintln(results, "Scratchcard session has not started. No play attempted.")
		return writeError("write Scratchcard play", err)
	}
	if !isNull(data.Scratchcard) {
		var existing card
		if err := json.Unmarshal(data.Scratchcard, &existing); err != nil {
			return fmt.Errorf("decode existing Scratchcard: %w", err)
		}
		if existing.Status == nil || *existing.Status == "" {
			return errors.New("decode existing Scratchcard: Octopus omitted required status")
		}
		details, err := decodeCardDetails(existing)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(results, "Scratchcard already exists with status: %s. No play attempted.\n", readableStatus(*existing.Status)); err != nil {
			return fmt.Errorf("write Scratchcard play: %w", err)
		}
		if err := displayCardDetails(results, details); err != nil {
			return err
		}
		return writeClaimGuidance(*existing.Status, results)
	}

	if !execute {
		_, err := fmt.Fprintf(results, "Dry run: would attempt one Scratchcard play for session %s (%s to %s). A missing card does not confirm play availability; Octopus decides whether to accept the attempt.\n", *activeSession.ExternalReference, *activeSession.StartsAt, *activeSession.EndsAt)
		return writeError("write Scratchcard play", err)
	}

	var result playResponse
	if err := client.Backend(ctx, scratchcardPlayMutation, map[string]any{"input": map[string]any{
		"accountNumber":                       accountNumber,
		"scratchcardSessionExternalReference": *activeSession.ExternalReference,
	}}, &result); err != nil {
		return fmt.Errorf("play Scratchcard: request outcome may be ambiguous and the play may have completed; check `octo-cli scratchcard status` before another attempt: %w", err)
	}
	if len(result.Result) == 0 || isNull(result.Result) {
		return errors.New("validate Scratchcard play response: Octopus returned no mutation result; the play may have completed, so check `octo-cli scratchcard status` before another attempt")
	}
	var mutation playResult
	if err := json.Unmarshal(result.Result, &mutation); err != nil {
		return fmt.Errorf("decode Scratchcard play result: %w; the play may have completed, so check `octo-cli scratchcard status` before another attempt", err)
	}
	if len(mutation.Scratchcard) == 0 || isNull(mutation.Scratchcard) {
		return errors.New("validate Scratchcard play response: Octopus returned no card; the play may have completed, so check `octo-cli scratchcard status` before another attempt")
	}
	var played playedCard
	if err := json.Unmarshal(mutation.Scratchcard, &played); err != nil {
		return fmt.Errorf("decode played Scratchcard: %w; the play may have completed, so check `octo-cli scratchcard status` before another attempt", err)
	}
	if played.ExternalReference == nil || !uuidPattern.MatchString(*played.ExternalReference) || played.Status == nil || *played.Status == "" {
		return errors.New("validate Scratchcard play response: Octopus omitted required card reference or status; the play may have completed, so check `octo-cli scratchcard status` before another attempt")
	}
	details, err := decodeCardDetails(card{Status: played.Status, Offer: played.Offer, Prize: played.Prize})
	if err != nil {
		return fmt.Errorf("validate Scratchcard play response: %w; the play may have completed, so check `octo-cli scratchcard status` before another attempt", err)
	}
	if _, err := fmt.Fprintf(results, "Scratchcard play completed. Status: %s.\n", readableStatus(*played.Status)); err != nil {
		return fmt.Errorf("write Scratchcard play result: %w", err)
	}
	if err := displayCardDetails(results, details); err != nil {
		return err
	}
	return writeClaimGuidance(*played.Status, results)
}

func validatePlayableSession(value session) (time.Time, time.Time, error) {
	if value.ExternalReference == nil || !uuidPattern.MatchString(*value.ExternalReference) {
		return time.Time{}, time.Time{}, errors.New("validate active Scratchcard session: Octopus returned a missing or malformed session reference")
	}
	if value.StartsAt == nil || value.EndsAt == nil {
		return time.Time{}, time.Time{}, errors.New("validate active Scratchcard session: Octopus omitted required session dates")
	}
	start, err := time.Parse(time.RFC3339, *value.StartsAt)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("validate active Scratchcard session start: invalid timestamp: %w", err)
	}
	end, err := time.Parse(time.RFC3339, *value.EndsAt)
	if err != nil || !end.After(start) {
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("validate active Scratchcard session end: invalid timestamp: %w", err)
		}
		return time.Time{}, time.Time{}, errors.New("validate active Scratchcard session: end timestamp must be after start timestamp")
	}
	return start, end, nil
}

func decodeCardDetails(value card) ([]string, error) {
	var details []string
	if len(value.Offer) > 0 && !isNull(value.Offer) {
		var cardOffer offer
		if err := json.Unmarshal(value.Offer, &cardOffer); err != nil {
			return nil, fmt.Errorf("decode Scratchcard offer: %w", err)
		}
		if line, ok := formatOffer("Offer", cardOffer); ok {
			details = append(details, line)
		}
	}
	if len(value.Prize) == 0 || isNull(value.Prize) {
		return details, nil
	}
	var cardPrize prize
	if err := json.Unmarshal(value.Prize, &cardPrize); err != nil {
		return nil, fmt.Errorf("decode Scratchcard prize: %w", err)
	}
	if cardPrize.TypeName == nil || *cardPrize.TypeName == "" {
		return nil, errors.New("decode Scratchcard prize: Octopus omitted the prize type")
	}
	switch *cardPrize.TypeName {
	case "OctoplusOfferType":
		if line, ok := formatOffer("Prize details returned", offer{Name: cardPrize.Name, Description: cardPrize.Description}); ok {
			details = append(details, line)
		}
	case "StampsAwardedType":
		details = append(details, "Prize details: stamps-awarded type.")
	default:
		details = append(details, fmt.Sprintf("Prize type returned: %s (details not interpreted).", *cardPrize.TypeName))
	}
	return details, nil
}

func displayCardDetails(results io.Writer, details []string) error {
	for _, detail := range details {
		if _, err := fmt.Fprintln(results, detail); err != nil {
			return fmt.Errorf("write Scratchcard details: %w", err)
		}
	}
	return nil
}

func writeClaimGuidance(status string, results io.Writer) error {
	if status != "PRIZE_NOT_YET_CLAIMED" {
		return nil
	}
	_, err := fmt.Fprintln(results, "Inspect and claim this prize in the Octopus app.")
	return writeError("write Scratchcard claim guidance", err)
}
