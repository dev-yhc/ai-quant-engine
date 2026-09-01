package application

import (
	"context"
	"fmt"

	"github.com/yhc/quant-engine-go/domains/valuation/domain"
)

type US10YearInputRepository interface {
	LoadUS10YearInput(context.Context) (domain.US10YearInput, error)
}

type BondEvaluationService struct {
	repository US10YearInputRepository
	config     domain.EngineConfig
}

type InputError struct{ Err error }

func (e InputError) Error() string { return e.Err.Error() }
func (e InputError) Unwrap() error { return e.Err }

func NewBondEvaluationService(repository US10YearInputRepository) BondEvaluationService {
	return BondEvaluationService{repository: repository, config: domain.DefaultEngineConfig()}
}

func (s BondEvaluationService) CalculateUSTreasury10YearTheoreticalYield(ctx context.Context) (domain.US10YearResult, error) {
	input, err := s.repository.LoadUS10YearInput(ctx)
	if err != nil {
		return domain.US10YearResult{}, fmt.Errorf("load US 10-year valuation input: %w", err)
	}
	result, err := domain.EvaluateUS10Year(input, s.config)
	if err != nil {
		return domain.US10YearResult{}, InputError{Err: fmt.Errorf("evaluate US 10-year Treasury: %w", err)}
	}
	return result, nil
}
