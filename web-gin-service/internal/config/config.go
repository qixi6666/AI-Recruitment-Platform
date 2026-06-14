package config

import (
	"errors"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr       string
	LogicGRPCAddr  string
	Redis          RedisConfig
	JWTSecret      string
	JWTExpireHours int64
	UploadDir      string
}

type RedisConfig struct {
	Address  string
	Password string
	DB       int
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}
	cfg := Config{HTTPAddr: ":8080", LogicGRPCAddr: "127.0.0.1:9002", Redis: RedisConfig{Address: "127.0.0.1:6379"}, JWTExpireHours: 72, UploadDir: "uploads"}
	overrideString(&cfg.HTTPAddr, "WEB_HTTP_ADDR")
	overrideString(&cfg.LogicGRPCAddr, "LOGIC_GRPC_ADDR")
	overrideString(&cfg.Redis.Address, "REDIS_ADDR")
	overrideString(&cfg.Redis.Password, "REDIS_PASSWORD")
	if value := os.Getenv("REDIS_DB"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Redis.DB = parsed
		}
	}
	overrideString(&cfg.JWTSecret, "JWT_SECRET")
	overrideString(&cfg.UploadDir, "UPLOAD_DIR")
	if value := os.Getenv("JWT_EXPIRE_HOURS"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			cfg.JWTExpireHours = parsed
		}
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("jwt secret is required")
	}
	return cfg, nil
}

func loadDotEnv() error {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func overrideString(target *string, key string) {
	if value := os.Getenv(key); value != "" {
		*target = value
	}
}
