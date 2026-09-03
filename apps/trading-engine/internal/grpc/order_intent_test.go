package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/application"
	strategyapp "github.com/yhc/quant-engine-go/apps/trading-engine/internal/application/strategy"
	orderdomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	strategydomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain/strategy"
	tradingdomain "github.com/yhc/quant-engine-go/domains/trading/domain"
	"google.golang.org/protobuf/types/known/structpb"
)

type strategyRepoStub struct {
	policy    strategydomain.LadderPolicy
	pending   strategydomain.Exposure
	decisions map[string]strategyapp.Decision
	orders    []orderdomain.Intent
}

func (r *strategyRepoStub) LoadPolicy(context.Context, string) (strategydomain.LadderPolicy, error) {
	return r.policy, nil
}

func (r *strategyRepoStub) FindDecision(_ context.Context, eventID, strategyID string) (strategyapp.Decision, bool, error) {
	decision, ok := r.decisions[eventID+":"+strategyID]
	return decision, ok, nil
}

func (r *strategyRepoStub) PendingExposure(context.Context, string, string) (strategydomain.Exposure, error) {
	return r.pending, nil
}

func (r *strategyRepoStub) SaveDecisionAndOrder(_ context.Context, d strategyapp.Decision, order *orderdomain.Intent, _ time.Time) (strategyapp.Decision, bool, error) {
	key := d.SignalEventID + ":" + d.StrategyID
	if prior, ok := r.decisions[key]; ok {
		return prior, false, nil
	}
	r.decisions[key] = d
	if order != nil {
		r.orders = append(r.orders, *order)
	}
	return d, true, nil
}

type strategyPortfolioStub struct{ book tradingdomain.Portfolio }

func (p strategyPortfolioStub) Portfolio(context.Context) (tradingdomain.Portfolio, error) {
	return p.book, nil
}

func (p strategyPortfolioStub) USDToKRWRate(context.Context) (string, error) { return "1250", nil }

func TestHandleSignalGRPCPlansTwoIEFOrders(t *testing.T) {
	repository := &strategyRepoStub{policy: strategydomain.IEFOvervaluedV1(), decisions: map[string]strategyapp.Decision{}}
	risk := orderdomain.RiskPolicy{ExecutionEnabled: true, AutoExecutionEnabled: true, AllowedStrategies: map[string]struct{}{"us10y-overvalued-ief": {}}, AllowedInstruments: map[string]struct{}{"US:IEF": {}}, MaxOrderAmount: "1000"}
	strategyService := strategyapp.New(repository, strategyPortfolioStub{book: tradingdomain.Portfolio{TotalMarketValue: "1000000"}}, risk, strategydomain.IEFOvervaluedV1())
	server := NewServer(application.Service{}, strategyService)

	first := callHandleSignal(t, server, "event-1", -0.5)
	if first["target_krw"] != "500000" || first["target_weight"] != "0.5" || first["effective_exposure_krw"] != "0" || first["delta_krw"] != "500000" || first["order_amount_krw"] != "500000" || first["order_intent_id"] == "" {
		t.Fatalf("first output = %#v", first)
	}
	if len(repository.orders) != 1 || repository.orders[0].OrderAmount != "400.000000" || repository.orders[0].Mode != orderdomain.AutoSignal {
		t.Fatalf("first order = %#v", repository.orders)
	}

	repository.pending.PendingBuyKRW = "500000"
	second := callHandleSignal(t, server, "event-2", -1.0)
	if second["target_krw"] != "1000000" || second["target_weight"] != "1" || second["effective_exposure_krw"] != "500000" || second["delta_krw"] != "500000" || second["order_amount_krw"] != "500000" {
		t.Fatalf("second output = %#v", second)
	}
	if len(repository.orders) != 2 || repository.orders[1].OrderAmount != "400.000000" {
		t.Fatalf("second order = %#v", repository.orders)
	}
}

func callHandleSignal(t *testing.T, server *Server, eventID string, zScore float64) map[string]any {
	t.Helper()
	request, err := structpb.NewStruct(map[string]any{
		"signal_event_id": eventID, "strategy_id": "us10y-overvalued-ief",
		"evaluated_at": "2099-01-01T00:00:00Z", "z_score": zScore,
		"signal": "OVERVALUED", "model_version": "v1", "as_of": "2026-09-03",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.HandleSignal(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return response.AsMap()
}
