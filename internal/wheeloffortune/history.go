package wheeloffortune

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

const historyPageSize = 100

type HistoryFilter struct {
	From string
	To   string
	Fuel string
}

type historyConnection struct {
	Edges    []*historyEdge   `json:"edges"`
	PageInfo *historyPageInfo `json:"pageInfo"`
}

type historyEdge struct {
	Cursor *string      `json:"cursor"`
	Node   *historySpin `json:"node"`
}

type historyPageInfo struct {
	HasNextPage *bool   `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type historySpin struct {
	Reference     *string       `json:"reference"`
	SpunAt        *time.Time    `json:"spunAt"`
	PrizeType     *string       `json:"prizeType"`
	IncentiveType *string       `json:"incentiveType"`
	Prize         *historyPrize `json:"prize"`
}

type historyPrize struct {
	Value   *int    `json:"value"`
	Display *string `json:"display"`
}

func RunHistory(ctx context.Context, client *octopus.Client, accountNumber string, filter HistoryFilter, output io.Writer) error {
	records, err := fetchHistory(ctx, client, accountNumber, filter)
	if err != nil {
		return fmt.Errorf("fetch wheel history: %w", err)
	}
	sort.SliceStable(records, func(i, j int) bool {
		left, right := records[i].SpunAt, records[j].SpunAt
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		return left.After(*right)
	})

	if len(records) == 0 {
		_, err := fmt.Fprintln(output, "No Wheel of Fortune history found.")
		return err
	}

	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "TIMESTAMP\tPRIZE\tPRIZE TYPE"); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", historyTimestamp(record.SpunAt), historyPrizeText(record.Prize), historyField(record.PrizeType)); err != nil {
			return err
		}
	}
	return table.Flush()
}

func fetchHistory(ctx context.Context, client *octopus.Client, accountNumber string, filter HistoryFilter) ([]historySpin, error) {
	query := `query WheelOfFortuneSpinHistory($accountNumber: String!, $fuelType: FuelType, $dateFrom: Date, $dateTo: Date, $after: String, $first: Int) {
        wheelOfFortuneSpinHistory(accountNumber: $accountNumber, fuelType: $fuelType, dateFrom: $dateFrom, dateTo: $dateTo, after: $after, first: $first) {
            edges { cursor node { reference spunAt prizeType incentiveType prize { value display } } }
            pageInfo { hasNextPage endCursor }
        }
    }`
	variables := map[string]any{
		"accountNumber": accountNumber,
		"first":         historyPageSize,
	}
	if filter.Fuel != "" {
		variables["fuelType"] = filter.Fuel
	}
	if filter.From != "" {
		variables["dateFrom"] = filter.From
	}
	if filter.To != "" {
		variables["dateTo"] = filter.To
	}

	var records []historySpin
	seenCursors := make(map[string]struct{})
	for {
		var response struct {
			History *historyConnection `json:"wheelOfFortuneSpinHistory"`
		}
		if err := client.Backend(ctx, query, variables, &response); err != nil {
			return nil, err
		}
		if response.History == nil || response.History.PageInfo == nil || response.History.Edges == nil || response.History.PageInfo.HasNextPage == nil {
			return nil, errors.New("Octopus returned an incomplete history page")
		}
		for _, edge := range response.History.Edges {
			if edge == nil || edge.Cursor == nil || edge.Node == nil {
				return nil, errors.New("Octopus returned an incomplete history edge")
			}
			records = append(records, *edge.Node)
		}
		if !*response.History.PageInfo.HasNextPage {
			break
		}
		if response.History.PageInfo.EndCursor == nil || *response.History.PageInfo.EndCursor == "" {
			return nil, errors.New("Octopus indicated another history page without an end cursor")
		}
		cursor := *response.History.PageInfo.EndCursor
		if _, exists := seenCursors[cursor]; exists {
			return nil, errors.New("Octopus repeated a history page cursor")
		}
		seenCursors[cursor] = struct{}{}
		if len(response.History.Edges) == 0 {
			return nil, errors.New("Octopus indicated another history page without returning records")
		}
		variables["after"] = cursor
	}
	return records, nil
}

func historyTimestamp(timestamp *time.Time) string {
	if timestamp == nil {
		return "(unknown)"
	}
	return timestamp.Format("2006-01-02 15:04:05 MST")
}

func historyField(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "(unknown)"
	}
	return *value
}

func historyPrizeText(prize *historyPrize) string {
	if prize == nil {
		return "(no prize)"
	}
	if prize.Display != nil && strings.TrimSpace(*prize.Display) != "" {
		return *prize.Display
	}
	if prize.Value != nil {
		return fmt.Sprintf("%d", *prize.Value)
	}
	return "(no prize details)"
}
