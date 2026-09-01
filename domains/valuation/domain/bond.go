// Package domain contains framework-independent valuation rules.
package domain

import "time"

type Observation struct {
	Date  time.Time
	Value float64
}

type US10YearInput struct {
	ActualYield        []Observation
	BreakevenInflation []Observation
	RealYield          []Observation
	TermPremium        []Observation
	NaturalRate        []Observation
	Treasury2Year      []Observation
	Treasury3Month     []Observation
	CPI                []Observation
	GDPGrowth          []Observation
}

type US10YearResult struct {
	Date              time.Time
	Actual            float64
	Anchor            float64
	MacroAnchor       float64
	StatisticalAnchor float64
	RegressionAnchor  float64
	RawDistance       float64
	Bias              float64
	Delta             float64
	DistanceStdDev    float64
	ZScore            float64
	Signal            string
}
