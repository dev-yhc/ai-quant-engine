package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yhc/quant-engine-go/apps/alert-dispatcher/internal/adapters/postgres"
	"github.com/yhc/quant-engine-go/apps/alert-dispatcher/internal/adapters/slack"
	"github.com/yhc/quant-engine-go/apps/alert-dispatcher/internal/application"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	repository, err := postgres.New(ctx, os.Getenv("DATABASE_CONNECTION_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	sender, err := slack.NewWebhookSender(os.Getenv("SLACK_WEBHOOK_URL"), &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	dispatcher := application.NewDispatcher(repository, sender)
	log.Print("starting alert dispatcher")
	if err := dispatcher.Run(ctx, 5*time.Second); err != nil {
		log.Fatal(err)
	}
}
