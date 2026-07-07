package whatsapp

import (
	"fmt"

	"github.com/vinhnguyentan99/my-whatsapp/internal/config"
)

func New(cfg config.Config) (Provider, error) {
	switch cfg.Provider {
	// Phase 2
	case config.ProviderBusiness:
		return NewWhatsMeowProvider(cfg.PostgresDSN()), nil
	// Phase 1: We need focus on this...
	case config.ProviderAPI:
		return NewWhatsAppAPIProvider(cfg.BusinessPhoneNumberID, cfg.BusinessAccessToken, cfg.BusinessAPIVersion), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}
