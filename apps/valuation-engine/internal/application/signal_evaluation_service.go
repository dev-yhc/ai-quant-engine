package application

import (
	"context"
	"fmt"

	"github.com/yhc/quant-engine-go/domains/valuation/domain"
)

// SignalRecord is the durable result of a valuation run. The event is the
// input to alert routing; it is never an order command.
type SignalRecord struct {
	EventID          int64
	EventKey         string
	ApprovalRequired bool
}

type SignalEventStore interface {
	RecordUS10YearSignal(context.Context, domain.US10YearResult) (SignalRecord, error)
}

type SignalEvaluationService struct {
	evaluator BondEvaluationService
	store     SignalEventStore
}

func NewSignalEvaluationService(evaluator BondEvaluationService, store SignalEventStore) SignalEvaluationService {
	return SignalEvaluationService{evaluator: evaluator, store: store}
}

func (s SignalEvaluationService) EvaluateAndEnqueueUS10YearSignal(ctx context.Context) (domain.US10YearResult, SignalRecord, error) {
	result, err := s.evaluator.CalculateUSTreasury10YearTheoreticalYield(ctx)
	if err != nil {
		return domain.US10YearResult{}, SignalRecord{}, err
	}
	record, err := s.store.RecordUS10YearSignal(ctx, result)
	if err != nil {
		return domain.US10YearResult{}, SignalRecord{}, fmt.Errorf("record US 10-year signal: %w", err)
	}
	return result, record, nil
}
