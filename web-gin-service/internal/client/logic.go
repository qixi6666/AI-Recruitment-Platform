package client

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"recruitment/shared/rpc"
)

func DialLogic(addr string) (*grpc.ClientConn, rpc.LogicServiceClient, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rpc.JSONCodec{})),
		grpc.WithUnaryInterceptor(timeoutUnaryInterceptor),
	)
	if err != nil {
		return nil, nil, err
	}
	return conn, rpc.NewLogicServiceClient(conn), nil
}

func timeoutUnaryInterceptor(ctx context.Context, method string, req any, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if _, ok := ctx.Deadline(); ok {
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	timeout := 8 * time.Second
	if method == "/recruitment.LogicService/AIChat" {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return invoker(ctx, method, req, reply, cc, opts...)
}
