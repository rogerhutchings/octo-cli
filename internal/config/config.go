package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	APIKey        string
	AccountNumber string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	apiKey := strings.TrimSpace(os.Getenv("OCTOPUS_API_KEY"))
	if apiKey == "" {
		return Config{}, errors.New("OCTOPUS_API_KEY is required")
	}

	accountNumber := strings.TrimSpace(os.Getenv("OCTOPUS_ACCOUNT_NUMBER"))
	if accountNumber == "" {
		return Config{}, errors.New("OCTOPUS_ACCOUNT_NUMBER is required")
	}

	return Config{
		APIKey:        apiKey,
		AccountNumber: accountNumber,
	}, nil
}
