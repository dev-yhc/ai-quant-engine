package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/fred"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/nyfed"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/application"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/config"
)

func main() {
	dotenvPath, err := dotenvPath()
	if err != nil {
		log.Fatal(err)
	}
	settings, err := config.Load(dotenvPath)
	if err != nil {
		log.Fatal(err)
	}

	client := &http.Client{}
	fredAdapter, err := fred.New(settings.FredAPIKey, client)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	fredResult, err := application.CollectFredValuationData(ctx, fredAdapter)
	if err != nil {
		log.Fatal(err)
	}
	nyFedResult, err := application.CollectNYFedValuationData(ctx, nyfed.New(client))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("FRED collected: %d observations through %s", fredResult.ObservationCount, fredResult.LatestObservation.Format("2006-01-02"))
	log.Printf("NY Fed collected: %d datasets", len(nyFedResult.Datasets))
}

func dotenvPath() (string, error) {
	if configuredPath := os.Getenv("DATA_COLLECTOR_ENV_FILE"); configuredPath != "" {
		return configuredPath, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{
		filepath.Join(workingDirectory, ".env"),
		filepath.Join(workingDirectory, "apps", "data_collector", ".env"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return filepath.Join(workingDirectory, ".env"), nil
}
