package scratchcard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/rogerhutchings/octo-cli/internal/octopus"
)

const activeScratchcardStatusQuery = `query ActiveScratchcardStatus($accountNumber: String!) {
  octoplusActiveScratchcardData(accountNumber: $accountNumber) {
    activeSession {
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
	StartsAt *string `json:"startsAt"`
	EndsAt   *string `json:"endsAt"`
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
	var result response
	if err := client.Backend(ctx, activeScratchcardStatusQuery, map[string]any{"accountNumber": accountNumber}, &result); err != nil {
		return fmt.Errorf("fetch Scratchcard status: %w", err)
	}
	if len(result.Data) == 0 || isNull(result.Data) {
		return errors.New("fetch Scratchcard status: Octopus returned no active Scratchcard data")
	}

	var data activeData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return fmt.Errorf("decode active Scratchcard data: %w", err)
	}
	if len(data.ActiveSession) == 0 || len(data.Scratchcard) == 0 {
		return errors.New("decode active Scratchcard data: Octopus omitted session or Scratchcard fields")
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
	if _, err := fmt.Fprintf(results, "Scratchcard status: %s.\n", readableStatus(*activeCard.Status)); err != nil {
		return fmt.Errorf("write Scratchcard status: %w", err)
	}
	if _, err := fmt.Fprintf(results, "Session: %s to %s.\n", *activeSession.StartsAt, *activeSession.EndsAt); err != nil {
		return fmt.Errorf("write Scratchcard session: %w", err)
	}
	if len(activeCard.Offer) > 0 && !isNull(activeCard.Offer) {
		var cardOffer offer
		if err := json.Unmarshal(activeCard.Offer, &cardOffer); err != nil {
			return fmt.Errorf("decode Scratchcard offer: %w", err)
		}
		if err := printOffer(results, "Offer", cardOffer); err != nil {
			return err
		}
	}
	if len(activeCard.Prize) == 0 || isNull(activeCard.Prize) {
		return nil
	}
	var cardPrize prize
	if err := json.Unmarshal(activeCard.Prize, &cardPrize); err != nil {
		return fmt.Errorf("decode Scratchcard prize: %w", err)
	}
	if cardPrize.TypeName == nil || *cardPrize.TypeName == "" {
		return errors.New("decode Scratchcard prize: Octopus omitted the prize type")
	}
	switch *cardPrize.TypeName {
	case "OctoplusOfferType":
		return printOffer(results, "Prize details returned", offer{Name: cardPrize.Name, Description: cardPrize.Description})
	case "StampsAwardedType":
		_, err := fmt.Fprintln(results, "Prize details: stamps-awarded type.")
		return writeError("write Scratchcard prize", err)
	default:
		_, err := fmt.Fprintf(results, "Prize type returned: %s (details not interpreted).\n", *cardPrize.TypeName)
		return writeError("write Scratchcard prize", err)
	}
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

func printOffer(results io.Writer, label string, value offer) error {
	if value.Name == nil && value.Description == nil {
		return nil
	}
	if value.Name != nil && value.Description != nil {
		_, err := fmt.Fprintf(results, "%s: %s — %s.\n", label, *value.Name, *value.Description)
		return writeError("write Scratchcard offer", err)
	}
	if value.Name != nil {
		_, err := fmt.Fprintf(results, "%s: %s.\n", label, *value.Name)
		return writeError("write Scratchcard offer", err)
	}
	_, err := fmt.Fprintf(results, "%s: %s.\n", label, *value.Description)
	return writeError("write Scratchcard offer", err)
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
