// Package strategy contains pure signal-to-target-position rules.
package strategy

import (
	"fmt"
	"time"
)

type Signal string

const Overvalued Signal = "OVERVALUED"

// SignalEvent is the portion of a valuation event a trading strategy needs.
// It deliberately contains no database, broker, or transport concern.
type SignalEvent struct {
	ID           string
	StrategyID   string
	ZScore       float64
	Signal       Signal
	ModelVersion string
	AsOf         string
	EvaluatedAt  time.Time
}

func (e SignalEvent) Validate() error {
	if e.ID == "" || e.StrategyID == "" || e.ModelVersion == "" || e.AsOf == "" || e.EvaluatedAt.IsZero() {
		return fmt.Errorf("signal_event_id, strategy_id, model_version, as_of, and evaluated_at are required")
	}
	return nil
}
