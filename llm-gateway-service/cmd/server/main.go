package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"recruitment/llm-gateway-service/internal/config"
	"recruitment/llm-gateway-service/internal/gateway"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	rt := gateway.NewRuntime(cfg)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	gateway.NewHandler(rt, cfg.APIToken).RegisterRoutes(router)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("llm gateway service listening on %s", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve http: %v", err)
		}
	case sig := <-sigCh:
		log.Printf("received %s, shutting down llm gateway service", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("llm gateway graceful shutdown failed: %v", err)
		}
	}
}
