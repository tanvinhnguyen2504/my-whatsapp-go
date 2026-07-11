package whatsapp

import (
	"fmt"

	"github.com/vinhnguyentan99/my-whatsapp/internal/config"
)

func NewWhatsAppService(cfg config.Config) (Provider, error) {
	switch cfg.Provider {
	// business: unofficial personal account via WhatsMeow (PostgreSQL session store).
	case config.ProviderBusiness:
		return NewWhatsMeowProvider(cfg.Database.WhatsmeosDSN()), nil
	// api: official Meta WhatsApp Business Cloud API.
	case config.ProviderAPI:
		return NewWhatsappAPIService(cfg.BusinessPhoneNumberID, cfg.BusinessAccessToken, cfg.BusinessAPIVersion), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}
