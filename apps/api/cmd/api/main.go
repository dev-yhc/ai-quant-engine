package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	valuationclient "github.com/yhc/quant-engine-go/apps/valuation-engine/client"
	tradinghttp "github.com/yhc/quant-engine-go/domains/trading/adapters/http"
	"github.com/yhc/quant-engine-go/domains/trading/application"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func main() {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	valuationConnection, err := grpc.NewClient(valuationEngineAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer valuationConnection.Close()
	valuationClient := valuationclient.New(valuationConnection, 5*time.Second)
	router.GET("/v1/bond-valuations/us-treasury/10-year/theoretical-yield", func(c *gin.Context) {
		yield, err := valuationClient.CalculateUSTreasury10YearTheoreticalYield(c.Request.Context())
		if err != nil {
			if grpcStatus, ok := status.FromError(err); ok && grpcStatus.Code() == codes.Unimplemented {
				c.JSON(http.StatusNotImplemented, gin.H{"error": grpcStatus.Message()})
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{"error": "valuation engine unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"theoretical_yield": yield})
	})

	tradinghttp.RegisterRoutes(router.Group("/v1"), application.NewService())

	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}

	if err := router.Run(address); err != nil {
		log.Fatal(err)
	}
}

func valuationEngineAddress() string {
	if address := os.Getenv("VALUATION_ENGINE_GRPC_ADDR"); address != "" {
		return address
	}
	return "localhost:9090"
}
