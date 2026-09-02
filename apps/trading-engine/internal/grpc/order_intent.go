// Package grpc exposes the trading engine's durable order-intent boundary.
package grpc

import (
	"context"
	"fmt"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/application"
	strategyapp "github.com/yhc/quant-engine-go/apps/trading-engine/internal/application/strategy"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	strategydomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain/strategy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"time"
)

const submitOrderIntentMethod = "/trading.v1.TradingService/SubmitOrderIntent"
const handleSignalMethod = "/trading.v1.TradingService/HandleSignal"

type TradingService interface {
	SubmitOrderIntent(context.Context, *structpb.Struct) (*structpb.Struct, error)
	HandleSignal(context.Context, *structpb.Struct) (*structpb.Struct, error)
}
type Server struct {
	application         application.Service
	strategyApplication strategyapp.Service
}

func NewServer(application application.Service, strategyApplication strategyapp.Service) *Server {
	return &Server{application: application, strategyApplication: strategyApplication}
}
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
func (s *Server) HandleSignal(ctx context.Context, request *structpb.Struct) (*structpb.Struct, error) {
	event, err := signalFromMap(request.AsMap())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.strategyApplication.HandleSignal(ctx, event)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	d := result.Decision
	return structpb.NewStruct(map[string]any{
		"decision_id": d.ID, "created": result.Created, "target_krw": d.TargetKRW,
		"target_weight": d.TargetWeight, "effective_exposure_krw": d.EffectiveExposureKRW,
		"delta_krw": d.DeltaKRW, "order_amount_krw": d.OrderAmountKRW,
		"order_intent_id": d.OrderID, "reason": d.Reason,
	})
}
func Register(server grpc.ServiceRegistrar, service TradingService) {
	server.RegisterService(&grpc.ServiceDesc{ServiceName: "trading.v1.TradingService", HandlerType: (*TradingService)(nil), Methods: []grpc.MethodDesc{{MethodName: "SubmitOrderIntent", Handler: submitHandler}, {MethodName: "HandleSignal", Handler: signalHandler}}}, service)
}
func signalHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(structpb.Struct)
	if err := decode(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TradingService).HandleSignal(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: handleSignalMethod}, func(ctx context.Context, request any) (any, error) {
		return srv.(TradingService).HandleSignal(ctx, request.(*structpb.Struct))
	})
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
func signalFromMap(m map[string]any) (strategydomain.SignalEvent, error) {
	evaluatedAt, err := time.Parse(time.RFC3339, stringValue(m, "evaluated_at"))
	if err != nil {
		return strategydomain.SignalEvent{}, fmt.Errorf("evaluated_at must be RFC3339: %w", err)
	}
	zScore, ok := m["z_score"].(float64)
	if !ok {
		return strategydomain.SignalEvent{}, fmt.Errorf("z_score must be a number")
	}
	event := strategydomain.SignalEvent{ID: stringValue(m, "signal_event_id"), StrategyID: stringValue(m, "strategy_id"), ZScore: zScore, Signal: strategydomain.Signal(stringValue(m, "signal")), ModelVersion: stringValue(m, "model_version"), AsOf: stringValue(m, "as_of"), EvaluatedAt: evaluatedAt}
	if err := event.Validate(); err != nil {
		return strategydomain.SignalEvent{}, err
	}
	return event, nil
}
func stringValue(m map[string]any, key string) string { v, _ := m[key].(string); return v }
