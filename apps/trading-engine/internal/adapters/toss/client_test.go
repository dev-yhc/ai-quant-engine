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

func TestMarketSnapshotUsesRESTBaselineAndSelectsInsideQuotes(t *testing.T) {
	client, err := New(Config{ClientID: "id", ClientSecret: "secret", AccountSeq: 7, BaseURL: "https://example.test", HTTPClient: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/oauth2/token":
			return response(200, `{"access_token":"token","expires_in":3600}`), nil
		case "/api/v1/orderbook":
			if request.Header.Get("Authorization") != "Bearer token" || request.URL.Query().Get("symbol") != "005930" {
				t.Fatalf("orderbook request: %s headers=%v", request.URL, request.Header)
			}
			return response(200, `{"result":{"timestamp":"2026-09-02T09:00:00+09:00","currency":"KRW","asks":[{"price":"70200","volume":"1"},{"price":"70100","volume":"2"}],"bids":[{"price":"69900","volume":"3"},{"price":"70000","volume":"4"}]}}`), nil
		case "/api/v1/trades":
			if request.Header.Get("Authorization") != "Bearer token" || request.URL.Query().Get("count") != "50" {
				t.Fatalf("trades request: %s headers=%v", request.URL, request.Header)
			}
			return response(200, `{"result":[{"price":"70000","volume":"10","timestamp":"2026-09-02T09:00:01+09:00","currency":"KRW"}]}`), nil
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
			return nil, nil
		}
	})}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.MarketSnapshot(context.Background(), "KR:005930")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Instrument != "KR:005930" || len(snapshot.Trades) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if bid, ok := snapshot.BestBid(); !ok || bid.Price != "70000" {
		t.Fatalf("best bid = %#v, %v", bid, ok)
	}
	if ask, ok := snapshot.BestAsk(); !ok || ask.Price != "70100" {
		t.Fatalf("best ask = %#v, %v", ask, ok)
	}
}

func TestPortfolioReturnsAllHoldingsWithKRWWeights(t *testing.T) {
	client, err := New(Config{ClientID: "id", ClientSecret: "secret", AccountSeq: 7, BaseURL: "https://example.test", HTTPClient: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/oauth2/token":
			return response(200, `{"access_token":"token","expires_in":3600}`), nil
		case "/api/v1/holdings":
			if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("X-Tossinvest-Account") != "7" {
				t.Fatalf("holdings headers: %v", request.Header)
			}
			return response(200, `{"result":{"marketValue":{"amount":{"krw":"700000","usd":"100"}},"profitLoss":{"amount":{"krw":"100000","usd":"10"}},"dailyProfitLoss":{"amount":{"krw":"10000","usd":"1"}},"items":[{"symbol":"005930","name":"Samsung","marketCountry":"KR","currency":"KRW","quantity":"10","lastPrice":"70000","averagePurchasePrice":"60000","marketValue":{"amount":"700000","amountAfterCost":"690000"},"profitLoss":{"amount":"100000","rate":"0.1667"},"dailyProfitLoss":{"amount":"10000","rate":"0.0145"},"cost":{}},{"symbol":"AAPL","name":"Apple","marketCountry":"US","currency":"USD","quantity":"1","lastPrice":"100","averagePurchasePrice":"90","marketValue":{"amount":"100","amountAfterCost":"99"},"profitLoss":{"amount":"10","rate":"0.1111"},"dailyProfitLoss":{"amount":"1","rate":"0.01"},"cost":{}}]}}`), nil
		case "/api/v1/exchange-rate":
			if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("X-Tossinvest-Account") != "" {
				t.Fatalf("exchange headers: %v", request.Header)
			}
			return response(200, `{"result":{"rate":"1400"}}`), nil
		case "/api/v1/buying-power":
			if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("X-Tossinvest-Account") != "7" {
				t.Fatalf("buying-power headers: %v", request.Header)
			}
			switch request.URL.Query().Get("currency") {
			case "KRW":
				return response(200, `{"result":{"currency":"KRW","cashBuyingPower":"160000"}}`), nil
			case "USD":
				return response(200, `{"result":{"currency":"USD","cashBuyingPower":"0"}}`), nil
			default:
				t.Fatalf("unexpected buying-power query: %s", request.URL.RawQuery)
				return nil, nil
			}
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
			return nil, nil
		}
	})}})
	if err != nil {
		t.Fatal(err)
	}
	book, err := client.Portfolio(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if book.ReportingCurrency != "KRW" || book.TotalMarketValue != "1000000" || book.USDKRWRate != "1400" || len(book.Holdings) != 2 || len(book.Cash) != 2 {
		t.Fatalf("unexpected book: %#v", book)
	}
	if book.Holdings[0].Weight != "0.7" || book.Holdings[1].Weight != "0.14" || book.Holdings[1].MarketValueKRW != "140000" || book.Cash[0].Weight != "0.16" {
		t.Fatalf("unexpected weights: %#v", book.Holdings)
	}
	if book.ProfitLossRate != "0.114" || book.DailyProfitLossRate != "0.0114" {
		t.Fatalf("unexpected rates: %#v", book)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
