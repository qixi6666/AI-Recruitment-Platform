package repository

import (
	"time"

	"recruitment/logic-grpc-service/internal/domain"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func Open(cfg Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.Job{},
		&domain.CandidateProfile{},
		&domain.Resume{},
		&domain.Application{},
		&domain.ChatMessage{},
	); err != nil {
		return nil, err
	}
	return db, nil
}
