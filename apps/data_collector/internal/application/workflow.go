package application

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// MarketDataCollectionWorkflowName is stable so schedules and workers can
// reference the workflow without depending on a Go function name.
const MarketDataCollectionWorkflowName = "market-data-collection"

// CollectMarketDataWorkflow fetches FRED and NY Fed data through independently
// retryable activities. A failure in either activity fails the workflow, which
// lets Temporal retry and expose the failed run to operators.
func CollectMarketDataWorkflow(ctx workflow.Context) error {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	if err := workflow.ExecuteActivity(ctx, CollectFredValuationActivityName).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.ExecuteActivity(ctx, CollectNYFedValuationActivityName).Get(ctx, nil); err != nil {
		return err
	}
	return workflow.ExecuteActivity(ctx, EvaluateUS10YearSignalActivityName).Get(ctx, nil)
}
