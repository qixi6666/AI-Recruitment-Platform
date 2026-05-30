package ai

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"

	"recruitment/logic-grpc-service/internal/config"
	"recruitment/logic-grpc-service/internal/domain"
)

type Client struct {
	cfg config.AIConfig
	db  *gorm.DB
}

type AgentAnswer struct {
	Answer    string
	UsedTools []string
	ToolCalls []ToolCallRecord
}

type ToolCallRecord struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
	Error     string `json:"error,omitempty"`
}

func NewClient(cfg config.AIConfig, db *gorm.DB) *Client {
	return &Client{cfg: cfg, db: db}
}

func (c *Client) SummarizeMemory(ctx context.Context, oldSummary string, pendingHistory string) (string, error) {
	if c.cfg.APIKey == "" {
		return "", fmt.Errorf("ai api key is required for memory summarization")
	}
	pendingHistory = strings.TrimSpace(pendingHistory)
	if pendingHistory == "" {
		return strings.TrimSpace(oldSummary), nil
	}
	timeout := time.Duration(c.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  c.cfg.APIKey,
		Model:   c.cfg.Model,
		BaseURL: c.cfg.BaseURL,
		Timeout: timeout,
	})
	if err != nil {
		return "", err
	}
	msg, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(memorySummaryInstruction()),
		schema.UserMessage(fmt.Sprintf("已有摘要：\n%s\n\n待压缩历史：\n%s", strings.TrimSpace(oldSummary), pendingHistory)),
	})
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(msg.Content)
	if summary == "" {
		return "", fmt.Errorf("memory summarizer returned empty summary")
	}
	return summary, nil
}

func (c *Client) AnswerWithTools(ctx context.Context, hrID uint64, question string, history []*schema.Message) (AgentAnswer, error) {
	var b strings.Builder
	answer, err := c.StreamWithTools(ctx, hrID, question, history, func(chunk string) error {
		b.WriteString(chunk)
		return nil
	})
	if err != nil {
		return AgentAnswer{}, err
	}
	answer.Answer = strings.TrimSpace(b.String())
	if answer.Answer == "" {
		return AgentAnswer{}, fmt.Errorf("agent returned empty answer")
	}
	return answer, nil
}

func (c *Client) StreamWithTools(ctx context.Context, hrID uint64, question string, history []*schema.Message, onChunk func(string) error) (AgentAnswer, error) {
	if c.cfg.APIKey == "" {
		return AgentAnswer{}, fmt.Errorf("ai api key is required for ChatModelAgent tool calling")
	}
	timeout := time.Duration(c.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  c.cfg.APIKey,
		Model:   c.cfg.Model,
		BaseURL: c.cfg.BaseURL,
		Timeout: timeout,
	})
	if err != nil {
		return AgentAnswer{}, err
	}
	tracker := &toolCallTracker{}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "HRRecruitmentDataAgent",
		Description: "Answer HR recruitment operation questions by deciding whether to call MySQL-backed tools.",
		Instruction: buildInstruction(),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:               c.toolsForHR(hrID),
				ExecuteSequentially: true,
			},
		},
		Handlers:      []adk.ChatModelAgentMiddleware{tracker},
		MaxIterations: 8,
	})
	if err != nil {
		return AgentAnswer{}, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	messages := make([]adk.Message, 0, len(history)+1)
	messages = append(messages, history...)
	messages = append(messages, schema.UserMessage(question))
	iter := runner.Run(ctx, messages)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return AgentAnswer{}, event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		if event.Output.MessageOutput.Role != schema.Assistant {
			continue
		}
		if event.Output.MessageOutput.IsStreaming {
			if err := consumeAssistantStream(event.Output.MessageOutput.MessageStream, onChunk); err != nil {
				return AgentAnswer{}, err
			}
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			return AgentAnswer{}, err
		}
		if msg.Content != "" {
			if err := onChunk(msg.Content); err != nil {
				return AgentAnswer{}, err
			}
		}
	}
	return AgentAnswer{UsedTools: tracker.names(), ToolCalls: tracker.recordsSnapshot()}, nil
}

func consumeAssistantStream(stream adk.MessageStream, onChunk func(string) error) error {
	if stream == nil {
		return nil
	}
	defer stream.Close()
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if chunk == nil || chunk.Content == "" {
			continue
		}
		if err := onChunk(chunk.Content); err != nil {
			return err
		}
	}
}

func buildInstruction() string {
	var b strings.Builder
	b.WriteString("你是招聘管理系统 HR 管理端的常驻 AI 数据助手。\n")
	b.WriteString("你可以自行判断是否需要调用工具来查询 MySQL 真实业务数据。\n")
	b.WriteString("可回答范围：投递总人数、单岗位投递统计、符合条件候选人筛选、岗位热度数据、岗位与候选人总体概览、候选人经历语义检索、基于 JD 的候选人推荐。\n")
	b.WriteString("涉及统计时调用 get_recruitment_stats；涉及结构化候选人筛选时调用 search_candidates；不要编造候选人、岗位、投递数量、简历附件或经历数据。\n")
	b.WriteString("当问题涉及候选人填写的项目经历、工作经历、业务背景、技术细节等非结构化内容时，优先调用 semantic_search_resumes 工具。\n")
	b.WriteString("当 HR 要求推荐候选人或按岗位 JD 匹配简历时，调用 recommend_resumes_by_jd；你需要基于工具返回的候选人自填项目/工作证据判断匹配度、给出评分、推荐理由和风险点，不要编造证据。\n")
	b.WriteString("历史工具调用记录只用于理解上下文；涉及当前统计、筛选和推荐时仍以本轮工具返回结果为准。\n")
	b.WriteString("工具已经按当前 HR 账号做了数据隔离，你不能要求或推断其他 HR 的数据。\n")
	b.WriteString("最终回答使用中文，简洁、结构化，明确说明查询口径来自当前 HR 创建的岗位。\n")
	return b.String()
}

func memorySummaryInstruction() string {
	var b strings.Builder
	b.WriteString("你是招聘系统 Agent 的长期记忆压缩器。\n")
	b.WriteString("请把待压缩历史合并进已有摘要，输出新的滚动摘要。\n")
	b.WriteString("只保留用户长期目标、偏好、已确认的系统设计、稳定背景和未完成事项。\n")
	b.WriteString("不要保留工具调用的详细参数、原始 JSON、候选人列表、岗位数量、投递数量、评分等可能过期的数据。\n")
	b.WriteString("工具相关最多概括为用户讨论过某类查询或推荐能力；涉及实时招聘数据时必须说明后续需要重新调用工具查询。\n")
	b.WriteString("摘要使用中文，结构化、简洁，控制在 800 字以内。\n")
	return b.String()
}

type toolCallTracker struct {
	*adk.BaseChatModelAgentMiddleware
	mu      sync.Mutex
	records []ToolCallRecord
}

func (t *toolCallTracker) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, toolCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		record := ToolCallRecord{Name: toolCtx.Name, Arguments: argumentsInJSON, Result: result}
		if err != nil {
			record.Error = err.Error()
		}
		t.mu.Lock()
		t.records = append(t.records, record)
		t.mu.Unlock()
		return result, err
	}, nil
}

func (t *toolCallTracker) names() []string {
	records := t.recordsSnapshot()
	out := make([]string, 0, len(records))
	seen := map[string]struct{}{}
	for _, record := range records {
		name := record.Name
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func (t *toolCallTracker) recordsSnapshot() []ToolCallRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]ToolCallRecord, len(t.records))
	copy(out, t.records)
	return out
}

func applyJobFilters(q *gorm.DB, filters JobFilters, includeSkills bool) *gorm.DB {
	if status := validStatus(filters.Status); status != "" {
		q = q.Where("jobs.status = ?", status)
	}
	if filters.City != "" {
		q = q.Where("jobs.city LIKE ?", "%"+cleanText(filters.City)+"%")
	}
	for _, keyword := range cleanList(filters.JobKeywords, 5) {
		like := "%" + keyword + "%"
		q = q.Where("(jobs.title LIKE ? OR jobs.skills LIKE ? OR jobs.description LIKE ?)", like, like, like)
	}
	if includeSkills {
		for _, skill := range cleanList(filters.Skills, 8) {
			q = q.Where("jobs.skills LIKE ?", "%"+skill+"%")
		}
	}
	return q
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func cleanList(items []string, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = cleanText(item)
		if len([]rune(item)) < 2 || len([]rune(item)) > 30 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func cleanText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'` ")
	if len([]rune(value)) > 50 {
		return string([]rune(value)[:50])
	}
	return value
}

func validStatus(status string) string {
	status = cleanText(status)
	if status != domain.JobStatusOpen && status != domain.JobStatusOffline {
		return ""
	}
	return status
}
