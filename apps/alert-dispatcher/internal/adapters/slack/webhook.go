// Package slack sends the dispatcher’s rendered messages to an incoming
// webhook. It is only exercised against an in-memory HTTP server in tests.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/yhc/quant-engine-go/apps/alert-dispatcher/internal/application"
)

type WebhookSender struct {
	url    string
	client *http.Client
}

func NewWebhookSender(url string, client *http.Client) (*WebhookSender, error) {
	if url == "" {
		return nil, fmt.Errorf("SLACK_WEBHOOK_URL is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &WebhookSender{url: url, client: client}, nil
}

func (s *WebhookSender) Send(ctx context.Context, alert application.Alert) error {
	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: application.SlackText(alert)})
	if err != nil {
		return fmt.Errorf("marshal Slack payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Slack request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("post Slack webhook: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Slack webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}
