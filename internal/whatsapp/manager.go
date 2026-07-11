package whatsapp

import (
	"fmt"

	"github.com/vinhnguyentan99/my-whatsapp/internal/config"
)

// NewWhatsAppService builds the active provider. onInbound receives messages
// arriving through the WhatsMeow workflow (nil disables that sink); the Business
// Cloud API workflow delivers inbound via its webhook instead.
func NewWhatsAppService(cfg config.Config, onInbound InboundFunc) (Provider, error) {
	switch cfg.Provider {
	// business: unofficial personal account via WhatsMeow (PostgreSQL session store).
	case config.ProviderBusiness:
		return NewWhatsMeowProvider(cfg.Database.WhatsmeosDSN(), onInbound), nil
	// api: official Meta WhatsApp Business Cloud API.
	case config.ProviderAPI:
		return NewWhatsappAPIService(cfg.BusinessPhoneNumberID, cfg.BusinessAccessToken, cfg.BusinessAPIVersion), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}
