// Package domain contains market-data business concepts.
package domain

import "time"

type Quote struct {
	Symbol     string
	Price      float64
	ObservedAt time.Time
}

// Observation is a provider-independent economic or market time-series value.
// It is shared by collection, storage, and valuation applications.
type Observation struct {
	Series     string
	ObservedAt time.Time
	Value      float64
	Provider   string
}

// ResearchDataset represents a versioned source file that needs parsing or
// persistence downstream, such as a New York Fed research workbook.
type ResearchDataset struct {
	Name        string
	Provider    string
	Content     []byte
	ContentType string
}
