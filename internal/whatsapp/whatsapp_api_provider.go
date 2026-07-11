package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WhatsappAPIService struct {
	phoneNumberID string
	accessToken   string
	apiVersion    string
	http          *http.Client
}

func NewWhatsappAPIService(phoneNumberID, accessToken, apiVersion string) *WhatsappAPIService {
	return &WhatsappAPIService{
		phoneNumberID: phoneNumberID,
		accessToken:   accessToken,
		apiVersion:    apiVersion,
		http:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *WhatsappAPIService) Name() string { return "whatsapp-api" }

func (p *WhatsappAPIService) Connect(ctx context.Context) error { return nil }

func (p *WhatsappAPIService) Disconnect() {}

func (p *WhatsappAPIService) IsReady() bool {
	return p.phoneNumberID != "" && p.accessToken != ""
}

func (p *WhatsappAPIService) QRCode() string { return "" }

func (p *WhatsappAPIService) SendText(ctx context.Context, to, body string) (SendResult, error) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "text",
		"text":              map[string]any{"body": body},
	}
	raw, err := p.post(ctx, "messages", payload)
	if err != nil {
		return SendResult{}, err
	}

	var out struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return SendResult{}, fmt.Errorf("decode cloud api response: %w", err)
	}
	var id string
	if len(out.Messages) > 0 {
		id = out.Messages[0].ID
	}
	return SendResult{MessageID: id, Provider: p.Name()}, nil
}

// post calls POST /{apiVersion}/{phoneNumberID}/{path} on the Graph API.
func (p *WhatsappAPIService) post(ctx context.Context, path string, body any) ([]byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	url := fmt.Sprintf("https://graph.facebook.com/%s/%s/%s", p.apiVersion, p.phoneNumberID, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call cloud api: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cloud api error (status %d): %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (p *WhatsappAPIService) SendMedia(ctx context.Context, to string, m MediaMessage) (SendResult, error) {
	return SendResult{}, fmt.Errorf("SendMedia not implemented for the Business Cloud API provider")
}
