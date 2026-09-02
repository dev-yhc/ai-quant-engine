package application

import (
	"context"
	"testing"

	"github.com/yhc/quant-engine-go/domains/valuation/domain"
)

type signalStoreSpy struct {
	result domain.US10YearResult
	calls  int
}

func (s *signalStoreSpy) RecordUS10YearSignal(_ context.Context, result domain.US10YearResult) (SignalRecord, error) {
	s.calls++
	s.result = result
	return SignalRecord{EventID: 9, EventKey: "US_TREASURY_10Y:2026-09-02:v1", ApprovalRequired: true}, nil
}

func TestSignalEvaluationServiceRecordsCalculatedResult(t *testing.T) {
	store := &signalStoreSpy{}
	evaluator := BondEvaluationService{repository: repositoryStub{input: syntheticInput(800)}, config: domain.EngineConfig{RegressionWindow: 200, NormalizationWindow: 100, MinimumSamples: 30}}
	result, record, err := NewSignalEvaluationService(evaluator, store).EvaluateAndEnqueueUS10YearSignal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.result != result {
		t.Fatalf("store calls=%d result=%#v", store.calls, store.result)
	}
	if record.EventID != 9 || !record.ApprovalRequired {
		t.Fatalf("record=%#v", record)
	}
}
