package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr                 string
	DatabaseURL          string
	DataDir              string
	MasterKey            []byte
	AdminUsername        string
	AdminPassword        string
	PublicBaseURL        string
	QQAPIBaseURL         string
	QQTokenURL           string
	CookieSecure         bool
	WorkerPoll           time.Duration
	AIRequestTimeout     time.Duration
	DefaultContextLimit  int
	MessageRetentionDays int
}

func Load() (Config, error) {
	c := Config{
		Addr:                 env("APP_ADDR", ":8080"),
		DatabaseURL:          env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/aibot?sslmode=disable"),
		DataDir:              env("DATA_DIR", "./data"),
		AdminUsername:        env("ADMIN_USERNAME", "admin"),
		AdminPassword:        env("ADMIN_PASSWORD", "admin123456"),
		PublicBaseURL:        env("PUBLIC_BASE_URL", "http://localhost:8080"),
		QQAPIBaseURL:         env("QQ_API_BASE_URL", "https://api.sgroup.qq.com"),
		QQTokenURL:           env("QQ_TOKEN_URL", "https://bots.qq.com/app/getAppAccessToken"),
		CookieSecure:         envBool("COOKIE_SECURE", false),
		WorkerPoll:           envDuration("WORKER_POLL_INTERVAL", time.Second),
		AIRequestTimeout:     envDuration("AI_REQUEST_TIMEOUT", 90*time.Second),
		DefaultContextLimit:  envInt("DEFAULT_CONTEXT_LIMIT", 20),
		MessageRetentionDays: envInt("MESSAGE_RETENTION_DAYS", 90),
	}
	raw := os.Getenv("APP_MASTER_KEY")
	if raw == "" {
		// Development-only deterministic key; production must override it.
		raw = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return Config{}, fmt.Errorf("APP_MASTER_KEY must be base64-encoded 32 bytes")
	}
	c.MasterKey = key
	return c, nil
}

func envInt(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envDuration(name string, fallback time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
