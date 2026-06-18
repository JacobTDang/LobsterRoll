// Package config loads trader-svc settings from the environment. The private key
// and API creds are injected (k8s Secret); never logged.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/JacobTDang/LobsterRoll/pkg/chain"
	cfg "github.com/JacobTDang/LobsterRoll/pkg/config"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/clob"
)

// Config is the resolved trader-svc configuration.
type Config struct {
	PrivateKey      string
	Creds           clob.Creds
	MakerAddress    string
	ExchangeAddress string
	SignatureType   uint8
	PerTradeUSD     float64
	PerDayUSD       float64
	ExposureUSD     float64
	Policy          cfg.ExecutionPolicy
	NATSURL         string
	CLOBBase        string
	DBPath          string
	QueueGroup      string
}

const (
	defNATSURL    = "nats://nats:4222"
	defDBPath     = "trader.db"
	defQueueGroup = "trader"
)

// Load resolves config using getenv, applying defaults and validating the
// required secrets and risk caps.
func Load(getenv func(string) string) (Config, error) {
	policy, err := cfg.ParseExecutionMode(orDefault(getenv("EXECUTION_MODE"), "approval"))
	if err != nil {
		return Config{}, err
	}

	perTrade, err := floatEnv(getenv, "MAX_USD_PER_TRADE", 25)
	if err != nil {
		return Config{}, err
	}
	perDay, err := floatEnv(getenv, "MAX_USD_PER_DAY", 200)
	if err != nil {
		return Config{}, err
	}
	exposure, err := floatEnv(getenv, "MAX_OPEN_EXPOSURE_USD", 500)
	if err != nil {
		return Config{}, err
	}
	sigType := uint8(0)
	if v := getenv("TRADER_SIGNATURE_TYPE"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 0 || n > 3 {
			return Config{}, fmt.Errorf("TRADER_SIGNATURE_TYPE %q: want 0..3", v)
		}
		sigType = uint8(n)
	}

	c := Config{
		PrivateKey: getenv("TRADER_PRIVATE_KEY"),
		Creds: clob.Creds{
			APIKey:     getenv("POLYMARKET_API_KEY"),
			Secret:     getenv("POLYMARKET_API_SECRET"),
			Passphrase: getenv("POLYMARKET_API_PASSPHRASE"),
			Address:    getenv("TRADER_FUNDER_ADDRESS"),
		},
		MakerAddress:    getenv("TRADER_MAKER_ADDRESS"),
		ExchangeAddress: orDefault(getenv("TRADER_EXCHANGE_ADDRESS"), chain.CTFExchange),
		SignatureType:   sigType,
		PerTradeUSD:     perTrade,
		PerDayUSD:       perDay,
		ExposureUSD:     exposure,
		Policy:          policy,
		NATSURL:         orDefault(getenv("NATS_URL"), defNATSURL),
		CLOBBase:        orDefault(getenv("CLOB_BASE_URL"), clob.DefaultBaseURL),
		DBPath:          orDefault(getenv("TRADER_DB_PATH"), defDBPath),
		QueueGroup:      orDefault(getenv("TRADER_QUEUE_GROUP"), defQueueGroup),
	}

	if c.PrivateKey == "" {
		return Config{}, fmt.Errorf("TRADER_PRIVATE_KEY is required")
	}
	if c.Creds.APIKey == "" || c.Creds.Secret == "" || c.Creds.Passphrase == "" {
		return Config{}, fmt.Errorf("POLYMARKET_API_KEY/SECRET/PASSPHRASE are required")
	}
	if c.PerTradeUSD <= 0 || c.PerDayUSD <= 0 || c.ExposureUSD <= 0 {
		return Config{}, fmt.Errorf("risk caps must all be > 0")
	}
	return c, nil
}

// LoadFromEnv loads config from the process environment.
func LoadFromEnv() (Config, error) { return Load(os.Getenv) }

func floatEnv(getenv func(string) string, key string, def float64) (float64, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
