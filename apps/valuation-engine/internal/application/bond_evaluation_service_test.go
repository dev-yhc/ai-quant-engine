package application

import (
	"context"
	"errors"
	"testing"
)

func TestCalculateUSTreasury10YearTheoreticalYieldIsExplicitlyUnimplemented(t *testing.T) {
	_, err := (BondEvaluationService{}).CalculateUSTreasury10YearTheoreticalYield(context.Background())
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("error = %v, want ErrNotImplemented", err)
	}
}
