package ai

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"recruitment/logic-grpc-service/internal/config"
)

type Client struct {
	cfg config.AIConfig
	db  *gorm.DB
}

type RecommendationProgressFunc func(stage string, message string) error

func NewClient(cfg config.AIConfig, db *gorm.DB) *Client {
	return &Client{cfg: cfg, db: db}
}

func (c *Client) RecommendResumesByJD(ctx context.Context, hrID uint64, input ResumeRecommendationInput) (ResumeRecommendationOutput, error) {
	return c.RecommendResumesByJDWithProgress(ctx, hrID, input, nil)
}

func (c *Client) RecommendResumesByJDWithProgress(ctx context.Context, hrID uint64, input ResumeRecommendationInput, progress RecommendationProgressFunc) (ResumeRecommendationOutput, error) {
	if !c.ragEnabled() {
		return ResumeRecommendationOutput{}, fmt.Errorf("rag resume recommendation is disabled")
	}
	return c.recommendResumesByJD(ctx, hrID, input, progress)
}
