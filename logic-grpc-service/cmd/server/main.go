package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	healthgrpc "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"recruitment/logic-grpc-service/internal/ai"
	"recruitment/logic-grpc-service/internal/config"
	"recruitment/logic-grpc-service/internal/repository"
	"recruitment/logic-grpc-service/internal/service"
	"recruitment/shared/rpc"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := repository.Open(repository.Config{
		DSN:             cfg.MySQL.DSN,
		MaxOpenConns:    cfg.MySQL.MaxOpenConns,
		MaxIdleConns:    cfg.MySQL.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.MySQL.ConnMaxLifetimeSeconds) * time.Second,
	})
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("mysql handle: %v", err)
	}
	defer sqlDB.Close()

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", cfg.GRPCAddr, err)
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(loggingUnaryInterceptor))
	rpc.RegisterLogicServiceServer(grpcServer, service.NewServer(db, ai.NewClient(cfg.AI, db)))
	healthServer := healthgrpc.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("logic grpc service listening on %s", cfg.GRPCAddr)
		errCh <- grpcServer.Serve(lis)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			log.Fatalf("serve grpc: %v", err)
		}
	case sig := <-sigCh:
		log.Printf("received %s, shutting down logic grpc service", sig)
		healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		gracefulStop(grpcServer, 15*time.Second)
	}
}

func loggingUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	log.Printf("grpc method=%s duration=%s code=%s", info.FullMethod, time.Since(start), status.Code(err))
	return resp, err
}

func gracefulStop(server *grpc.Server, timeout time.Duration) {
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(timeout):
		log.Printf("grpc graceful stop timed out; forcing stop")
		server.Stop()
	}
}
