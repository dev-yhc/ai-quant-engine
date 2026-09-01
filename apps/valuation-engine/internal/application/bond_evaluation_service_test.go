package application

import (
	"context"
	"testing"
	"time"

	"github.com/yhc/quant-engine-go/domains/valuation/domain"
)

type repositoryStub struct{ input domain.US10YearInput }

func (r repositoryStub) LoadUS10YearInput(context.Context) (domain.US10YearInput, error) {
	return r.input, nil
}

func TestCalculateUSTreasury10YearTheoreticalYield(t *testing.T) {
	service := BondEvaluationService{repository: repositoryStub{input: syntheticInput(800)}, config: domain.EngineConfig{RegressionWindow: 200, NormalizationWindow: 100, MinimumSamples: 30}}
	result, err := service.CalculateUSTreasury10YearTheoreticalYield(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Date.IsZero() || result.Signal == "" {
		t.Fatalf("incomplete result: %#v", result)
	}
}

func syntheticInput(count int) domain.US10YearInput {
	var input domain.US10YearInput
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		date := start.AddDate(0, 0, i)
		cpi := 250 * (1 + .00008*float64(i))
		gdp := 2 + .4*float64((i%31)-15)/15
		slope := .6 + .2*float64((i%23)-11)/11
		realYield := 1.1 + .1*float64((i%17)-8)/8
		inflation := (cpi/(250*(1+.00008*float64(max(0, i-365)))) - 1) * 100
		actual := 1.5 + .25*inflation + .15*gdp + .2*slope + .4*realYield + .15*float64((i%13)-6)/6
		add := func(target *[]domain.Observation, value float64) {
			*target = append(*target, domain.Observation{Date: date, Value: value})
		}
		add(&input.ActualYield, actual)
		add(&input.BreakevenInflation, 2.2+.05*float64(i%11)/10)
		add(&input.RealYield, realYield)
		add(&input.TermPremium, .4+.02*float64(i%7))
		add(&input.NaturalRate, .7+.01*float64(i%5))
		add(&input.Treasury2Year, 3+slope)
		add(&input.Treasury3Month, 3)
		add(&input.CPI, cpi)
		add(&input.GDPGrowth, gdp)
	}
	return input
}
