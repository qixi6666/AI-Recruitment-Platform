package main

import (
	"log"

	"github.com/gin-gonic/gin"

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

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS())
	handlers.New(logic, cfg.JWTSecret, cfg.JWTExpireHours, cfg.UploadDir).RegisterRoutes(r)

	log.Printf("web gin service listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("serve http: %v", err)
	}
}
