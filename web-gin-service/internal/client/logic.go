package client

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"recruitment/shared/rpc"
)

type LogicClients struct {
	Auth            rpc.AuthServiceClient
	Jobs            rpc.JobServiceClient
	Candidates      rpc.CandidateServiceClient
	Applications    rpc.ApplicationServiceClient
	Recommendations rpc.ResumeRecommendationServiceClient
	LLM             rpc.LLMGatewayServiceClient
}

func DialLogic(addr string) (*grpc.ClientConn, LogicClients, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rpc.JSONCodec{})),
		grpc.WithUnaryInterceptor(timeoutUnaryInterceptor),
	)
	if err != nil {
		return nil, LogicClients{}, err
	}
	return conn, LogicClients{
		Auth:            rpc.NewAuthServiceClient(conn),
		Jobs:            rpc.NewJobServiceClient(conn),
		Candidates:      rpc.NewCandidateServiceClient(conn),
		Applications:    rpc.NewApplicationServiceClient(conn),
		Recommendations: rpc.NewResumeRecommendationServiceClient(conn),
		LLM:             rpc.NewLLMGatewayServiceClient(conn),
	}, nil
}

func timeoutUnaryInterceptor(ctx context.Context, method string, req any, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if _, ok := ctx.Deadline(); ok {
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	timeout := 8 * time.Second
	if method == "/recruitment.ResumeRecommendationService/RecommendResumes" {
		timeout = 60 * time.Second
	}
	if method == "/recruitment.LLMGatewayService/Chat" || method == "/recruitment.LLMGatewayService/Evaluate" {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return invoker(ctx, method, req, reply, cc, opts...)
}
