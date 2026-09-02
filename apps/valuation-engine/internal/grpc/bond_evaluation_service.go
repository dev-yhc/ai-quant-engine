// Package grpc exposes valuation use cases to internal callers.
package grpc

import (
	"context"
	"errors"

	"github.com/yhc/quant-engine-go/apps/valuation-engine/internal/application"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

const calculateUSTreasury10YearTheoreticalYieldMethod = "/valuation.v1.BondEvaluationService/CalculateUSTreasury10YearTheoreticalYield"
const evaluateAndEnqueueUS10YearSignalMethod = "/valuation.v1.BondEvaluationService/EvaluateAndEnqueueUS10YearSignal"

type BondEvaluationService interface {
	CalculateUSTreasury10YearTheoreticalYield(context.Context, *emptypb.Empty) (*structpb.Struct, error)
	EvaluateAndEnqueueUS10YearSignal(context.Context, *emptypb.Empty) (*structpb.Struct, error)
}

type BondEvaluationServer struct {
	application application.BondEvaluationService
	signals     application.SignalEvaluationService
}

func NewBondEvaluationServer(service application.BondEvaluationService, signals application.SignalEvaluationService) *BondEvaluationServer {
	return &BondEvaluationServer{application: service, signals: signals}
}

func (s *BondEvaluationServer) EvaluateAndEnqueueUS10YearSignal(ctx context.Context, _ *emptypb.Empty) (*structpb.Struct, error) {
	result, record, err := s.signals.EvaluateAndEnqueueUS10YearSignal(ctx)
	if err != nil {
		var inputError application.InputError
		if errors.As(err, &inputError) {
			return nil, status.Error(codes.FailedPrecondition, inputError.Error())
		}
		return nil, status.Error(codes.Internal, "signal evaluation failed")
	}
	return structpb.NewStruct(map[string]any{
		"event_id": record.EventID, "event_key": record.EventKey, "approval_required": record.ApprovalRequired,
		"date": result.Date.Format("2006-01-02"), "signal": result.Signal, "z_score": result.ZScore,
	})
}

func (s *BondEvaluationServer) CalculateUSTreasury10YearTheoreticalYield(ctx context.Context, _ *emptypb.Empty) (*structpb.Struct, error) {
	result, err := s.application.CalculateUSTreasury10YearTheoreticalYield(ctx)
	if err != nil {
		var inputError application.InputError
		if errors.As(err, &inputError) {
			return nil, status.Error(codes.FailedPrecondition, inputError.Error())
		}
		return nil, status.Error(codes.Internal, "bond evaluation failed")
	}
	return structpb.NewStruct(map[string]any{
		"date": result.Date.Format("2006-01-02"), "actual": result.Actual, "anchor": result.Anchor,
		"macro_anchor": result.MacroAnchor, "statistical_anchor": result.StatisticalAnchor,
		"regression_anchor": result.RegressionAnchor, "raw_distance": result.RawDistance,
		"bias": result.Bias, "delta": result.Delta, "distance_std_dev": result.DistanceStdDev,
		"z_score": result.ZScore, "signal": result.Signal,
	})
}

func RegisterBondEvaluationServer(server grpc.ServiceRegistrar, service BondEvaluationService) {
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "valuation.v1.BondEvaluationService",
		HandlerType: (*BondEvaluationService)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "CalculateUSTreasury10YearTheoreticalYield",
			Handler:    calculateUSTreasury10YearTheoreticalYieldHandler,
		}, {
			MethodName: "EvaluateAndEnqueueUS10YearSignal",
			Handler:    evaluateAndEnqueueUS10YearSignalHandler,
		}},
	}, service)
}

func evaluateAndEnqueueUS10YearSignalHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(emptypb.Empty)
	if err := decode(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BondEvaluationService).EvaluateAndEnqueueUS10YearSignal(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: evaluateAndEnqueueUS10YearSignalMethod}, func(ctx context.Context, request any) (any, error) {
		return srv.(BondEvaluationService).EvaluateAndEnqueueUS10YearSignal(ctx, request.(*emptypb.Empty))
	})
}

func calculateUSTreasury10YearTheoreticalYieldHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(emptypb.Empty)
	if err := decode(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BondEvaluationService).CalculateUSTreasury10YearTheoreticalYield(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: calculateUSTreasury10YearTheoreticalYieldMethod}, func(ctx context.Context, request any) (any, error) {
		return srv.(BondEvaluationService).CalculateUSTreasury10YearTheoreticalYield(ctx, request.(*emptypb.Empty))
	})
}
