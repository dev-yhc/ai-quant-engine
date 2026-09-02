package strategy

import (
	"fmt"
	"math/big"
	"strings"
)

// Band maps a direction-adjusted score threshold to a total target exposure.
// The target is cumulative, not a new order amount.
type Band struct {
	ScoreThreshold float64
	TargetKRW      string
}

// LadderPolicy is a versioned, broker-independent strategy policy.
type LadderPolicy struct {
	StrategyID              string
	Instrument              string
	PolicyVersion           string
	Enabled                 bool
	DirectionMultiplier     float64
	EntryBands              []Band
	MaxExposureKRW          string
	MaxPortfolioWeight      string
	MaxOrderStepKRW         string
	RequireOvervaluedSignal bool
}

// IEFOvervaluedV1 is the deliberately narrow initial policy: buy IEF in two
// KRW tranches as a US 10Y valuation signal becomes more negative.
func IEFOvervaluedV1() LadderPolicy {
	return LadderPolicy{
		StrategyID:              "us10y-overvalued-ief",
		Instrument:              "US:IEF",
		PolicyVersion:           "v1",
		Enabled:                 true,
		DirectionMultiplier:     -1,
		EntryBands:              []Band{{ScoreThreshold: 0.5, TargetKRW: "500000"}, {ScoreThreshold: 1.0, TargetKRW: "1000000"}},
		MaxExposureKRW:          "1000000",
		MaxPortfolioWeight:      "1",
		MaxOrderStepKRW:         "500000",
		RequireOvervaluedSignal: true,
	}
}

type Target struct {
	Score        float64
	Eligible     bool
	TargetKRW    string
	TargetWeight string
	Reason       string
}

// TargetFor returns a cumulative target exposure. A non-eligible signal does
// not imply a sell; this initial policy enters IEF only and records a hold.
func (p LadderPolicy) TargetFor(event SignalEvent, portfolioValueKRW string) (Target, error) {
	if err := p.Validate(); err != nil {
		return Target{}, err
	}
	if err := event.Validate(); err != nil {
		return Target{}, err
	}
	portfolio, err := decimal(portfolioValueKRW)
	if err != nil || portfolio.Sign() <= 0 {
		return Target{}, fmt.Errorf("portfolio value must be a positive KRW amount")
	}
	score := p.DirectionMultiplier * event.ZScore
	if p.RequireOvervaluedSignal && event.Signal != Overvalued {
		return Target{Score: score, Reason: "signal is not OVERVALUED"}, nil
	}
	var target *big.Rat
	for _, band := range p.EntryBands {
		if score >= band.ScoreThreshold {
			target, err = decimal(band.TargetKRW)
			if err != nil {
				return Target{}, fmt.Errorf("entry band target: %w", err)
			}
		}
	}
	if target == nil {
		return Target{Score: score, Reason: "score is below first entry band"}, nil
	}
	maxExposure, _ := decimal(p.MaxExposureKRW)
	maxByWeight := new(big.Rat).Mul(portfolio, mustDecimal(p.MaxPortfolioWeight))
	if target.Cmp(maxExposure) > 0 {
		target = maxExposure
	}
	if target.Cmp(maxByWeight) > 0 {
		target = maxByWeight
	}
	weight := new(big.Rat).Quo(target, portfolio)
	return Target{Score: score, Eligible: true, TargetKRW: decimalString(target), TargetWeight: decimalString(weight), Reason: "entry band reached"}, nil
}

func (p LadderPolicy) Validate() error {
	if p.StrategyID == "" || p.Instrument == "" || p.PolicyVersion == "" || p.DirectionMultiplier == 0 || len(p.EntryBands) == 0 {
		return fmt.Errorf("strategy_id, instrument, policy_version, direction_multiplier, and entry bands are required")
	}
	previousThreshold := -1.0
	previousTarget := new(big.Rat)
	for _, band := range p.EntryBands {
		target, err := decimal(band.TargetKRW)
		if err != nil || band.ScoreThreshold <= previousThreshold || target.Cmp(previousTarget) < 0 {
			return fmt.Errorf("entry bands must have ascending thresholds and cumulative positive targets")
		}
		previousThreshold, previousTarget = band.ScoreThreshold, target
	}
	for _, amount := range []string{p.MaxExposureKRW, p.MaxOrderStepKRW, p.MaxPortfolioWeight} {
		value, err := decimal(amount)
		if err != nil || value.Sign() <= 0 {
			return fmt.Errorf("policy limits must be positive decimals")
		}
	}
	return nil
}

func decimal(value string) (*big.Rat, error) {
	result, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal", value)
	}
	return result, nil
}

func mustDecimal(value string) *big.Rat { result, _ := decimal(value); return result }

func decimalString(value *big.Rat) string {
	if value.IsInt() {
		return value.Num().String()
	}
	return strings.TrimRight(strings.TrimRight(value.FloatString(6), "0"), ".")
}
