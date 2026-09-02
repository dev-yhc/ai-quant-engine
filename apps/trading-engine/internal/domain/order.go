// Package domain contains the trading engine's broker-independent rules.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

type Side string

const (
	Buy  Side = "BUY"
	Sell Side = "SELL"
)

type OrderType string

const (
	Market OrderType = "MARKET"
	Limit  OrderType = "LIMIT"
)

type ExecutionMode string

const (
	ApprovedIntent ExecutionMode = "APPROVED"
	AutoSignal     ExecutionMode = "AUTO_SIGNAL"
)

type Intent struct {
	ID                string
	SignalEventID     string
	ApprovalRequestID string
	Strategy          string
	Instrument        string // US:AAPL or KR:005930
	Side              Side
	OrderType         OrderType
	Quantity          string
	OrderAmount       string
	LimitPrice        string
	IdempotencyKey    string
	PolicyVersion     string
	Mode              ExecutionMode
	ExpiresAt         time.Time
}

func (i Intent) TossClientOrderID() string {
	sum := sha256.Sum256([]byte(i.IdempotencyKey))
	return "qe-" + hex.EncodeToString(sum[:16]) // 35 chars; valid Toss clientOrderId.
}

func (i Intent) MarketAndSymbol() (string, string, error) {
	parts := strings.SplitN(i.Instrument, ":", 2)
	if len(parts) != 2 || (parts[0] != "US" && parts[0] != "KR") || parts[1] == "" {
		return "", "", fmt.Errorf("instrument must be US:<ticker> or KR:<six-digit-symbol>")
	}
	return parts[0], parts[1], nil
}

func (i Intent) Validate(now time.Time) error {
	if i.ID == "" || i.SignalEventID == "" || i.Strategy == "" || i.IdempotencyKey == "" || i.PolicyVersion == "" {
		return fmt.Errorf("id, signal_event_id, strategy, idempotency_key, and policy_version are required")
	}
	if _, _, err := i.MarketAndSymbol(); err != nil {
		return err
	}
	if i.Side != Buy && i.Side != Sell {
		return fmt.Errorf("side must be BUY or SELL")
	}
	if i.OrderType != Market && i.OrderType != Limit {
		return fmt.Errorf("order_type must be MARKET or LIMIT")
	}
	if i.ExpiresAt.IsZero() || !i.ExpiresAt.After(now) {
		return fmt.Errorf("order intent has expired")
	}
	if i.Mode != ApprovedIntent && i.Mode != AutoSignal {
		return fmt.Errorf("mode must be APPROVED or AUTO_SIGNAL")
	}
	if i.Mode == ApprovedIntent && i.ApprovalRequestID == "" {
		return fmt.Errorf("approved intent requires approval_request_id")
	}
	quantity, quantityOK := positiveDecimal(i.Quantity)
	amount, amountOK := positiveDecimal(i.OrderAmount)
	if quantityOK == amountOK { // exactly one must be supplied
		return fmt.Errorf("exactly one of quantity or order_amount is required")
	}
	if i.OrderType == Limit {
		if _, ok := positiveDecimal(i.LimitPrice); !ok {
			return fmt.Errorf("limit_price is required for LIMIT orders")
		}
	} else if i.LimitPrice != "" {
		return fmt.Errorf("limit_price is not allowed for MARKET orders")
	}
	market, _, _ := i.MarketAndSymbol()
	if amountOK && (market != "US" || i.OrderType != Market) {
		return fmt.Errorf("order_amount is only supported for US MARKET orders")
	}
	if quantityOK && strings.Contains(i.Quantity, ".") && !(market == "US" && i.OrderType == Market && i.Side == Sell) {
		return fmt.Errorf("fractional quantity is only supported for US MARKET SELL orders")
	}
	_ = quantity
	_ = amount
	return nil
}

func positiveDecimal(value string) (*big.Rat, bool) {
	if value == "" {
		return nil, false
	}
	n, ok := new(big.Rat).SetString(value)
	return n, ok && n.Sign() > 0
}

type RiskPolicy struct {
	ExecutionEnabled     bool
	AutoExecutionEnabled bool
	KillSwitch           bool
	AllowedStrategies    map[string]struct{}
	AllowedInstruments   map[string]struct{}
	MaxQuantity          string
	MaxOrderAmount       string
}

func (p RiskPolicy) Validate(i Intent, now time.Time) error {
	if err := i.Validate(now); err != nil {
		return err
	}
	if !p.ExecutionEnabled {
		return fmt.Errorf("trading execution is disabled")
	}
	if p.KillSwitch {
		return fmt.Errorf("trading kill switch is enabled")
	}
	if i.Mode == AutoSignal && !p.AutoExecutionEnabled {
		return fmt.Errorf("automatic execution is disabled")
	}
	if _, ok := p.AllowedStrategies[i.Strategy]; !ok {
		return fmt.Errorf("strategy %q is not allowlisted", i.Strategy)
	}
	if _, ok := p.AllowedInstruments[i.Instrument]; !ok {
		return fmt.Errorf("instrument %q is not allowlisted", i.Instrument)
	}
	if i.Quantity != "" && p.MaxQuantity != "" && exceeds(i.Quantity, p.MaxQuantity) {
		return fmt.Errorf("quantity exceeds configured maximum")
	}
	if i.OrderAmount != "" && p.MaxOrderAmount != "" && exceeds(i.OrderAmount, p.MaxOrderAmount) {
		return fmt.Errorf("order amount exceeds configured maximum")
	}
	return nil
}

func exceeds(value, maximum string) bool {
	v, vok := positiveDecimal(value)
	m, mok := positiveDecimal(maximum)
	return vok && mok && v.Cmp(m) > 0
}

type Status string

const (
	Pending    Status = "PENDING"
	Processing Status = "PROCESSING"
	Submitted  Status = "SUBMITTED"
	Rejected   Status = "REJECTED"
	Unknown    Status = "UNKNOWN"
)

type Order struct {
	Intent
	Status                Status
	BrokerOrderID         string
	BrokerClientOrderID   string
	AttemptCount          int
	NextAttemptAt         time.Time
	BrokerIdempotencyTill time.Time
	LastError             string
}
