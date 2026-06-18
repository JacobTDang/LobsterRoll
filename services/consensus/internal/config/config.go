// Package config loads consensus-svc settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the resolved consensus-svc configuration.
type Config struct {
	NATSURL    string
	MinWallets int
	Window     time.Duration
	DBPath     string
	QueueGroup string
}

const (
	defNATSURL    = "nats://nats:4222"
	defMinWallets = 3
	defWindow     = 6 * time.Hour
	defDBPath     = "consensus.db"
	defQueueGroup = "consensus"
)

// Load resolves config using getenv, applying defaults and validating.
func Load(getenv func(string) string) (Config, error) {
	minWallets := defMinWallets
	if v := getenv("CONSENSUS_MIN_WALLETS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("CONSENSUS_MIN_WALLETS: %w", err)
		}
		if n < 2 {
			return Config{}, fmt.Errorf("CONSENSUS_MIN_WALLETS must be >= 2, got %d", n)
		}
		minWallets = n
	}

	window := defWindow
	if v := getenv("CONSENSUS_WINDOW"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("CONSENSUS_WINDOW: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("CONSENSUS_WINDOW must be > 0, got %s", d)
		}
		window = d
	}

	return Config{
		NATSURL:    orDefault(getenv("NATS_URL"), defNATSURL),
		MinWallets: minWallets,
		Window:     window,
		DBPath:     orDefault(getenv("CONSENSUS_DB_PATH"), defDBPath),
		QueueGroup: orDefault(getenv("CONSENSUS_QUEUE_GROUP"), defQueueGroup),
	}, nil
}

// LoadFromEnv loads config from the process environment.
func LoadFromEnv() (Config, error) { return Load(os.Getenv) }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
