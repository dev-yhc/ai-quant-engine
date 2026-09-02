package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yhc/quant-engine-go/apps/valuation-engine/internal/adapters/postgres"
	"github.com/yhc/quant-engine-go/apps/valuation-engine/internal/application"
	valuationgrpc "github.com/yhc/quant-engine-go/apps/valuation-engine/internal/grpc"
	"google.golang.org/grpc"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	repository, err := postgres.New(ctx, os.Getenv("DATABASE_CONNECTION_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()

	grpcListener, err := net.Listen("tcp", envOr("VALUATION_ENGINE_GRPC_ADDR", ":9090"))
	if err != nil {
		log.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	evaluationService := application.NewBondEvaluationService(repository)
	valuationgrpc.RegisterBondEvaluationServer(grpcServer, valuationgrpc.NewBondEvaluationServer(evaluationService, application.NewSignalEvaluationService(evaluationService, repository)))
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Printf("gRPC server stopped: %v", err)
		}
	}()

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	httpServer := &http.Server{Addr: envOr("VALUATION_ENGINE_HTTP_ADDR", ":8081"), Handler: router}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("health server stopped: %v", err)
		}
	}()

	<-ctx.Done()
	grpcServer.GracefulStop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown health server: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
