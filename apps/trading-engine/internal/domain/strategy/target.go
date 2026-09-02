package strategy

import (
	"fmt"
	"math/big"
)

type Exposure struct {
	ConfirmedKRW   string
	PendingBuyKRW  string
	PendingSellKRW string
}

type RebalancePlan struct {
	Target       Target
	EffectiveKRW string
	DeltaKRW     string
	OrderKRW     string
	Buy          bool
	Reason       string
}

// Rebalance subtracts confirmed holdings and still-open orders from a target.
// The initial IEF strategy only creates BUY plans; exit policy is intentionally
// deferred until it is explicitly specified.
func (p LadderPolicy) Rebalance(target Target, exposure Exposure) (RebalancePlan, error) {
	confirmed, err := decimalOrZero(exposure.ConfirmedKRW)
	if err != nil {
		return RebalancePlan{}, fmt.Errorf("confirmed exposure: %w", err)
	}
	pendingBuy, err := decimalOrZero(exposure.PendingBuyKRW)
	if err != nil {
		return RebalancePlan{}, fmt.Errorf("pending buy exposure: %w", err)
	}
	pendingSell, err := decimalOrZero(exposure.PendingSellKRW)
	if err != nil {
		return RebalancePlan{}, fmt.Errorf("pending sell exposure: %w", err)
	}
	effective := new(big.Rat).Add(confirmed, pendingBuy)
	effective.Sub(effective, pendingSell)
	if effective.Sign() < 0 {
		effective.SetInt64(0)
	}
	plan := RebalancePlan{Target: target, EffectiveKRW: decimalString(effective), Reason: target.Reason}
	if !target.Eligible {
		return plan, nil
	}
	targetAmount, err := decimal(target.TargetKRW)
	if err != nil {
		return RebalancePlan{}, fmt.Errorf("target exposure: %w", err)
	}
	delta := new(big.Rat).Sub(targetAmount, effective)
	plan.DeltaKRW = decimalString(delta)
	if delta.Sign() <= 0 {
		plan.Reason = "target is already covered by effective exposure"
		return plan, nil
	}
	maxStep, _ := decimal(p.MaxOrderStepKRW)
	if delta.Cmp(maxStep) > 0 {
		delta = maxStep
	}
	plan.OrderKRW = decimalString(delta)
	plan.Buy = true
	plan.Reason = "increase IEF exposure toward target"
	return plan, nil
}

func decimalOrZero(value string) (*big.Rat, error) {
	if value == "" {
		return new(big.Rat), nil
	}
	return decimal(value)
}
