// collect-data-once performs a synchronous collection for initial loads,
// backfills, and connection verification without waiting for a Temporal schedule.
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/fred"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/nyfed"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/postgres"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/application"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/config"
)

func main() {
	dotenvPath, err := config.DotenvPath()
	if err != nil {
		log.Fatal(err)
	}
	settings, err := config.Load(dotenvPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	repository, err := postgres.New(ctx, settings.DatabaseConnectionURL)
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	httpClient := &http.Client{Timeout: 90 * time.Second}
	fredAdapter, err := fred.New(settings.FredAPIKey, httpClient)
	if err != nil {
		log.Fatal(err)
	}
	fredResult, err := application.CollectFredValuationData(ctx, fredAdapter, repository)
	if err != nil {
		log.Fatal(err)
	}
	nyFedResult, err := application.CollectNYFedValuationData(ctx, nyfed.New(httpClient), repository)
	if err != nil {
		log.Fatal(err)
	}
	counts, err := repository.Counts(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("FRED collected: %d observations through %s", fredResult.ObservationCount, fredResult.LatestObservation.Format("2006-01-02"))
	log.Printf("NY Fed collected: %d datasets", len(nyFedResult.Datasets))
	log.Printf("PostgreSQL rows: %d observations, %d research datasets", counts.Observations, counts.Datasets)
}
