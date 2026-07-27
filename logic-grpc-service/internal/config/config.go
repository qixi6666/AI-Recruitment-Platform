package config

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	GRPCAddr            string
	MySQL               MySQLConfig
	Redis               RedisConfig
	RecommendationQueue RecommendationQueueConfig
	AI                  AIConfig
}

type MySQLConfig struct {
	DSN                    string
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetimeSeconds int64
}

type RedisConfig struct {
	Address  string
	Password string
	DB       int
}

type RecommendationQueueConfig struct {
	Enabled            bool
	StreamName         string
	ConsumerGroup      string
	ConsumerName       string
	WorkerCount        int
	TaskTTLSeconds     int64
	TaskTimeoutSeconds int64
}

type AIConfig struct {
	Provider       string
	APIKey         string
	BaseURL        string
	Model          string
	TimeoutSeconds int64
	Gateway        LLMGatewayConfig
	RAG            RAGConfig
}

type LLMGatewayConfig struct {
	Address                   string
	Token                     string
	Models                    []LLMModelConfig
	SimpleJDQueryModel        string
	HRSearchPlanModel         string
	RecommendationResultModel string
}

type LLMModelConfig struct {
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	APIKey         string `json:"api_key"`
	APIKeyEnv      string `json:"api_key_env"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	TimeoutSeconds int64  `json:"timeout_seconds"`
}

type RAGConfig struct {
	Enabled                 bool
	EmbeddingAPIKey         string
	EmbeddingEndpoint       string
	EmbeddingModel          string
	EmbeddingDimension      int
	EmbeddingTimeoutSeconds int64
	MilvusAddress           string
	MilvusUsername          string
	MilvusPassword          string
	MilvusDBName            string
	MilvusCollection        string
	MilvusVectorField       string
	MilvusSparseVectorField string
	MilvusTextField         string
	MilvusMetricType        string
	MilvusOutputFields      []string
	TopK                    int
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}
	cfg := Config{
		GRPCAddr: ":9002",
		MySQL: MySQLConfig{
			MaxOpenConns:           25,
			MaxIdleConns:           10,
			ConnMaxLifetimeSeconds: int64((30 * time.Minute).Seconds()),
		},
		Redis: RedisConfig{
			Address: "127.0.0.1:6379",
		},
		RecommendationQueue: RecommendationQueueConfig{
			StreamName:         "resume_recommendation:queue",
			ConsumerGroup:      "resume_recommendation:workers",
			ConsumerName:       "logic-1",
			WorkerCount:        5,
			TaskTTLSeconds:     int64((24 * time.Hour).Seconds()),
			TaskTimeoutSeconds: int64((5 * time.Minute).Seconds()),
		},
		AI: AIConfig{
			Provider:       "deepseek",
			BaseURL:        "https://api.deepseek.com",
			Model:          "deepseek-chat",
			TimeoutSeconds: int64((45 * time.Second).Seconds()),
			RAG: RAGConfig{
				EmbeddingEndpoint:       "https://dashscope.aliyuncs.com/compatible-mode/v1",
				EmbeddingModel:          "text-embedding-v3",
				EmbeddingDimension:      1024,
				EmbeddingTimeoutSeconds: int64((15 * time.Second).Seconds()),
				MilvusAddress:           "127.0.0.1:19530",
				MilvusCollection:        "resume_experience_chunk_vectors",
				MilvusVectorField:       "dense_vector",
				MilvusSparseVectorField: "sparse_vector",
				MilvusTextField:         "search_text",
				MilvusMetricType:        "COSINE",
				MilvusOutputFields: []string{
					"chunk_id", "hr_id", "job_id", "created_at",
				},
				TopK: 8,
			},
		},
	}
	overrideString(&cfg.GRPCAddr, "LOGIC_GRPC_ADDR")
	overrideString(&cfg.MySQL.DSN, "MYSQL_DSN")
	overrideInt(&cfg.MySQL.MaxOpenConns, "MYSQL_MAX_OPEN_CONNS")
	overrideInt(&cfg.MySQL.MaxIdleConns, "MYSQL_MAX_IDLE_CONNS")
	overrideInt64(&cfg.MySQL.ConnMaxLifetimeSeconds, "MYSQL_CONN_MAX_LIFETIME_SECONDS")
	overrideString(&cfg.Redis.Address, "REDIS_ADDR")
	overrideString(&cfg.Redis.Password, "REDIS_PASSWORD")
	overrideInt(&cfg.Redis.DB, "REDIS_DB")
	overrideBool(&cfg.RecommendationQueue.Enabled, "RECOMMENDATION_QUEUE_ENABLED")
	overrideString(&cfg.RecommendationQueue.StreamName, "RECOMMENDATION_QUEUE_STREAM")
	overrideString(&cfg.RecommendationQueue.ConsumerGroup, "RECOMMENDATION_QUEUE_GROUP")
	overrideString(&cfg.RecommendationQueue.ConsumerName, "RECOMMENDATION_QUEUE_CONSUMER")
	overrideInt(&cfg.RecommendationQueue.WorkerCount, "RECOMMENDATION_WORKER_COUNT")
	overrideInt64(&cfg.RecommendationQueue.TaskTTLSeconds, "RECOMMENDATION_TASK_TTL_SECONDS")
	overrideInt64(&cfg.RecommendationQueue.TaskTimeoutSeconds, "RECOMMENDATION_TASK_TIMEOUT_SECONDS")
	overrideString(&cfg.AI.APIKey, "OPENAI_API_KEY")
	overrideString(&cfg.AI.BaseURL, "OPENAI_BASE_URL")
	overrideString(&cfg.AI.Model, "OPENAI_MODEL")
	overrideInt64(&cfg.AI.TimeoutSeconds, "OPENAI_TIMEOUT_SECONDS")
	overrideString(&cfg.AI.APIKey, "DEEPSEEK_API_KEY")
	overrideString(&cfg.AI.BaseURL, "DEEPSEEK_BASE_URL")
	overrideString(&cfg.AI.Model, "DEEPSEEK_MODEL")
	overrideInt64(&cfg.AI.TimeoutSeconds, "DEEPSEEK_TIMEOUT_SECONDS")
	overrideString(&cfg.AI.Provider, "AI_PROVIDER")
	if value := os.Getenv("LLM_GATEWAY_MODELS"); value != "" {
		models, err := parseLLMModelConfigs(value)
		if err != nil {
			return Config{}, err
		}
		cfg.AI.Gateway.Models = models
	} else {
		cfg.AI.Gateway.Models = []LLMModelConfig{{
			Name:           defaultLLMModelName(cfg.AI.Provider, cfg.AI.Model),
			Provider:       cfg.AI.Provider,
			APIKey:         cfg.AI.APIKey,
			BaseURL:        cfg.AI.BaseURL,
			Model:          cfg.AI.Model,
			TimeoutSeconds: cfg.AI.TimeoutSeconds,
		}}
	}
	overrideString(&cfg.AI.Gateway.SimpleJDQueryModel, "LLM_SIMPLE_JD_QUERY_MODEL")
	overrideString(&cfg.AI.Gateway.HRSearchPlanModel, "LLM_HR_SEARCH_PLAN_MODEL")
	overrideString(&cfg.AI.Gateway.RecommendationResultModel, "LLM_RECOMMENDATION_RESULT_MODEL")
	overrideString(&cfg.AI.Gateway.Address, "LLM_GATEWAY_ADDR")
	overrideString(&cfg.AI.Gateway.Token, "LLM_GATEWAY_TOKEN")
	overrideBool(&cfg.AI.RAG.Enabled, "RAG_ENABLED")
	overrideString(&cfg.AI.RAG.EmbeddingAPIKey, "DASHSCOPE_API_KEY")
	overrideString(&cfg.AI.RAG.EmbeddingAPIKey, "RAG_EMBEDDING_API_KEY")
	overrideString(&cfg.AI.RAG.EmbeddingEndpoint, "RAG_EMBEDDING_ENDPOINT")
	overrideString(&cfg.AI.RAG.EmbeddingModel, "RAG_EMBEDDING_MODEL")
	overrideInt(&cfg.AI.RAG.EmbeddingDimension, "RAG_EMBEDDING_DIMENSION")
	overrideInt64(&cfg.AI.RAG.EmbeddingTimeoutSeconds, "RAG_EMBEDDING_TIMEOUT_SECONDS")
	overrideString(&cfg.AI.RAG.MilvusAddress, "MILVUS_ADDRESS")
	overrideString(&cfg.AI.RAG.MilvusUsername, "MILVUS_USERNAME")
	overrideString(&cfg.AI.RAG.MilvusPassword, "MILVUS_PASSWORD")
	overrideString(&cfg.AI.RAG.MilvusDBName, "MILVUS_DB_NAME")
	overrideString(&cfg.AI.RAG.MilvusCollection, "MILVUS_COLLECTION")
	overrideString(&cfg.AI.RAG.MilvusVectorField, "MILVUS_VECTOR_FIELD")
	overrideString(&cfg.AI.RAG.MilvusSparseVectorField, "MILVUS_SPARSE_VECTOR_FIELD")
	overrideString(&cfg.AI.RAG.MilvusTextField, "MILVUS_TEXT_FIELD")
	overrideString(&cfg.AI.RAG.MilvusMetricType, "MILVUS_METRIC_TYPE")
	overrideStringSlice(&cfg.AI.RAG.MilvusOutputFields, "MILVUS_OUTPUT_FIELDS")
	overrideInt(&cfg.AI.RAG.TopK, "RAG_TOP_K")
	if cfg.MySQL.DSN == "" {
		return Config{}, errors.New("mysql dsn is required")
	}
	return cfg, nil
}

func parseLLMModelConfigs(value string) ([]LLMModelConfig, error) {
	var models []LLMModelConfig
	if err := json.Unmarshal([]byte(value), &models); err != nil {
		return nil, err
	}
	for i := range models {
		models[i].Name = strings.TrimSpace(models[i].Name)
		models[i].Provider = strings.TrimSpace(models[i].Provider)
		models[i].BaseURL = strings.TrimSpace(models[i].BaseURL)
		models[i].Model = strings.TrimSpace(models[i].Model)
		models[i].APIKeyEnv = strings.TrimSpace(models[i].APIKeyEnv)
		if models[i].APIKey == "" && models[i].APIKeyEnv != "" {
			models[i].APIKey = os.Getenv(models[i].APIKeyEnv)
		}
		if models[i].Provider == "" {
			models[i].Provider = models[i].Name
		}
		if models[i].Name == "" {
			models[i].Name = defaultLLMModelName(models[i].Provider, models[i].Model)
		}
		if models[i].TimeoutSeconds <= 0 {
			models[i].TimeoutSeconds = 45
		}
	}
	return models, nil
}

func defaultLLMModelName(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider != "" && model != "" {
		return provider + "/" + model
	}
	if model != "" {
		return model
	}
	if provider != "" {
		return provider
	}
	return "default"
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

func overrideInt64(target *int64, key string) {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			*target = parsed
		}
	}
}

func overrideInt(target *int, key string) {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			*target = parsed
		}
	}
}

func overrideBool(target *bool, key string) {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			*target = parsed
		}
	}
}

func overrideStringSlice(target *[]string, key string) {
	if value := os.Getenv(key); value != "" {
		parts := strings.Split(value, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
		if len(items) > 0 {
			*target = items
		}
	}
}
