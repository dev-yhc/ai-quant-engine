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
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const calculateUSTreasury10YearTheoreticalYieldMethod = "/valuation.v1.BondEvaluationService/CalculateUSTreasury10YearTheoreticalYield"

type BondEvaluationService interface {
	CalculateUSTreasury10YearTheoreticalYield(context.Context, *emptypb.Empty) (*wrapperspb.DoubleValue, error)
}

type BondEvaluationServer struct {
	application application.BondEvaluationService
}

func NewBondEvaluationServer(service application.BondEvaluationService) *BondEvaluationServer {
	return &BondEvaluationServer{application: service}
}

func (s *BondEvaluationServer) CalculateUSTreasury10YearTheoreticalYield(ctx context.Context, _ *emptypb.Empty) (*wrapperspb.DoubleValue, error) {
	yield, err := s.application.CalculateUSTreasury10YearTheoreticalYield(ctx)
	if errors.Is(err, application.ErrNotImplemented) {
		return nil, status.Error(codes.Unimplemented, err.Error())
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "bond evaluation failed")
	}
	return wrapperspb.Double(yield), nil
}

func RegisterBondEvaluationServer(server grpc.ServiceRegistrar, service BondEvaluationService) {
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "valuation.v1.BondEvaluationService",
		HandlerType: (*BondEvaluationService)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "CalculateUSTreasury10YearTheoreticalYield",
			Handler:    calculateUSTreasury10YearTheoreticalYieldHandler,
		}},
	}, service)
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
