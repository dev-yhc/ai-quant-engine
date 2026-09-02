// schedule-data-collector creates the recurring Temporal schedule for market
// data collection. Run it once after the worker and Temporal service are ready.
package main

import (
	"context"
	"errors"
	"log"

	"github.com/yhc/quant-engine-go/apps/data_collector/internal/application"
	"github.com/yhc/quant-engine-go/apps/data_collector/internal/config"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

func main() {
	dotenvPath, err := config.DotenvPath()
	if err != nil {
		log.Fatal(err)
	}
	settings, err := config.LoadSchedule(dotenvPath)
	if err != nil {
		log.Fatal(err)
	}

	temporalClient, err := client.Dial(client.Options{HostPort: settings.TemporalHostPort, Namespace: settings.TemporalNamespace})
	if err != nil {
		log.Fatal(err)
	}
	defer temporalClient.Close()

	schedule := client.ScheduleOptions{
		ID: settings.TemporalScheduleID,
		Spec: client.ScheduleSpec{
			CronExpressions: []string{settings.TemporalScheduleCron},
			TimeZoneName:    settings.TemporalScheduleTimeZone,
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        settings.TemporalScheduleID,
			Workflow:  application.MarketDataCollectionWorkflowName,
			TaskQueue: settings.TemporalTaskQueue,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	}
	_, err = temporalClient.ScheduleClient().Create(context.Background(), schedule)
	var alreadyExists *serviceerror.AlreadyExists
	if errors.As(err, &alreadyExists) {
		err = temporalClient.ScheduleClient().GetHandle(context.Background(), settings.TemporalScheduleID).Update(context.Background(), client.ScheduleUpdateOptions{
			DoUpdate: func(client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				return &client.ScheduleUpdate{Schedule: &client.Schedule{
					Action: schedule.Action,
					Spec:   &schedule.Spec,
					Policy: &client.SchedulePolicies{Overlap: schedule.Overlap},
				}}, nil
			},
		})
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ensured Temporal schedule %q (%s, %s)", settings.TemporalScheduleID, settings.TemporalScheduleCron, settings.TemporalScheduleTimeZone)
}
