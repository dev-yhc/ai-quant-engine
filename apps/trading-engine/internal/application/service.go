package application

import (
	"context"
	"fmt"
	"time"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
)

// Repository is owned by trading-engine. No valuation or dispatcher table is read here.
type Repository interface {
	Accept(context.Context, domain.Intent, time.Time) (domain.Order, bool, error)
	ClaimNext(context.Context, time.Time) (domain.Order, bool, error)
	MarkSubmitted(context.Context, string, string, time.Time) error
	Reschedule(context.Context, string, int, time.Time, string) error
	MarkRejected(context.Context, string, string, time.Time) error
	MarkUnknown(context.Context, string, string, time.Time) error
}

type Broker interface {
	Submit(context.Context, domain.Intent) (string, error)
}

type Service struct {
	repository Repository
	broker     Broker
	policy     domain.RiskPolicy
	now        func() time.Time
}

func New(repository Repository, broker Broker, policy domain.RiskPolicy) Service {
	return Service{repository: repository, broker: broker, policy: policy, now: time.Now}
}

// SubmitOrderIntent durably accepts an already-approved intent. It never calls a broker inline.
func (s Service) SubmitOrderIntent(ctx context.Context, intent domain.Intent) (domain.Order, bool, error) {
	now := s.now().UTC()
	if err := s.policy.Validate(intent, now); err != nil {
		return domain.Order{}, false, err
	}
	order, created, err := s.repository.Accept(ctx, intent, now)
	if err != nil {
		return domain.Order{}, false, fmt.Errorf("durably accept order intent: %w", err)
	}
	return order, created, nil
}

// ProcessOne performs the final risk re-check immediately before the external order call.
func (s Service) ProcessOne(ctx context.Context) (bool, error) {
	now := s.now().UTC()
	order, found, err := s.repository.ClaimNext(ctx, now)
	if err != nil || !found {
		return found, err
	}
	if err := s.policy.Validate(order.Intent, now); err != nil {
		return true, s.repository.MarkRejected(ctx, order.ID, err.Error(), now)
	}
	brokerOrderID, err := s.broker.Submit(ctx, order.Intent)
	if err == nil {
		return true, s.repository.MarkSubmitted(ctx, order.ID, brokerOrderID, now)
	}
	if retryable(err) && now.Before(order.BrokerIdempotencyTill) {
		delay := retryDelay(order.AttemptCount)
		return true, s.repository.Reschedule(ctx, order.ID, order.AttemptCount+1, now.Add(delay), err.Error())
	}
	// Do not retry after Toss's ten-minute clientOrderId window: that could create a second order.
	return true, s.repository.MarkUnknown(ctx, order.ID, err.Error(), now)
}

type Retryable interface{ Retryable() bool }

func retryable(err error) bool { r, ok := err.(Retryable); return ok && r.Retryable() }
func retryDelay(attempt int) time.Duration {
	if attempt > 5 {
		attempt = 5
	}
	return time.Second * time.Duration(1<<attempt)
}
