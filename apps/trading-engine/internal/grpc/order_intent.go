// Package grpc exposes the trading engine's durable order-intent boundary.
package grpc

import (
	"context"
	"fmt"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/application"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"time"
)

const submitOrderIntentMethod = "/trading.v1.TradingService/SubmitOrderIntent"

type TradingService interface {
	SubmitOrderIntent(context.Context, *structpb.Struct) (*structpb.Struct, error)
}
type Server struct{ application application.Service }

func NewServer(application application.Service) *Server { return &Server{application: application} }
func (s *Server) SubmitOrderIntent(ctx context.Context, request *structpb.Struct) (*structpb.Struct, error) {
	intent, err := intentFromMap(request.AsMap())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	order, created, err := s.application.SubmitOrderIntent(ctx, intent)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return structpb.NewStruct(map[string]any{"order_intent_id": order.ID, "status": string(order.Status), "created": created, "broker_client_order_id": order.BrokerClientOrderID})
}
func Register(server grpc.ServiceRegistrar, service TradingService) {
	server.RegisterService(&grpc.ServiceDesc{ServiceName: "trading.v1.TradingService", HandlerType: (*TradingService)(nil), Methods: []grpc.MethodDesc{{MethodName: "SubmitOrderIntent", Handler: submitHandler}}}, service)
}
func submitHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(structpb.Struct)
	if err := decode(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TradingService).SubmitOrderIntent(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: submitOrderIntentMethod}, func(ctx context.Context, request any) (any, error) {
		return srv.(TradingService).SubmitOrderIntent(ctx, request.(*structpb.Struct))
	})
}
func intentFromMap(m map[string]any) (domain.Intent, error) {
	expiresAt, err := time.Parse(time.RFC3339, stringValue(m, "expires_at"))
	if err != nil {
		return domain.Intent{}, fmt.Errorf("expires_at must be RFC3339: %w", err)
	}
	return domain.Intent{ID: stringValue(m, "id"), SignalEventID: stringValue(m, "signal_event_id"), ApprovalRequestID: stringValue(m, "approval_request_id"), Strategy: stringValue(m, "strategy"), Instrument: stringValue(m, "instrument"), Side: domain.Side(stringValue(m, "side")), OrderType: domain.OrderType(stringValue(m, "order_type")), Quantity: stringValue(m, "quantity"), OrderAmount: stringValue(m, "order_amount"), LimitPrice: stringValue(m, "limit_price"), IdempotencyKey: stringValue(m, "idempotency_key"), PolicyVersion: stringValue(m, "policy_version"), Mode: domain.ExecutionMode(stringValue(m, "mode")), ExpiresAt: expiresAt}, nil
}
func stringValue(m map[string]any, key string) string { v, _ := m[key].(string); return v }
