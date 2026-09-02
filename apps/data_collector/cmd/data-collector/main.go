package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/fred"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/nyfed"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/postgres"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/adapters/valuationengine"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/application"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/config"
	valuationclient "github.com/yhc/quant-engine-go/apps/valuation-engine/client"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	temporalClient, err := client.Dial(client.Options{
		HostPort:  settings.TemporalHostPort,
		Namespace: settings.TemporalNamespace,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer temporalClient.Close()
	valuationConnection, err := grpc.NewClient(settings.ValuationEngineGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer valuationConnection.Close()

	activities := application.Activities{
		FredProvider:    fredAdapter,
		NYFedProvider:   nyfed.New(httpClient),
		Repository:      repository,
		SignalEvaluator: valuationengine.New(valuationclient.New(valuationConnection, 30*time.Second)),
	}
	temporalWorker := worker.New(temporalClient, settings.TemporalTaskQueue, worker.Options{})
	temporalWorker.RegisterWorkflowWithOptions(application.CollectMarketDataWorkflow, workflow.RegisterOptions{Name: application.MarketDataCollectionWorkflowName})
	temporalWorker.RegisterActivityWithOptions(activities.CollectFredValuationData, activity.RegisterOptions{Name: application.CollectFredValuationActivityName})
	temporalWorker.RegisterActivityWithOptions(activities.CollectNYFedValuationData, activity.RegisterOptions{Name: application.CollectNYFedValuationActivityName})
	temporalWorker.RegisterActivityWithOptions(activities.EvaluateUS10YearSignal, activity.RegisterOptions{Name: application.EvaluateUS10YearSignalActivityName})

	log.Printf("starting Temporal worker: namespace=%s task_queue=%s", settings.TemporalNamespace, settings.TemporalTaskQueue)
	if err := temporalWorker.Run(worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
