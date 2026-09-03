package application

import (
	"context"
	"testing"
	"time"

	tradingdomain "github.com/yhc/quant-engine-go/domains/trading/domain"
)

type repositorySpy struct {
	signals         []SignalOutboxItem
	alerts          []Alert
	routed          []SignalOutboxItem
	deliveredAlerts []int64
}

func (r *repositorySpy) ClaimSignal(context.Context) (SignalOutboxItem, bool, error) {
	if len(r.signals) == 0 {
		return SignalOutboxItem{}, false, nil
	}
	item := r.signals[0]
	r.signals = r.signals[1:]
	return item, true, nil
}

func (r *repositorySpy) RouteSignal(_ context.Context, item SignalOutboxItem) error {
	r.routed = append(r.routed, item)
	r.alerts = append(r.alerts, Alert{ID: 1, Kind: AlertKindInformation, Event: item.Event})
	if item.Event.ApprovalRequired {
		r.alerts = append(r.alerts, Alert{ID: 2, Kind: AlertKindApproval, Event: item.Event, ApprovalRequestID: 42, ExpiresAt: time.Now().Add(time.Hour)})
	}
	return nil
}

func (*repositorySpy) MarkSignalDelivered(context.Context, int64) error    { return nil }

func (*repositorySpy) MarkSignalRetry(context.Context, int64, error) error { return nil }

func (r *repositorySpy) ClaimAlert(context.Context) (Alert, bool, error) {
	if len(r.alerts) == 0 {
		return Alert{}, false, nil
	}
	alert := r.alerts[0]
	r.alerts = r.alerts[1:]
	return alert, true, nil
}

func (r *repositorySpy) MarkAlertDelivered(_ context.Context, alert Alert) error {
	r.deliveredAlerts = append(r.deliveredAlerts, alert.ID)
	return nil
}

func (*repositorySpy) MarkAlertRetry(context.Context, Alert, error) error { return nil }

type senderSpy struct{ alerts []Alert }

func (s *senderSpy) Send(_ context.Context, alert Alert) error {
	s.alerts = append(s.alerts, alert)
	return nil
}

func TestPortfolioAlertContainsEachHoldingAndWeight(t *testing.T) {
	book := tradingdomain.Portfolio{
		AsOf:              time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC),
		ReportingCurrency: "KRW",
		TotalMarketValue:  "1000000",
		Holdings: []tradingdomain.Holding{{
			Instrument: "US:AAPL", Name: "Apple", Quantity: "1", Currency: "USD", MarketValueKRW: "700000", Weight: "0.7",
		}},
	}
	text := SlackText(Alert{Kind: AlertKindPortfolio, Portfolio: &book})
	if !contains(text, "US:AAPL") || !contains(text, "70%") || !contains(text, "1000000") {
		t.Fatalf("portfolio Slack text: %q", text)
	}
}

func TestRunOnceRoutesInformationAndApprovalAlerts(t *testing.T) {
	repository := &repositorySpy{signals: []SignalOutboxItem{{ID: 10, Event: SignalEvent{ID: 99, Instrument: "US_TREASURY_10Y", EvaluatedOn: "2026-09-02", AsOf: "2026-09-01", ModelVersion: "v1", Actual: 4.2, Anchor: 4.0, ZScore: 1.5, Signal: "UNDERVALUED", ApprovalRequired: true}}}}
	sender := &senderSpy{}
	if err := NewDispatcher(repository, sender).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sender.alerts) != 2 {
		t.Fatalf("sent %d alerts, want information plus approval", len(sender.alerts))
	}
	if sender.alerts[0].Kind != AlertKindInformation || sender.alerts[1].Kind != AlertKindApproval {
		t.Fatalf("unexpected alert kinds: %#v", sender.alerts)
	}
	if text := SlackText(sender.alerts[1]); text == "" || !contains(text, "No order has been submitted") {
		t.Fatalf("approval text must not imply an order: %q", text)
	}
}

func contains(text, fragment string) bool {
	return len(text) >= len(fragment) && (text == fragment || stringContains(text, fragment))
}

func stringContains(text, fragment string) bool {
	for i := 0; i+len(fragment) <= len(text); i++ {
		if text[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
