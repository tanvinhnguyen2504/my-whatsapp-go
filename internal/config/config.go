package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Provider selects which WhatsApp workflow the server drives.
const (
	// ProviderAPI is the official Meta WhatsApp Business Cloud API workflow
	// (access token + phone number id). Value: WHATSAPP_PROVIDER=api
	ProviderAPI = "api"
	// ProviderBusiness is the unofficial personal-account workflow backed by
	// WhatsMeow (QR login, PostgreSQL session store). Value: WHATSAPP_PROVIDER=business
	ProviderBusiness = "business"
)

// Database holds the PostgreSQL session-store settings used by the WhatsMeow
// (business) workflow.
type Database struct {
	Host       string
	Port       string
	User       string
	Pass       string
	Name       string
	SSLEnabled bool
}

func (d Database) DSN() string {
	sslMode := "disable"
	if d.SSLEnabled {
		sslMode = "require"
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		d.User, d.Pass, d.Host, d.Port, d.Name, sslMode)
	return dsn
}

func (d Database) WhatsmeosDSN() string {
	schema := "whatsmeow"

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&search_path=%s",
		d.User, d.Pass, d.Host, d.Port, d.Name, d.SSLEnabled, schema)
	return dsn

}

type Config struct {
	Mode     string // development | production
	HTTPPort string
	Provider string // api | business

	Database Database

	// WhatsApp Business Cloud API workflow.
	BusinessPhoneNumberID string
	BusinessAccessToken   string
	BusinessAPIVersion    string

	// Webhook (used by the api workflow's verification handshake).
	WebhookVerifyToken string

	// Google Cloud Storage (media uploads).
	GoogleCloudProjectID  string
	GoogleCloudBucketName string
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		Mode:     env("MODE", "development"),
		HTTPPort: env("HTTP_PORT", "8082"),
		Provider: env("WHATSAPP_PROVIDER", ProviderAPI),
		Database: Database{
			Host:       env("DB_HOST", "localhost"),
			Port:       env("DB_PORT", "5432"),
			User:       env("DB_USER", "postgres"),
			Pass:       os.Getenv("DB_PASS"),
			Name:       env("DB_NAME", "my_whatsapp"),
			SSLEnabled: os.Getenv("DB_SSL") == "true",
		},
		BusinessPhoneNumberID: os.Getenv("WHATSAPP_BUSINESS_PHONE_NUMBER_ID"),
		BusinessAccessToken:   os.Getenv("WHATSAPP_BUSINESS_ACCESS_TOKEN"),
		BusinessAPIVersion:    env("WHATSAPP_BUSINESS_API_VERSION", "v21.0"),
		WebhookVerifyToken:    os.Getenv("WHATSAPP_API_WEBHOOK_VERIFY_TOKEN"),
		GoogleCloudProjectID:  env("STORAGE_PROJECT_ID", ""),
		GoogleCloudBucketName: env("STORAGE_BUCKET_NAME", ""),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch c.Provider {
	case ProviderBusiness:
		if c.Database.Host == "" || c.Database.User == "" || c.Database.Name == "" {
			return fmt.Errorf("business provider requires DB_HOST, DB_USER and DB_NAME (PostgreSQL session store)")
		}
	case ProviderAPI:
		if c.BusinessPhoneNumberID == "" || c.BusinessAccessToken == "" {
			return fmt.Errorf("api provider requires WHATSAPP_BUSINESS_PHONE_NUMBER_ID and WHATSAPP_BUSINESS_ACCESS_TOKEN")
		}
	default:
		return fmt.Errorf("unknown WHATSAPP_PROVIDER %q (want %q or %q)", c.Provider, ProviderAPI, ProviderBusiness)
	}
	return nil
}

// func (c Config) WhatsmeowConnectionString() string {
// 	schema := "whatsmeow"

// 	return fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%d sslmode=%s search_path=%s",
// 		c.Database.User,
// 		c.Database.Pass,
// 		c.Database.Name,
// 		c.Database.Host,
// 		c.Database.Port,
// 		c.Database.SSLEnabled,
// 		schema,
// 	)
// }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
