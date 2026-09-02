// Package strategy orchestrates durable strategy decisions without owning
// broker execution. Generated intents continue through trading-engine's
// existing order worker and RiskPolicy.
package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	orderdomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	strategydomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain/strategy"
	tradingdomain "github.com/yhc/quant-engine-go/domains/trading/domain"
)

type Repository interface {
	LoadPolicy(context.Context, string) (strategydomain.LadderPolicy, error)
	FindDecision(context.Context, string, string) (Decision, bool, error)
	PendingExposure(context.Context, string, string) (strategydomain.Exposure, error)
	SaveDecisionAndOrder(context.Context, Decision, *orderdomain.Intent, time.Time) (Decision, bool, error)
}

type PortfolioProvider interface {
	Portfolio(context.Context) (tradingdomain.Portfolio, error)
	USDToKRWRate(context.Context) (string, error)
}

type Decision struct {
	ID                   string
	SignalEventID        string
	StrategyID           string
	Instrument           string
	ModelVersion         string
	PolicyVersion        string
	ZScore               float64
	Signal               string
	AsOf                 string
	TargetKRW            string
	TargetWeight         string
	EffectiveExposureKRW string
	DeltaKRW             string
	OrderAmountKRW       string
	OrderID              string
	Reason               string
}

type Result struct {
	Decision Decision
	Created  bool
}

type Service struct {
	repository Repository
	portfolio  PortfolioProvider
	policy     orderdomain.RiskPolicy
	ladder     strategydomain.LadderPolicy
	now        func() time.Time
}

func New(repository Repository, portfolio PortfolioProvider, policy orderdomain.RiskPolicy, ladder strategydomain.LadderPolicy) Service {
	return Service{repository: repository, portfolio: portfolio, policy: policy, ladder: ladder, now: time.Now}
}

// HandleSignal creates at most one decision per (signal_event_id, strategy_id).
// It creates an AUTO_SIGNAL intent only when the target exceeds effective
// exposure and the existing final risk policy permits automatic execution.
func (s Service) HandleSignal(ctx context.Context, event strategydomain.SignalEvent) (Result, error) {
	if event.StrategyID != s.ladder.StrategyID {
		return Result{}, fmt.Errorf("strategy %q is not handled by this service", event.StrategyID)
	}
	if previous, found, err := s.repository.FindDecision(ctx, event.ID, event.StrategyID); err != nil {
		return Result{}, fmt.Errorf("find prior strategy decision: %w", err)
	} else if found {
		return Result{Decision: previous}, nil
	}
	ladder, err := s.repository.LoadPolicy(ctx, event.StrategyID)
	if err != nil {
		return Result{}, fmt.Errorf("load strategy policy: %w", err)
	}
	if !ladder.Enabled {
		return Result{}, fmt.Errorf("strategy %q is disabled", event.StrategyID)
	}
	book, err := s.portfolio.Portfolio(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load trading book: %w", err)
	}
	target, err := ladder.TargetFor(event, book.TotalMarketValue)
	if err != nil {
		return Result{}, err
	}
	pending, err := s.repository.PendingExposure(ctx, event.StrategyID, ladder.Instrument)
	if err != nil {
		return Result{}, fmt.Errorf("load pending strategy exposure: %w", err)
	}
	pending.ConfirmedKRW, err = confirmedExposure(book, ladder.Instrument)
	if err != nil {
		return Result{}, err
	}
	plan, err := ladder.Rebalance(target, pending)
	if err != nil {
		return Result{}, err
	}
	decision := Decision{
		ID:                   decisionID(event.ID, event.StrategyID),
		SignalEventID:        event.ID,
		StrategyID:           event.StrategyID,
		Instrument:           ladder.Instrument,
		ModelVersion:         event.ModelVersion,
		PolicyVersion:        ladder.PolicyVersion,
		ZScore:               event.ZScore,
		Signal:               string(event.Signal),
		AsOf:                 event.AsOf,
		TargetKRW:            target.TargetKRW,
		TargetWeight:         target.TargetWeight,
		EffectiveExposureKRW: plan.EffectiveKRW,
		DeltaKRW:             plan.DeltaKRW,
		OrderAmountKRW:       plan.OrderKRW,
		Reason:               plan.Reason,
	}
	var intent *orderdomain.Intent
	if plan.Buy {
		rate := book.USDKRWRate
		if rate == "" {
			rate, err = s.portfolio.USDToKRWRate(ctx)
			if err != nil {
				return Result{}, fmt.Errorf("load USD/KRW rate: %w", err)
			}
		}
		amountUSD, err := krwToUSD(plan.OrderKRW, rate)
		if err != nil {
			return Result{}, err
		}
		order := orderdomain.Intent{
			ID:             orderID(event.ID, event.StrategyID),
			SignalEventID:  event.ID,
			Strategy:       event.StrategyID,
			Instrument:     ladder.Instrument,
			Side:           orderdomain.Buy,
			OrderType:      orderdomain.Market,
			OrderAmount:    amountUSD,
			IdempotencyKey: "strategy:" + event.StrategyID + ":signal:" + event.ID,
			PolicyVersion:  ladder.PolicyVersion,
			Mode:           orderdomain.AutoSignal,
			ExpiresAt:      event.EvaluatedAt.UTC().Add(24 * time.Hour),
		}
		if err := s.policy.Validate(order, s.now().UTC()); err != nil {
			return Result{}, fmt.Errorf("validate strategy order: %w", err)
		}
		decision.OrderID = order.ID
		intent = &order
	}
	saved, created, err := s.repository.SaveDecisionAndOrder(ctx, decision, intent, s.now().UTC())
	if err != nil {
		return Result{}, fmt.Errorf("save strategy decision: %w", err)
	}
	return Result{Decision: saved, Created: created}, nil
}

func confirmedExposure(book tradingdomain.Portfolio, instrument string) (string, error) {
	for _, holding := range book.Holdings {
		if holding.Instrument == instrument {
			if _, ok := new(big.Rat).SetString(holding.MarketValueKRW); !ok {
				return "", fmt.Errorf("invalid KRW market value for %s", instrument)
			}
			return holding.MarketValueKRW, nil
		}
	}
	return "0", nil
}

func krwToUSD(krw, rate string) (string, error) {
	krwValue, ok := new(big.Rat).SetString(krw)
	if !ok || krwValue.Sign() <= 0 {
		return "", fmt.Errorf("order KRW amount must be positive")
	}
	rateValue, ok := new(big.Rat).SetString(rate)
	if !ok || rateValue.Sign() <= 0 {
		return "", fmt.Errorf("USD/KRW rate must be positive")
	}
	return new(big.Rat).Quo(krwValue, rateValue).FloatString(6), nil
}

func stableID(prefix, signalID, strategyID string) string {
	sum := sha256.Sum256([]byte(strategyID + "\x00" + signalID))
	return prefix + "-" + hex.EncodeToString(sum[:12])
}
func decisionID(signalID, strategyID string) string {
	return stableID("decision", signalID, strategyID)
}
func orderID(signalID, strategyID string) string { return stableID("order", signalID, strategyID) }
