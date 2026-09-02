package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yhc/quant-engine-go/apps/alert-dispatcher/internal/application"
)

func TestWebhookSenderPostsApprovalMessage(t *testing.T) {
	var payload struct {
		Text string `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s", request.Method)
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	sender, err := NewWebhookSender(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), application.Alert{Kind: application.AlertKindApproval, ApprovalRequestID: 7, ExpiresAt: time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC), Event: application.SignalEvent{ID: 99, EvaluatedOn: "2026-09-02", AsOf: "2026-09-01", ModelVersion: "v1", Actual: 4.2, Anchor: 4.0, RawDistance: .2, ZScore: 1.5, Signal: "UNDERVALUED"}})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Text == "" || !contains(payload.Text, "Trade approval required") || !contains(payload.Text, "No order has been submitted") {
		t.Fatalf("unexpected Slack text: %q", payload.Text)
	}
}

func contains(text, fragment string) bool {
	for i := 0; i+len(fragment) <= len(text); i++ {
		if text[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
