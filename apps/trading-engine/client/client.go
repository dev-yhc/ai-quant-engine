// Package client is used by the execution dispatcher to submit order intents.
package client

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
	"time"
)

const submitOrderIntentMethod = "/trading.v1.TradingService/SubmitOrderIntent"
const handleSignalMethod = "/trading.v1.TradingService/HandleSignal"

type Client struct {
	connection grpc.ClientConnInterface
	timeout    time.Duration
}

func New(connection grpc.ClientConnInterface, timeout time.Duration) Client {
	return Client{connection: connection, timeout: timeout}
}

func (c Client) SubmitOrderIntent(ctx context.Context, intent map[string]any) (map[string]any, error) {
	request, err := structpb.NewStruct(intent)
	if err != nil {
		return nil, err
	}
	response := new(structpb.Struct)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.connection.Invoke(ctx, submitOrderIntentMethod, request, response); err != nil {
		return nil, err
	}
	return response.AsMap(), nil
}

func (c Client) HandleSignal(ctx context.Context, signal map[string]any) (map[string]any, error) {
	request, err := structpb.NewStruct(signal)
	if err != nil {
		return nil, err
	}
	response := new(structpb.Struct)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.connection.Invoke(ctx, handleSignalMethod, request, response); err != nil {
		return nil, err
	}
	return response.AsMap(), nil
}
