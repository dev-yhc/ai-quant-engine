package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	tradingdomain "github.com/yhc/quant-engine-go/domains/trading/domain"
)

type memoryRepository struct {
	order  *domain.Order
	alerts []tradingdomain.Portfolio
}

func (r *memoryRepository) Accept(_ context.Context, i domain.Intent, now time.Time) (domain.Order, bool, error) {
	if r.order != nil {
		return *r.order, false, nil
	}
	r.order = &domain.Order{Intent: i, Status: domain.Pending, NextAttemptAt: now, BrokerIdempotencyTill: now.Add(9 * time.Minute)}
	return *r.order, true, nil
}

func (r *memoryRepository) ClaimNext(_ context.Context, now time.Time) (domain.Order, bool, error) {
	if r.order == nil || r.order.Status != domain.Pending || r.order.NextAttemptAt.After(now) {
		return domain.Order{}, false, nil
	}
	r.order.Status = domain.Processing
	return *r.order, true, nil
}

func (r *memoryRepository) MarkSubmitted(_ context.Context, id, brokerID string, _ time.Time) error {
	r.order.Status = domain.Submitted
	r.order.BrokerOrderID = brokerID
	return nil
}

func (r *memoryRepository) Reschedule(_ context.Context, _ string, a int, next time.Time, message string) error {
	r.order.Status = domain.Pending
	r.order.AttemptCount = a
	r.order.NextAttemptAt = next
	r.order.LastError = message
	return nil
}

func (r *memoryRepository) MarkRejected(_ context.Context, _ string, message string, _ time.Time) error {
	r.order.Status = domain.Rejected
	r.order.LastError = message
	return nil
}

func (r *memoryRepository) MarkUnknown(_ context.Context, _ string, message string, _ time.Time) error {
	r.order.Status = domain.Unknown
	r.order.LastError = message
	return nil
}

func (r *memoryRepository) EnqueuePortfolioAlert(_ context.Context, book tradingdomain.Portfolio) error {
	r.alerts = append(r.alerts, book)
	return nil
}

type brokerStub struct {
	id        string
	err       error
	portfolio tradingdomain.Portfolio
	bookErr   error
}

func (b brokerStub) Submit(context.Context, domain.Intent) (string, error) { return b.id, b.err }

func (b brokerStub) Portfolio(context.Context) (tradingdomain.Portfolio, error) {
	return b.portfolio, b.bookErr
}

func testPolicy() domain.RiskPolicy {
	return domain.RiskPolicy{ExecutionEnabled: true, AutoExecutionEnabled: true, AllowedStrategies: map[string]struct{}{"valuation": {}}, AllowedInstruments: map[string]struct{}{"US:AAPL": {}}, MaxQuantity: "10"}
}

func testIntent(now time.Time) domain.Intent {
	return domain.Intent{ID: "o1", SignalEventID: "signal-1", ApprovalRequestID: "approval-1", Strategy: "valuation", Instrument: "US:AAPL", Side: domain.Buy, OrderType: domain.Market, Quantity: "1", IdempotencyKey: "event-1", PolicyVersion: "v1", Mode: domain.ApprovedIntent, ExpiresAt: now.Add(time.Hour)}
}

func TestApprovalIntentIsDurableThenSubmitted(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	repo := &memoryRepository{}
	service := New(repo, brokerStub{id: "broker-1"}, testPolicy())
	service.now = func() time.Time { return now }
	got, created, err := service.SubmitOrderIntent(context.Background(), testIntent(now))
	if err != nil || !created || got.Status != domain.Pending {
		t.Fatalf("accept: %#v %v %v", got, created, err)
	}
	processed, err := service.ProcessOne(context.Background())
	if err != nil || !processed || repo.order.Status != domain.Submitted || repo.order.BrokerOrderID != "broker-1" {
		t.Fatalf("process: %#v %v", repo.order, err)
	}
}

func TestAutoSignalRequiresFeatureFlag(t *testing.T) {
	now := time.Now().UTC()
	p := testPolicy()
	p.AutoExecutionEnabled = false
	s := New(&memoryRepository{}, brokerStub{}, p)
	s.now = func() time.Time { return now }
	i := testIntent(now)
	i.Mode = domain.AutoSignal
	i.ApprovalRequestID = ""
	_, _, err := s.SubmitOrderIntent(context.Background(), i)
	if err == nil {
		t.Fatal("expected automatic execution denial")
	}
}

type temporaryError struct{ error }

func (temporaryError) Retryable() bool { return true }

func TestRetryDoesNotCreateSecondIntent(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	repo := &memoryRepository{}
	s := New(repo, brokerStub{err: temporaryError{errors.New("timeout")}}, testPolicy())
	s.now = func() time.Time { return now }
	_, _, _ = s.SubmitOrderIntent(context.Background(), testIntent(now))
	_, err := s.ProcessOne(context.Background())
	if err != nil || repo.order.Status != domain.Pending || repo.order.AttemptCount != 1 {
		t.Fatalf("order: %#v %v", repo.order, err)
	}
}

func TestAlertCurrentPortfolioQueuesSnapshot(t *testing.T) {
	repository := &memoryRepository{}
	want := tradingdomain.Portfolio{ReportingCurrency: "KRW", TotalMarketValue: "1000"}
	service := New(repository, brokerStub{portfolio: want}, testPolicy())
	got, err := service.AlertCurrentPortfolio(context.Background())
	if err != nil || got.TotalMarketValue != "1000" || len(repository.alerts) != 1 {
		t.Fatalf("alert portfolio: %#v alerts=%#v err=%v", got, repository.alerts, err)
	}
}
