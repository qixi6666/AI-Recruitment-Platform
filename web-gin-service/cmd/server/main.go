package main

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"recruitment/web-gin-service/internal/client"
	"recruitment/web-gin-service/internal/config"
	"recruitment/web-gin-service/internal/handlers"
	"recruitment/web-gin-service/internal/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	conn, logic, err := client.DialLogic(cfg.LogicGRPCAddr)
	if err != nil {
		log.Fatalf("dial logic service: %v", err)
	}
	defer conn.Close()
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		cancel()
		log.Fatalf("connect redis %s: %v", cfg.Redis.Address, err)
	}
	cancel()
	defer redisClient.Close()

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS())
	handlers.New(logic, redisClient, cfg.JWTSecret, cfg.JWTExpireHours, cfg.UploadDir).RegisterRoutes(r)

	log.Printf("web gin service listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("serve http: %v", err)
	}
}
