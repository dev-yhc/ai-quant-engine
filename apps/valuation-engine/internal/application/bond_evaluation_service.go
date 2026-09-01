package application

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("US Treasury 10-year theoretical yield calculation is not implemented")

type BondEvaluationService struct{}

func (BondEvaluationService) CalculateUSTreasury10YearTheoreticalYield(context.Context) (float64, error) {
	// TODO: calculate the theoretical yield from the collected market and macro data.
	return 0, ErrNotImplemented
}
