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
	HTTPAddr       string
	APIToken       string
	DefaultTimeout time.Duration
	Models         []ModelConfig
	TaskRoutes     map[string]TaskRoute
}

type ModelConfig struct {
	Name                 string  `json:"name"`
	Provider             string  `json:"provider"`
	APIKey               string  `json:"api_key"`
	APIKeyEnv            string  `json:"api_key_env"`
	BaseURL              string  `json:"base_url"`
	Model                string  `json:"model"`
	TimeoutSeconds       int64   `json:"timeout_seconds"`
	ContextWindow        int     `json:"context_window"`
	MaxOutputTokens      int     `json:"max_output_tokens"`
	InputPricePer1KUSD   float64 `json:"input_price_per_1k_usd"`
	OutputPricePer1KUSD  float64 `json:"output_price_per_1k_usd"`
	RPM                  int     `json:"rpm"`
	TPM                  int     `json:"tpm"`
	Weight               int     `json:"weight"`
	SupportsStreaming    bool    `json:"supports_streaming"`
	SupportsJSONResponse bool    `json:"supports_json_response"`
}

type TaskRoute struct {
	Primary           string   `json:"primary"`
	Fallbacks         []string `json:"fallbacks"`
	MaxCostUSD        float64  `json:"max_cost_usd"`
	LatencySLOMillis  int64    `json:"latency_slo_ms"`
	RequiresJSON      bool     `json:"requires_json"`
	AllowFallback     bool     `json:"allow_fallback"`
	AllowStreamSwitch bool     `json:"allow_stream_switch"`
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}
	cfg := Config{
		HTTPAddr:       ":8090",
		DefaultTimeout: 45 * time.Second,
		TaskRoutes: map[string]TaskRoute{
			"simple_jd_query": {
				AllowFallback: true,
				RequiresJSON:  true,
			},
			"hr_search_plan": {
				AllowFallback: true,
				RequiresJSON:  true,
			},
			"resume_recommendation_result": {
				AllowFallback:     true,
				RequiresJSON:      true,
				AllowStreamSwitch: false,
			},
		},
	}
	overrideString(&cfg.HTTPAddr, "LLM_GATEWAY_HTTP_ADDR")
	overrideString(&cfg.APIToken, "LLM_GATEWAY_TOKEN")
	if seconds := envInt64("LLM_GATEWAY_DEFAULT_TIMEOUT_SECONDS"); seconds > 0 {
		cfg.DefaultTimeout = time.Duration(seconds) * time.Second
	}

	models, err := loadModels()
	if err != nil {
		return Config{}, err
	}
	cfg.Models = models
	if value := os.Getenv("LLM_TASK_ROUTES"); value != "" {
		var routes map[string]TaskRoute
		if err := json.Unmarshal([]byte(value), &routes); err != nil {
			return Config{}, err
		}
		for task, route := range routes {
			cfg.TaskRoutes[strings.TrimSpace(task)] = route
		}
	}
	overrideRoutePrimary(cfg.TaskRoutes, "simple_jd_query", "LLM_SIMPLE_JD_QUERY_MODEL")
	overrideRoutePrimary(cfg.TaskRoutes, "hr_search_plan", "LLM_HR_SEARCH_PLAN_MODEL")
	overrideRoutePrimary(cfg.TaskRoutes, "resume_recommendation_result", "LLM_RECOMMENDATION_RESULT_MODEL")
	if len(cfg.Models) == 0 {
		return Config{}, errors.New("at least one llm model is required")
	}
	return cfg, nil
}

func loadModels() ([]ModelConfig, error) {
	value := os.Getenv("LLM_GATEWAY_MODELS")
	if value == "" {
		model := ModelConfig{
			Name:                 defaultModelName(os.Getenv("AI_PROVIDER"), os.Getenv("DEEPSEEK_MODEL")),
			Provider:             envOr("AI_PROVIDER", "deepseek"),
			APIKey:               os.Getenv("DEEPSEEK_API_KEY"),
			BaseURL:              envOr("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
			Model:                envOr("DEEPSEEK_MODEL", "deepseek-chat"),
			TimeoutSeconds:       envInt64Or("DEEPSEEK_TIMEOUT_SECONDS", 45),
			ContextWindow:        envIntOr("LLM_DEFAULT_CONTEXT_WINDOW", 32000),
			MaxOutputTokens:      envIntOr("LLM_DEFAULT_MAX_OUTPUT_TOKENS", 4096),
			RPM:                  envIntOr("LLM_DEFAULT_RPM", 60),
			TPM:                  envIntOr("LLM_DEFAULT_TPM", 120000),
			Weight:               1,
			SupportsStreaming:    true,
			SupportsJSONResponse: true,
		}
		return []ModelConfig{model}, nil
	}
	var models []ModelConfig
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
			models[i].Name = defaultModelName(models[i].Provider, models[i].Model)
		}
		if models[i].TimeoutSeconds <= 0 {
			models[i].TimeoutSeconds = 45
		}
		if models[i].ContextWindow <= 0 {
			models[i].ContextWindow = 32000
		}
		if models[i].MaxOutputTokens <= 0 {
			models[i].MaxOutputTokens = 4096
		}
		if models[i].RPM <= 0 {
			models[i].RPM = 60
		}
		if models[i].TPM <= 0 {
			models[i].TPM = 120000
		}
		if models[i].Weight <= 0 {
			models[i].Weight = 1
		}
	}
	return models, nil
}

func loadDotEnv() error {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func overrideRoutePrimary(routes map[string]TaskRoute, task string, key string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	route := routes[task]
	route.Primary = value
	route.AllowFallback = true
	routes[task] = route
}

func defaultModelName(provider, model string) string {
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

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func envInt64(key string) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func envInt64Or(key string, fallback int64) int64 {
	if value := envInt64(key); value > 0 {
		return value
	}
	return fallback
}

func overrideString(target *string, key string) {
	if value := os.Getenv(key); value != "" {
		*target = value
	}
}
