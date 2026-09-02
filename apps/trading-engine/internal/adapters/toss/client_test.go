package toss

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestSubmitUsesOAuthAccountHeaderAndClientOrderID(t *testing.T) {
	var tokenCalls, orderCalls int
	client, err := New(Config{ClientID: "id", ClientSecret: "secret", AccountSeq: 7, BaseURL: "https://example.test", HTTPClient: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/oauth2/token" {
			tokenCalls++
			return response(200, `{"access_token":"token","expires_in":3600}`), nil
		}
		orderCalls++
		if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("X-Tossinvest-Account") != "7" {
			t.Fatalf("headers: %v", request.Header)
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"clientOrderId":"qe-`) {
			t.Fatalf("payload: %s", body)
		}
		return response(200, `{"result":{"orderId":"order-1"}}`), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.Intent{IdempotencyKey: "same-key", Instrument: "US:AAPL", Side: domain.Buy, OrderType: domain.Market, Quantity: "1", ExpiresAt: time.Now().Add(time.Hour)}
	id, err := client.Submit(context.Background(), intent)
	if err != nil || id != "order-1" || tokenCalls != 1 || orderCalls != 1 {
		t.Fatalf("%q %v token=%d order=%d", id, err, tokenCalls, orderCalls)
	}
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
