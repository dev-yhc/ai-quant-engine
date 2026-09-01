// Package client is the public gRPC client for the valuation engine.
package client

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const calculateUSTreasury10YearTheoreticalYieldMethod = "/valuation.v1.BondEvaluationService/CalculateUSTreasury10YearTheoreticalYield"

type Client struct {
	connection grpc.ClientConnInterface
	timeout    time.Duration
}

func New(connection grpc.ClientConnInterface, timeout time.Duration) Client {
	return Client{connection: connection, timeout: timeout}
}

func (c Client) CalculateUSTreasury10YearTheoreticalYield(ctx context.Context) (float64, error) {
	response := new(wrapperspb.DoubleValue)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.connection.Invoke(ctx, calculateUSTreasury10YearTheoreticalYieldMethod, &emptypb.Empty{}, response); err != nil {
		return 0, err
	}
	return response.Value, nil
}
