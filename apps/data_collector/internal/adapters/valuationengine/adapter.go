// Package valuationengine adapts the valuation-engine's public gRPC client to
// the collector workflow's signal-evaluation port.
package valuationengine

import (
	"context"
	"fmt"
)

type client interface {
	EvaluateAndEnqueueUS10YearSignal(context.Context) (map[string]any, error)
}

type Adapter struct{ client client }

func New(client client) Adapter { return Adapter{client: client} }

func (a Adapter) EvaluateAndEnqueueUS10YearSignal(ctx context.Context) error {
	if _, err := a.client.EvaluateAndEnqueueUS10YearSignal(ctx); err != nil {
		return fmt.Errorf("evaluate and enqueue US 10-year signal: %w", err)
	}
	return nil
}
