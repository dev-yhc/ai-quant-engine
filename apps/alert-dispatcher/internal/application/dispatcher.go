// Package application routes durable valuation signal events to independent
// informational and approval-request alert deliveries.
package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	AlertKindInformation = "INFORMATION"
	AlertKindApproval    = "APPROVAL_REQUEST"
)

type SignalEvent struct {
	ID               int64
	Instrument       string
	EvaluatedOn      string
	AsOf             string
	ModelVersion     string
	Actual           float64
	Anchor           float64
	RawDistance      float64
	Delta            float64
	ZScore           float64
	Signal           string
	ApprovalRequired bool
}

type SignalOutboxItem struct {
	ID    int64
	Event SignalEvent
}

type Alert struct {
	ID                int64
	Kind              string
	Event             SignalEvent
	ApprovalRequestID int64
	ExpiresAt         time.Time
}

type Repository interface {
	ClaimSignal(context.Context) (SignalOutboxItem, bool, error)
	RouteSignal(context.Context, SignalOutboxItem) error
	MarkSignalDelivered(context.Context, int64) error
	MarkSignalRetry(context.Context, int64, error) error
	ClaimAlert(context.Context) (Alert, bool, error)
	MarkAlertDelivered(context.Context, int64) error
	MarkAlertRetry(context.Context, int64, error) error
}

type Sender interface {
	Send(context.Context, Alert) error
}

type Dispatcher struct {
	repository Repository
	sender     Sender
}

func NewDispatcher(repository Repository, sender Sender) Dispatcher {
	return Dispatcher{repository: repository, sender: sender}
}

// RunOnce drains the available signal-routing and alert-delivery work. Failed
// items stay in their outboxes for a later retry; they do not abort the loop.
func (d Dispatcher) RunOnce(ctx context.Context) error {
	for {
		item, found, err := d.repository.ClaimSignal(ctx)
		if err != nil {
			return fmt.Errorf("claim signal outbox: %w", err)
		}
		if !found {
			break
		}
		if err := d.repository.RouteSignal(ctx, item); err != nil {
			if retryErr := d.repository.MarkSignalRetry(ctx, item.ID, err); retryErr != nil {
				return fmt.Errorf("route signal: %w; mark retry: %v", err, retryErr)
			}
			continue
		}
		if err := d.repository.MarkSignalDelivered(ctx, item.ID); err != nil {
			return fmt.Errorf("mark signal delivered: %w", err)
		}
	}
	for {
		alert, found, err := d.repository.ClaimAlert(ctx)
		if err != nil {
			return fmt.Errorf("claim alert outbox: %w", err)
		}
		if !found {
			return nil
		}
		if err := d.sender.Send(ctx, alert); err != nil {
			if retryErr := d.repository.MarkAlertRetry(ctx, alert.ID, err); retryErr != nil {
				return fmt.Errorf("send alert: %w; mark retry: %v", err, retryErr)
			}
			continue
		}
		if err := d.repository.MarkAlertDelivered(ctx, alert.ID); err != nil {
			return fmt.Errorf("mark alert delivered: %w", err)
		}
	}
}

func (d Dispatcher) Run(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		return fmt.Errorf("poll interval must be positive")
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if err := d.RunOnce(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// SlackText produces a human-readable message. Approval messages identify a
// durable request; they never claim that an order was created or approved.
func SlackText(alert Alert) string {
	lines := []string{
		fmt.Sprintf("*US Treasury 10Y valuation* — %s", alert.Kind),
		fmt.Sprintf("Signal: *%s* | Z-score: %.2f", alert.Event.Signal, alert.Event.ZScore),
		fmt.Sprintf("Actual: %.3f%% | Anchor: %.3f%% | Distance: %.3f%%", alert.Event.Actual, alert.Event.Anchor, alert.Event.RawDistance),
		fmt.Sprintf("As of: %s | Evaluated: %s | Model: %s", alert.Event.AsOf, alert.Event.EvaluatedOn, alert.Event.ModelVersion),
		fmt.Sprintf("Signal event: %d", alert.Event.ID),
	}
	if alert.Kind == AlertKindApproval {
		lines = append(lines,
			fmt.Sprintf("*Trade approval required* — request #%d", alert.ApprovalRequestID),
			fmt.Sprintf("Expires: %s", alert.ExpiresAt.UTC().Format(time.RFC3339)),
			"This message is a review request only. No order has been submitted.",
		)
	} else {
		lines = append(lines, "Informational only. No order action is requested.")
	}
	return strings.Join(lines, "\n")
}
