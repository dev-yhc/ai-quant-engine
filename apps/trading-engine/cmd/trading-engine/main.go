package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/adapters/postgres"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/adapters/toss"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/application"
	strategyapp "github.com/yhc/quant-engine-go/apps/trading-engine/internal/application/strategy"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	strategydomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain/strategy"
	tradinggrpc "github.com/yhc/quant-engine-go/apps/trading-engine/internal/grpc"
	"google.golang.org/grpc"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	repository, err := postgres.New(ctx, envOr("TRADING_DATABASE_CONNECTION_URL", os.Getenv("DATABASE_CONNECTION_URL")))
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	broker, err := toss.New(toss.Config{ClientID: os.Getenv("TOSS_CLIENT_ID"), ClientSecret: os.Getenv("TOSS_CLIENT_SECRET"), AccountSeq: int64Value("TOSS_ACCOUNT_SEQ"), BaseURL: os.Getenv("TOSS_BASE_URL")})
	if err != nil {
		log.Fatal(err)
	}
	policy := riskPolicy()
	service := application.New(repository, broker, policy)
	strategyService := strategyapp.New(repository, broker, policy, strategydomain.IEFOvervaluedV1())
	realtime := toss.NewRealtime(broker)
	go func() {
		if err := realtime.Run(ctx, allowedInstruments(policy)); err != nil && ctx.Err() == nil {
			log.Printf("Toss realtime feed stopped: %v", err)
		}
	}()
	listener, err := net.Listen("tcp", envOr("TRADING_ENGINE_GRPC_ADDR", ":9091"))
	if err != nil {
		log.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	tradinggrpc.Register(grpcServer, tradinggrpc.NewServer(service, strategyService))
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			log.Printf("gRPC server stopped: %v", err)
		}
	}()
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/v1/market/:market/:symbol", func(c *gin.Context) {
		instrument := strings.ToUpper(c.Param("market")) + ":" + strings.ToUpper(c.Param("symbol"))
		snapshot, ok := realtime.Snapshot(instrument)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "market snapshot unavailable", "lastRealtimeError": realtime.LastError()})
			return
		}
		bestBid, hasBid := snapshot.BestBid()
		bestAsk, hasAsk := snapshot.BestAsk()
		response := gin.H{"snapshot": snapshot}
		if hasBid {
			response["buyLimitPrice"] = bestBid.Price
			response["bestBid"] = bestBid
		}
		if hasAsk {
			response["sellLimitPrice"] = bestAsk.Price
			response["bestAsk"] = bestAsk
		}
		c.JSON(http.StatusOK, response)
	})
	router.GET("/v1/broker-orders/:orderID", func(c *gin.Context) {
		event, ok := realtime.Order(c.Param("orderID"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "broker order event unavailable", "lastRealtimeError": realtime.LastError()})
			return
		}
		c.JSON(http.StatusOK, event)
	})
	router.GET("/v1/trading-book", func(c *gin.Context) {
		book, err := service.TradingBook(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "trading book unavailable"})
			return
		}
		c.JSON(http.StatusOK, book)
	})
	router.POST("/v1/trading-book/alerts", func(c *gin.Context) {
		book, err := service.AlertCurrentPortfolio(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "portfolio alert unavailable"})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "queued", "portfolio": book})
	})
	httpServer := &http.Server{Addr: envOr("TRADING_ENGINE_HTTP_ADDR", ":8082"), Handler: router}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("health server stopped: %v", err)
		}
	}()
	go worker(ctx, service)
	<-ctx.Done()
	grpcServer.GracefulStop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown health server: %v", err)
	}
}

func worker(ctx context.Context, service application.Service) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				processed, err := service.ProcessOne(ctx)
				if err != nil {
					log.Printf("process order: %v", err)
					break
				}
				if !processed {
					break
				}
			}
		}
	}
}

func riskPolicy() domain.RiskPolicy {
	return domain.RiskPolicy{ExecutionEnabled: boolValue("TRADING_EXECUTION_ENABLED"), AutoExecutionEnabled: boolValue("TRADING_AUTO_EXECUTION_ENABLED"), KillSwitch: boolValue("TRADING_KILL_SWITCH"), AllowedStrategies: allowed(os.Getenv("TRADING_ALLOWED_STRATEGIES")), AllowedInstruments: allowed(os.Getenv("TRADING_ALLOWED_INSTRUMENTS")), MaxQuantity: os.Getenv("TRADING_MAX_QUANTITY"), MaxOrderAmount: os.Getenv("TRADING_MAX_ORDER_AMOUNT")}
}

func allowed(value string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			result[v] = struct{}{}
		}
	}
	return result
}

func allowedInstruments(policy domain.RiskPolicy) []string {
	result := make([]string, 0, len(policy.AllowedInstruments))
	for instrument := range policy.AllowedInstruments {
		result = append(result, instrument)
	}
	return result
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func boolValue(key string) bool { return strings.EqualFold(os.Getenv(key), "true") }

func int64Value(key string) int64 {
	var result int64
	_, _ = fmt.Sscan(os.Getenv(key), &result)
	return result
}
