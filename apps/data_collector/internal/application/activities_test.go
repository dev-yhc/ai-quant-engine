package application

import (
	"context"
	"testing"
	"time"

	"github.com/yhc/quant-engine-go/domains/marketdata/domain"
)

type researchProviderStub struct{}

func (researchProviderStub) Dataset(_ context.Context, name string) (domain.ResearchDataset, error) {
	return domain.ResearchDataset{Name: name + ":hash", Provider: "ny_fed", Content: []byte("workbook")}, nil
}

func (researchProviderStub) Observations(dataset domain.ResearchDataset) ([]domain.Observation, error) {
	series := "ACM_TERM_PREMIUM"
	if dataset.Name[:10] == "hlw_r_star" {
		series = "HLW_R_STAR"
	}
	return []domain.Observation{{Series: series, Provider: "ny_fed", ObservedAt: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), Value: 0.7}}, nil
}

type marketDataRepositorySpy struct {
	datasets     []domain.ResearchDataset
	observations []domain.Observation
}

func (r *marketDataRepositorySpy) SaveObservations(_ context.Context, observations []domain.Observation) error {
	r.observations = append(r.observations, observations...)
	return nil
}

func (r *marketDataRepositorySpy) SaveResearchDataset(_ context.Context, dataset domain.ResearchDataset) error {
	r.datasets = append(r.datasets, dataset)
	return nil
}

func TestCollectNYFedValuationDataPersistsNormalizedObservations(t *testing.T) {
	repository := &marketDataRepositorySpy{}
	result, err := CollectNYFedValuationData(context.Background(), researchProviderStub{}, repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.datasets) != 2 || len(repository.observations) != 2 || len(result.Datasets) != 2 {
		t.Fatalf("unexpected persistence: datasets=%d observations=%d result=%#v", len(repository.datasets), len(repository.observations), result)
	}
	if result.Datasets[0].Series != "ACM_TERM_PREMIUM" || result.Datasets[0].ObservationCount != 1 || result.Datasets[0].LatestObservation.IsZero() {
		t.Fatalf("unexpected ACM result: %#v", result.Datasets[0])
	}
}
