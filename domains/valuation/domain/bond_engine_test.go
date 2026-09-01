package domain

import (
	"math"
	"testing"
	"time"
)

func TestEvaluateUS10YearProducesThreeAnchorsAndNormalizedSignal(t *testing.T) {
	result, err := EvaluateUS10Year(testInput(800), EngineConfig{RegressionWindow: 200, NormalizationWindow: 100, MinimumSamples: 30})
	if err != nil {
		t.Fatal(err)
	}
	wantComposite := (result.MacroAnchor + result.StatisticalAnchor + result.RegressionAnchor) / 3
	if math.Abs(result.Anchor-wantComposite) > 1e-10 {
		t.Fatalf("anchor = %f, want %f", result.Anchor, wantComposite)
	}
	if math.Abs(result.Delta/result.DistanceStdDev-result.ZScore) > 1e-10 {
		t.Fatalf("invalid z-score: %#v", result)
	}
}

func TestEvaluateUS10YearRejectsMissingRequiredSeries(t *testing.T) {
	input := testInput(800)
	input.NaturalRate = nil
	if _, err := EvaluateUS10Year(input, EngineConfig{RegressionWindow: 200, NormalizationWindow: 100, MinimumSamples: 30}); err == nil {
		t.Fatal("expected error")
	}
}

func testInput(count int) US10YearInput {
	var input US10YearInput
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		date := start.AddDate(0, 0, i)
		cpi := 250 * (1 + .00008*float64(i))
		gdp := 2 + .4*float64((i%31)-15)/15
		slope := .6 + .2*float64((i%23)-11)/11
		realYield := 1.1 + .1*float64((i%17)-8)/8
		inflation := (cpi/(250*(1+.00008*float64(max(0, i-365)))) - 1) * 100
		actual := 1.5 + .25*inflation + .15*gdp + .2*slope + .4*realYield + .15*float64((i%13)-6)/6
		add := func(target *[]Observation, value float64) {
			*target = append(*target, Observation{Date: date, Value: value})
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
