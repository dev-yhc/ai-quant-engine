package strategy

import (
	"context"
	"testing"
	"time"

	orderdomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	strategydomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain/strategy"
	tradingdomain "github.com/yhc/quant-engine-go/domains/trading/domain"
)

type memoryRepository struct {
	policy    strategydomain.LadderPolicy
	pending   strategydomain.Exposure
	decisions map[string]Decision
	orders    []orderdomain.Intent
}

func (r *memoryRepository) LoadPolicy(context.Context, string) (strategydomain.LadderPolicy, error) {
	return r.policy, nil
}
func (r *memoryRepository) FindDecision(_ context.Context, eventID, strategyID string) (Decision, bool, error) {
	decision, ok := r.decisions[eventID+":"+strategyID]
	return decision, ok, nil
}
func (r *memoryRepository) PendingExposure(context.Context, string, string) (strategydomain.Exposure, error) {
	return r.pending, nil
}
func (r *memoryRepository) SaveDecisionAndOrder(_ context.Context, d Decision, order *orderdomain.Intent, _ time.Time) (Decision, bool, error) {
	if prior, ok := r.decisions[d.SignalEventID+":"+d.StrategyID]; ok {
		return prior, false, nil
	}
	r.decisions[d.SignalEventID+":"+d.StrategyID] = d
	if order != nil {
		r.orders = append(r.orders, *order)
	}
	return d, true, nil
}

type portfolioStub struct{ book tradingdomain.Portfolio }

func (p portfolioStub) Portfolio(context.Context) (tradingdomain.Portfolio, error) {
	return p.book, nil
}
func (p portfolioStub) USDToKRWRate(context.Context) (string, error) { return "1250", nil }

func enabledPolicy() orderdomain.RiskPolicy {
	return orderdomain.RiskPolicy{ExecutionEnabled: true, AutoExecutionEnabled: true, AllowedStrategies: map[string]struct{}{"us10y-overvalued-ief": {}}, AllowedInstruments: map[string]struct{}{"US:IEF": {}}, MaxOrderAmount: "1000"}
}
func signal(id string, score float64) strategydomain.SignalEvent {
	return strategydomain.SignalEvent{ID: id, StrategyID: "us10y-overvalued-ief", ZScore: score, Signal: strategydomain.Overvalued, ModelVersion: "v1", AsOf: "2026-09-03", EvaluatedAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)}
}

func TestHandleSignalBuildsTwoIEFTranches(t *testing.T) {
	repository := &memoryRepository{policy: strategydomain.IEFOvervaluedV1(), decisions: map[string]Decision{}}
	service := New(repository, portfolioStub{book: tradingdomain.Portfolio{TotalMarketValue: "1000000"}}, enabledPolicy(), strategydomain.IEFOvervaluedV1())
	service.now = func() time.Time { return time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC) }

	first, err := service.HandleSignal(context.Background(), signal("event-1", -0.5))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.Decision.TargetKRW != "500000" || first.Decision.TargetWeight != "0.5" || first.Decision.OrderAmountKRW != "500000" || len(repository.orders) != 1 || repository.orders[0].OrderAmount != "400.000000" {
		t.Fatalf("first result=%#v orders=%#v", first, repository.orders)
	}

	// The first order is still open, so it contributes to effective exposure.
	repository.pending.PendingBuyKRW = "500000"
	second, err := service.HandleSignal(context.Background(), signal("event-2", -1.0))
	if err != nil {
		t.Fatal(err)
	}
	if second.Decision.TargetKRW != "1000000" || second.Decision.TargetWeight != "1" || second.Decision.EffectiveExposureKRW != "500000" || second.Decision.DeltaKRW != "500000" || second.Decision.OrderAmountKRW != "500000" || len(repository.orders) != 2 || repository.orders[1].OrderAmount != "400.000000" {
		t.Fatalf("second result=%#v orders=%#v", second, repository.orders)
	}
}

func TestHandleSignalDoesNotBuyBelowEntryOrDuplicateEvent(t *testing.T) {
	repository := &memoryRepository{policy: strategydomain.IEFOvervaluedV1(), decisions: map[string]Decision{}}
	service := New(repository, portfolioStub{book: tradingdomain.Portfolio{TotalMarketValue: "1000000"}}, enabledPolicy(), strategydomain.IEFOvervaluedV1())
	service.now = func() time.Time { return time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC) }

	below, err := service.HandleSignal(context.Background(), signal("event-below", -0.49))
	if err != nil || below.Decision.OrderID != "" || len(repository.orders) != 0 {
		t.Fatalf("below-entry result=%#v err=%v", below, err)
	}
	first, err := service.HandleSignal(context.Background(), signal("event-duplicate", -0.5))
	if err != nil || !first.Created || len(repository.orders) != 1 {
		t.Fatalf("first result=%#v err=%v", first, err)
	}
	again, err := service.HandleSignal(context.Background(), signal("event-duplicate", -0.5))
	if err != nil || again.Created || len(repository.orders) != 1 || again.Decision.ID != first.Decision.ID {
		t.Fatalf("duplicate result=%#v err=%v", again, err)
	}
}
