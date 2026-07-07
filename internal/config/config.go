package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Provider selects which WhatsApp workflow the server drives.
const (
	// ProviderAPI is the unofficial personal-account workflow backed by WhatsMeow
	// (QR login, local session store). Value: WHATSAPP_PROVIDER=api
	ProviderAPI = "api"
	// ProviderBusiness is the official Meta WhatsApp Business Cloud API workflow
	// (access token + phone number id). Value: WHATSAPP_PROVIDER=business
	ProviderBusiness = "business"
)

// Config holds all runtime configuration, loaded from environment variables.
type Config struct {
	Mode     string // development | production
	HTTPPort string
	Provider string // api | business

	// WhatsMeow (personal account) workflow session store — PostgreSQL.
	DBHost   string
	DBPort   string
	DBUser   string
	DBPass   string
	DBName   string
	DBSchema string // PostgreSQL schema holding the whatsmeow_* tables (search_path)

	// WhatsApp Business Cloud API workflow.
	BusinessPhoneNumberID string
	BusinessAccessToken   string
	BusinessAPIVersion    string

	// Webhook (used by the Business workflow's verification handshake).
	WebhookVerifyToken string
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		Mode:                  env("MODE", "development"),
		HTTPPort:              env("HTTP_PORT", "8082"),
		Provider:              env("WHATSAPP_PROVIDER", ProviderAPI),
		DBHost:                os.Getenv("DB_HOST"),
		DBPort:                env("DB_PORT", "5432"),
		DBUser:                os.Getenv("DB_USER"),
		DBPass:                os.Getenv("DB_PASS"),
		DBName:                os.Getenv("DB_NAME"),
		DBSchema:              os.Getenv("DB_SCHEMA"),
		BusinessPhoneNumberID: os.Getenv("WHATSAPP_BUSINESS_PHONE_NUMBER_ID"),
		BusinessAccessToken:   os.Getenv("WHATSAPP_BUSINESS_ACCESS_TOKEN"),
		BusinessAPIVersion:    env("WHATSAPP_BUSINESS_API_VERSION", "v21.0"),
		WebhookVerifyToken:    os.Getenv("WHATSAPP_API_WEBHOOK_VERIFY_TOKEN"),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch c.Provider {
	case ProviderBusiness:
		if c.DBHost == "" || c.DBUser == "" || c.DBName == "" {
			return fmt.Errorf("api provider requires DB_HOST, DB_USER and DB_NAME (PostgreSQL session store)")
		}
	case ProviderAPI:
		if c.BusinessPhoneNumberID == "" || c.BusinessAccessToken == "" {
			return fmt.Errorf("business provider requires WHATSAPP_BUSINESS_PHONE_NUMBER_ID and WHATSAPP_BUSINESS_ACCESS_TOKEN")
		}
	default:
		return fmt.Errorf("unknown WHATSAPP_PROVIDER %q (want %q or %q)", c.Provider, ProviderAPI, ProviderBusiness)
	}
	return nil
}

func (c Config) PostgresDSN() string {
	auth := c.DBUser
	if c.DBPass != "" {
		auth += ":" + c.DBPass
	}
	dsn := fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", auth, c.DBHost, c.DBPort, c.DBName)
	if c.DBSchema != "" {
		dsn += "&search_path=" + c.DBSchema
	}
	return dsn
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
