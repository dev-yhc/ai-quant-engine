package application

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestCollectMarketDataWorkflowEvaluatesSignalAfterBothCollections(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	var calls []string
	environment.RegisterActivityWithOptions(func(context.Context) (FredCollectionResult, error) {
		calls = append(calls, CollectFredValuationActivityName)
		return FredCollectionResult{}, nil
	}, activity.RegisterOptions{Name: CollectFredValuationActivityName})
	environment.RegisterActivityWithOptions(func(context.Context) (NYFedCollectionResult, error) {
		calls = append(calls, CollectNYFedValuationActivityName)
		return NYFedCollectionResult{}, nil
	}, activity.RegisterOptions{Name: CollectNYFedValuationActivityName})
	environment.RegisterActivityWithOptions(func(context.Context) error {
		calls = append(calls, EvaluateUS10YearSignalActivityName)
		return nil
	}, activity.RegisterOptions{Name: EvaluateUS10YearSignalActivityName})

	environment.ExecuteWorkflow(CollectMarketDataWorkflow)
	if err := environment.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	want := []string{CollectFredValuationActivityName, CollectNYFedValuationActivityName, EvaluateUS10YearSignalActivityName}
	if len(calls) != len(want) {
		t.Fatalf("activities = %#v, want %#v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("activities = %#v, want %#v", calls, want)
		}
	}
}
