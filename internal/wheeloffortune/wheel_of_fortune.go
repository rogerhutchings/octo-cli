package wheeloffortune

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/rogerhutchings/octo-cli/internal/octopus"
)

type availableSpins struct {
	Electricity *spinAllowance `json:"electricity"`
	Gas         *spinAllowance `json:"gas"`
}

type spinAllowance struct {
	SpinsAllowed *int `json:"spinsAllowed"`
}

func (spins availableSpins) remaining(fuelType string) int {
	if fuelType == "ELECTRICITY" {
		return *spins.Electricity.SpinsAllowed
	}
	return *spins.Gas.SpinsAllowed
}

func fetchAvailableSpins(ctx context.Context, client *octopus.Client, accountNumber string) (availableSpins, error) {
	query := fmt.Sprintf(`query {
        electricity: wheelOfFortuneSpinsAllowed(fuelType: ELECTRICITY, accountNumber: %q) { spinsAllowed }
        gas: wheelOfFortuneSpinsAllowed(fuelType: GAS, accountNumber: %q) { spinsAllowed }
    }`, accountNumber, accountNumber)
	var spins availableSpins
	if err := client.Backend(ctx, query, nil, &spins); err != nil {
		return availableSpins{}, err
	}
	for _, allowance := range []*spinAllowance{spins.Electricity, spins.Gas} {
		if allowance == nil || allowance.SpinsAllowed == nil || *allowance.SpinsAllowed < 0 {
			return availableSpins{}, errors.New("Octopus returned missing or invalid spin allowances")
		}
	}
	return spins, nil
}

func spinWheel(ctx context.Context, client *octopus.Client, accountNumber, fuelType string) (*json.Number, error) {
	if fuelType != "ELECTRICITY" && fuelType != "GAS" {
		return nil, fmt.Errorf("unsupported fuel type %q", fuelType)
	}
	query := fmt.Sprintf(`mutation {
        spinWheelOfFortune(input: { accountNumber: %q, fuelType: %s }) {
            prize { value }
        }
    }`, accountNumber, fuelType)
	var response struct {
		Spin *struct {
			Prize *struct {
				Value *json.Number `json:"value"`
			} `json:"prize"`
		} `json:"spinWheelOfFortune"`
	}
	if err := client.Backend(ctx, query, nil, &response); err != nil {
		return nil, err
	}
	if response.Spin == nil {
		return nil, errors.New("Octopus returned no spin result")
	}
	// A successful spin need not have a prize. Preserve the API value without
	// guessing whether it represents points or money for this account.
	if response.Spin.Prize == nil {
		return nil, nil
	}
	return response.Spin.Prize.Value, nil
}

// Run checks both fuels and, only when execute is true, uses available spins.
// Mutations are never retried: after an ambiguous result, a later invocation
// must read the server's allowance afresh.
func Run(ctx context.Context, client *octopus.Client, accountNumber string, execute bool, maxSpins int, results io.Writer) error {
	spins, err := fetchAvailableSpins(ctx, client, accountNumber)
	if err != nil {
		return fmt.Errorf("fetch wheel spins: %w", err)
	}
	if _, err := fmt.Fprintf(results, "Available spins: electricity=%d, gas=%d\n",
		spins.remaining("ELECTRICITY"), spins.remaining("GAS")); err != nil {
		return fmt.Errorf("write wheel allowance result: %w", err)
	}
	if spins.remaining("ELECTRICITY") == 0 && spins.remaining("GAS") == 0 {
		if _, err := fmt.Fprintln(results, "No available spins; no action taken."); err != nil {
			return fmt.Errorf("write no-spins result: %w", err)
		}
		return nil
	}
	if !execute {
		message := "Dry run: available spins were not used."
		if maxSpins > 0 {
			planned := spins.remaining("ELECTRICITY") + spins.remaining("GAS")
			if planned > maxSpins {
				planned = maxSpins
			}
			message = fmt.Sprintf("Dry run: would use at most %d of the available spins.", planned)
		}
		if _, err := fmt.Fprintln(results, message); err != nil {
			return fmt.Errorf("write wheel dry-run result: %w", err)
		}
		return nil
	}
	// Bound this run by the initial allowance, even if the remote count grows.
	initialSpins := spins
	attemptedTotal := 0
	for _, fuelType := range []string{"ELECTRICITY", "GAS"} {
		for attempted := 0; attempted < initialSpins.remaining(fuelType) && spins.remaining(fuelType) > 0 && (maxSpins == 0 || attemptedTotal < maxSpins); attempted++ {
			before := spins.remaining(fuelType)
			prize, err := spinWheel(ctx, client, accountNumber, fuelType)
			if err != nil {
				return fmt.Errorf("spin %s wheel (not retried; check remaining spins before running again): %w", fuelType, err)
			}
			attemptedTotal++
			prizeValue := "not returned"
			if prize != nil {
				prizeValue = prize.String()
			}
			_, outputErr := fmt.Fprintf(results, "Wheel spun: fuel=%s prize_value=%s\n", fuelType, prizeValue)
			spins, err = fetchAvailableSpins(ctx, client, accountNumber)
			if err != nil {
				return fmt.Errorf("check allowance after %s spin; stopping: %w", fuelType, err)
			}
			if spins.remaining(fuelType) >= before {
				return fmt.Errorf("%s allowance did not decrease after a spin; stopping to avoid repeated requests", fuelType)
			}
			if outputErr != nil {
				return fmt.Errorf("write wheel spin result: %w", outputErr)
			}
		}
	}
	if _, err := fmt.Fprintf(results, "Wheel spins finished: electricity_remaining=%d gas_remaining=%d\n",
		spins.remaining("ELECTRICITY"), spins.remaining("GAS")); err != nil {
		return fmt.Errorf("write wheel completion result: %w", err)
	}
	return nil
}
