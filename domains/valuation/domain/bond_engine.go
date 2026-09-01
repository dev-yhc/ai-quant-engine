package domain

import (
	"fmt"
	"math"
	"sort"
	"time"
)

const regressionFeatureCount = 5

type regressionRow struct {
	features [regressionFeatureCount]float64
	actual   float64
}

type EngineConfig struct {
	RegressionWindow    int
	NormalizationWindow int
	MinimumSamples      int
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{RegressionWindow: 1260, NormalizationWindow: 756, MinimumSamples: 60}
}

// EvaluateUS10Year implements the ADR-001 dynamic multi-anchor model. Every
// estimator at date t uses only observations available on or before t.
func EvaluateUS10Year(input US10YearInput, config EngineConfig) (US10YearResult, error) {
	if config.RegressionWindow <= 0 || config.NormalizationWindow <= 0 || config.MinimumSamples < 2 {
		return US10YearResult{}, fmt.Errorf("invalid engine configuration")
	}
	actual := normalized(input.ActualYield)
	if len(actual) < config.MinimumSamples*2 {
		return US10YearResult{}, fmt.Errorf("at least %d actual-yield observations are required", config.MinimumSamples*2)
	}

	series := struct {
		breakeven, realYield, termPremium, naturalRate, treasury2Y, treasury3M, cpi, gdp []Observation
	}{
		normalized(input.BreakevenInflation), normalized(input.RealYield), normalized(input.TermPremium),
		normalized(input.NaturalRate), normalized(input.Treasury2Year), normalized(input.Treasury3Month),
		normalized(input.CPI), normalized(input.GDPGrowth),
	}
	for name, observations := range map[string][]Observation{
		"breakeven inflation": series.breakeven, "real yield": series.realYield,
		"term premium": series.termPremium, "natural rate": series.naturalRate,
		"2-year Treasury": series.treasury2Y, "3-month Treasury": series.treasury3M,
		"CPI": series.cpi, "GDP growth": series.gdp,
	} {
		if len(observations) == 0 {
			return US10YearResult{}, fmt.Errorf("%s observations are required", name)
		}
	}

	var rows []regressionRow
	var distances []float64
	level, variance := actual[0].Value, 1.0
	differenceStats := onlineVariance{}
	var latest *US10YearResult

	for i, observation := range actual {
		if i > 0 {
			differenceStats.Add(observation.Value - actual[i-1].Value)
		}
		differenceVariance := math.Max(differenceStats.Variance(), 1e-4)
		processVariance, measurementVariance := differenceVariance*.05, differenceVariance
		predictedVariance := variance + processVariance
		gain := predictedVariance / (predictedVariance + measurementVariance)
		level += gain * (observation.Value - level)
		variance = (1 - gain) * predictedVariance

		breakeven, ok1 := latestAt(series.breakeven, observation.Date)
		realYield, ok2 := latestAt(series.realYield, observation.Date)
		termPremium, ok3 := latestAt(series.termPremium, observation.Date)
		naturalRate, ok4 := latestAt(series.naturalRate, observation.Date)
		twoYear, ok5 := latestAt(series.treasury2Y, observation.Date)
		threeMonth, ok6 := latestAt(series.treasury3M, observation.Date)
		gdpGrowth, ok7 := latestAt(series.gdp, observation.Date)
		cpiNow, ok8 := latestAt(series.cpi, observation.Date)
		cpiYearAgo, ok9 := latestAt(series.cpi, observation.Date.AddDate(-1, 0, 0))
		if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7 && ok8 && ok9) || cpiYearAgo == 0 {
			continue
		}
		cpiInflation := (cpiNow/cpiYearAgo - 1) * 100
		features := [regressionFeatureCount]float64{1, cpiInflation, gdpGrowth, twoYear - threeMonth, realYield}

		if len(rows) >= config.MinimumSamples {
			window := rows[max(0, len(rows)-config.RegressionWindow):]
			coefficients, err := ordinaryLeastSquares(window)
			if err == nil {
				regressionAnchor := dot(coefficients, features)
				macroAnchor := naturalRate + breakeven + termPremium
				anchor := (macroAnchor + level + regressionAnchor) / 3
				rawDistance := observation.Value - anchor
				if len(distances) >= config.MinimumSamples {
					normalization := distances[max(0, len(distances)-config.NormalizationWindow):]
					bias, standardDeviation := meanAndStdDev(normalization)
					if standardDeviation > 0 {
						delta := rawDistance - bias
						result := US10YearResult{Date: observation.Date, Actual: observation.Value, Anchor: anchor, MacroAnchor: macroAnchor, StatisticalAnchor: level, RegressionAnchor: regressionAnchor, RawDistance: rawDistance, Bias: bias, Delta: delta, DistanceStdDev: standardDeviation, ZScore: delta / standardDeviation}
						result.Signal = valuationSignal(result.ZScore)
						latest = &result
					}
				}
				distances = append(distances, rawDistance)
			}
		}
		rows = append(rows, regressionRow{features: features, actual: observation.Value})
	}
	if latest == nil {
		return US10YearResult{}, fmt.Errorf("insufficient aligned history to calculate a normalized valuation")
	}
	return *latest, nil
}

func normalized(values []Observation) []Observation {
	result := append([]Observation(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i].Date.Before(result[j].Date) })
	return result
}

func latestAt(values []Observation, date time.Time) (float64, bool) {
	index := sort.Search(len(values), func(i int) bool { return values[i].Date.After(date) }) - 1
	if index < 0 {
		return 0, false
	}
	return values[index].Value, true
}

type onlineVariance struct {
	count    int
	mean, m2 float64
}

func (s *onlineVariance) Add(value float64) {
	s.count++
	delta := value - s.mean
	s.mean += delta / float64(s.count)
	s.m2 += delta * (value - s.mean)
}
func (s onlineVariance) Variance() float64 {
	if s.count < 2 {
		return 0
	}
	return s.m2 / float64(s.count-1)
}

func ordinaryLeastSquares(rows []regressionRow) ([regressionFeatureCount]float64, error) {
	var matrix [regressionFeatureCount][regressionFeatureCount + 1]float64
	for _, row := range rows {
		for i := 0; i < regressionFeatureCount; i++ {
			for j := 0; j < regressionFeatureCount; j++ {
				matrix[i][j] += row.features[i] * row.features[j]
			}
			matrix[i][regressionFeatureCount] += row.features[i] * row.actual
		}
	}
	for column := 0; column < regressionFeatureCount; column++ {
		pivot := column
		for row := column + 1; row < regressionFeatureCount; row++ {
			if math.Abs(matrix[row][column]) > math.Abs(matrix[pivot][column]) {
				pivot = row
			}
		}
		if math.Abs(matrix[pivot][column]) < 1e-10 {
			return [regressionFeatureCount]float64{}, fmt.Errorf("singular regression matrix")
		}
		matrix[column], matrix[pivot] = matrix[pivot], matrix[column]
		divisor := matrix[column][column]
		for j := column; j <= regressionFeatureCount; j++ {
			matrix[column][j] /= divisor
		}
		for row := 0; row < regressionFeatureCount; row++ {
			if row != column {
				factor := matrix[row][column]
				for j := column; j <= regressionFeatureCount; j++ {
					matrix[row][j] -= factor * matrix[column][j]
				}
			}
		}
	}
	var coefficients [regressionFeatureCount]float64
	for i := range coefficients {
		coefficients[i] = matrix[i][regressionFeatureCount]
	}
	return coefficients, nil
}

func dot(left, right [regressionFeatureCount]float64) float64 {
	result := 0.0
	for i := range left {
		result += left[i] * right[i]
	}
	return result
}
func meanAndStdDev(values []float64) (float64, float64) {
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, v := range values {
		difference := v - mean
		variance += difference * difference
	}
	return mean, math.Sqrt(variance / float64(len(values)))
}
func valuationSignal(zScore float64) string {
	if zScore > 0 {
		return "UNDERVALUED"
	}
	if zScore < 0 {
		return "OVERVALUED"
	}
	return "FAIR"
}
