# AI Recruitment Platform

一个招聘业务系统，包含候选人投递、HR 岗位管理、简历附件上传、投递台账和简历推荐。

当前版本已经去掉通用 AI 对话和长期记忆，只保留一个核心智能能力：**基于 JD 推荐候选人简历**。

## 核心能力

- 候选人注册、登录、完善档案、上传简历、投递岗位。
- HR 发布、编辑、下架岗位，查看投递台账，下载候选人简历。
- HR 按岗位或 JD 发起简历推荐。
- HR 通过 LLM 调用网关并发评估多个 OpenAI-compatible 大模型。
- 推荐模块使用 RAG 检索候选人经历证据，再由推荐 agent 输出结构化推荐结果。
- 推荐任务通过 Redis Stream 异步排队，避免大模型调用阻塞 Gin 网关。
- 最终推荐回答通过 SSE 流式推送给前端。

## 技术栈

| 模块 | 技术 |
| --- | --- |
| Frontend | Vue 3, TypeScript, Vite |
| HTTP Gateway | Go, Gin, JWT, SSE |
| Logic Service | Go, gRPC, GORM |
| LLM Gateway | Go, Gin, OpenAI-compatible Adapter |
| Database | MySQL |
| Queue | Redis Stream Consumer Group |
| RAG | Milvus Hybrid Search, DashScope Embedding |
| LLM | OpenAI-compatible Chat Model, 默认 DeepSeek |
| File Storage | Gin 本地上传，MySQL 保存元数据 |

## 系统架构

```text
Vue Frontend
    |
    | HTTP / SSE
    v
Gin Web Gateway
    |
    | gRPC clients
    v
Logic gRPC Process
    ├── AuthService
    ├── JobService
    ├── CandidateService
    ├── ApplicationService
    ├── ResumeRecommendationService
    └── LLMGatewayService Proxy
            |
            +--> Redis Stream Queue
            +--> MySQL
            +--> Milvus
            |
            v
Standalone Gin LLM Gateway
    ├── Model Registry / Task Router
    ├── Rate Limiter / Circuit Breaker
    ├── Fallback Manager / Billing / Stats
    └── OpenAI-compatible Provider Adapters
            |
            +--> DeepSeek / OpenAI / Qwen / Local Inference
```

说明：

- `web-gin-service` 是 HTTP 网关，负责 JWT、角色校验、REST API、文件上传和 SSE。
- `logic-grpc-service` 是核心业务进程，内部注册多个 gRPC service，但仍是一个部署单元。
- `llm-gateway-service` 是独立 Gin 服务，负责模型注册、任务路由、Provider 调用、限流、熔断、fallback、SSE 转发、成本和统计。
- `shared` 保存 Web 和 Logic 共用的 RPC DTO、service descriptor 和 JSON codec。
- `recruitment-frontend` 是 Vue 前端。

## gRPC Service 拆分

Logic 进程内注册了 6 个业务 service：

```text
AuthService
  Register
  Login

JobService
  ListJobs
  CreateJob
  UpdateJob
  OfflineJob

CandidateService
  GetProfile
  UpsertProfile
  SaveResume
  GetResume

ApplicationService
  ApplyJob
  ListApplications

ResumeRecommendationService
  RecommendResumes
  CreateResumeRecommendationTask
  RecommendResumesStream
  WatchResumeRecommendationTask

LLMGatewayService
  ListModels
  Chat
  Evaluate
```

推荐模块单独作为 `ResumeRecommendationService`，方便后续独立扩容或拆成单独服务。

## 简历推荐链路

推荐模块只做一件事：**根据岗位 JD 推荐候选人**。

```text
候选人填写工作经历 / 项目经历
    -> 候选人投递岗位
    -> Logic 将经历拆成证据片段
    -> MySQL 保存证据事实
    -> Milvus 保存向量索引
    -> HR 发起推荐
    -> RAG 检索候选人经历证据
    -> 推荐 agent 基于证据生成推荐结果
    -> 前端展示候选人、分数、理由、风险点和证据
```

注意：

- 系统不解析上传的 PDF/DOC/DOCX 简历内容。
- RAG 使用候选人自填的工作经历和项目经历。
- 大模型只看到 JD 和 RAG 召回的证据片段，不允许凭空编造候选人能力。
- 如果模型不可用，会降级返回 RAG 排序结果。

## 异步任务与流式输出

推荐请求不会同步等待大模型。

前端先提交任务：

```text
POST /api/v1/hr/resume-recommendations
```

返回：

```json
{
  "task_id": "xxx",
  "status": "queued",
  "stream_url": "/api/v1/hr/resume-recommendations/xxx/stream"
}
```

然后前端建立 SSE：

```text
GET /api/v1/hr/resume-recommendations/{task_id}/stream
```

整体流程：

```text
Gin 创建推荐任务
    -> Logic 写入 Redis Stream 队列
    -> Worker 消费任务
    -> RAG 检索候选人
    -> 推荐 agent 生成最终推荐回答
    -> final_answer_streaming / done 写入任务输出 Stream
    -> Gin XRange / XRead 读取输出 Stream
    -> SSE 推给前端
```

任务输出 Stream 只保存最终回答相关事件：

```text
final_answer_streaming
done
failed
```

RAG 检索过程不写入输出 Stream。

正常完成后，Gin 会删除任务输出 Stream：

```text
DEL resume_recommendation:task:{task_id}:events
```

如果连接异常断开，输出 Stream 有 10 分钟 TTL 兜底清理。

推荐结果缓存只覆盖“默认按 JD 推荐”的请求，也就是没有 HR 额外检索要求的推荐任务。缓存 TTL 为 10 分钟；如果 HR 输入了额外要求、偏好或限制，系统只做队列层面的短时防重复提交，不缓存最终候选人推荐结果，避免不同检索意图之间互相污染。

## 可靠队列语义

推荐任务队列使用 Redis Stream Consumer Group。

```text
XREADGROUP 读取消息
    -> 消息进入 PEL
    -> 任务完整成功
    -> XACK
```

失败、超时、panic 或 Worker 崩溃时不会 `XACK`，消息继续留在 PEL。

健康 Worker 会定期恢复 pending 消息：

```text
XPENDING IDLE ...
XCLAIM ...
重新处理
成功后 XACK
```

系统保证至少一次投递。Redis 中只保留恢复和鉴权必要字段：

```text
resume_recommendation:request:{fingerprint} = task_id

resume_recommendation:task:{task_id}
    task_id
    hr_id
    status
    created_at
    owner_id      # 仅 running 时存在
    lease_until   # 仅 running 时存在
    error         # 仅 failed 时存在

{queue_stream}
    task_id
    payload = {"hr_id":..., "input":...}

resume_recommendation:task:{task_id}:events
    payload = ResumeRecommendationStreamChunk
```

`request_key`、`cache_key`、`updated_at`、`heartbeat_at` 不再重复落库。request/cache key 都可以从请求 fingerprint 推导；lease 是否有效只看 `owner_id` 和 `lease_until`。重复投递到达时，如果已有有效租约，后来的 Worker 会跳过并 ACK；旧 Worker 后续恢复时会发现自己不再持有 `owner_id`，停止写入，避免重复输出。

异步推荐任务还会把业务中间结果写入 checkpoint：

```text
resume_recommendation:task:{task_id}:checkpoint
    plan
    rag_items
    evidence_items
    candidates
```

checkpoint 写入时也会校验 `owner_id`。如果 Worker 在生成 plan、完成 RAG 召回、回表证据或聚合候选人之后崩溃，新的 Worker 通过 `XCLAIM` 接手后会从最近 checkpoint 继续执行。最终流式回答不做 checkpoint，因为它可能已经向客户端输出半截内容；恢复时只清理旧的输出 Stream，然后从 `candidates` 重新生成最终回答。

## 重试和降级边界

LLM Gateway 的 fallback 只处理“模型调用层”的失败，例如 Provider 429、模型超时、实例熔断、候选模型切换。Gateway fallback 成功后，业务 Worker 看到的是一次正常 LLM 调用结果。

Worker 的业务重试处理“任务执行层”的临时故障，例如 Redis 写事件失败、Milvus/MySQL 临时不可用、网络连接中断、短暂超时等。业务重试不会清空 checkpoint，只会清理可能已经输出的最终回答 Stream，并从已完成阶段继续跑。

以下错误不会做业务重试：

- 请求参数错误，例如缺少 JD 或 query
- 权限或资源不存在
- 当前 Worker 已失去 `owner_id`
- RAG 未启用或检索计划为空
- 模型已经在 Gateway 内部完成 fallback 后仍返回的确定性格式错误

RAG-only 由推荐业务层决定，不由 LLM Gateway 决定。Gateway 不知道“这个任务是否允许跳过最终推荐判断”，它只负责尽力返回模型结果。推荐业务层只有在已经拿到 RAG 候选证据、但最终推荐判断模型不可用或输出不可解析时，才返回 RAG-only；如果是 Milvus、MySQL、Redis 或权限问题，则任务失败，不用 RAG-only 掩盖数据链路故障。

以下情况会降级为 RAG-only 推荐：

- 模型超时
- 模型返回 429 / rate limit
- 模型调用失败
- 模型输出 JSON 解析失败

降级时会清理可能已经生成的半截最终回答 Stream，然后返回 RAG 检索候选人的兜底推荐结果，并以 `done` 结束任务。

## LLM 调用网关

LLM 网关已经独立为 `llm-gateway-service`，默认监听：

```text
http://127.0.0.1:8090
```

它对内部服务暴露：

```text
GET  /api/v1/models
GET  /api/v1/stats
POST /api/v1/chat
POST /api/v1/evaluate
POST /api/v1/stream
```

当前已落地的网关能力包括：

- Model Registry：统一管理模型名称、Provider、base_url、上下文窗口、价格、RPM/TPM、是否支持流式输出。
- Task Router / Policy：按 `simple_jd_query`、`hr_search_plan`、`resume_recommendation_result` 路由不同模型。
- Fallback Manager：非流式调用失败后按候选模型链 fallback；流式调用只在未输出 token 前允许切换。
- Rate Limiter：按模型维护进程内 RPM/TPM 预算。
- Circuit Breaker：模型连续失败后短时熔断，避免故障 Provider 被持续打爆。
- Load Selection：根据熔断状态、限流状态、上下文长度和权重选择候选模型。
- Provider Adapter：统一 OpenAI-compatible `/chat/completions` 调用。
- SSE Stream Proxy：把 Provider 的 stream 转成统一 `delta` / `done` SSE 事件。
- Billing / Observability：统计请求量、失败数、fallback 次数、token 和估算成本。

HTTP 网关给 HR 暴露三个接口：

```text
GET  /api/v1/hr/llm/models
POST /api/v1/hr/llm/chat
POST /api/v1/hr/llm/evaluate
```

`/llm/evaluate` 会把同一组 messages/prompt 并发发送给多个模型，并返回每个模型的输出、耗时、token usage 和错误信息，适合做小批量人工评测。

示例请求：

```json
{
  "models": ["deepseek-chat", "openai-gpt-4o-mini"],
  "system_prompt": "你是招聘系统的回答评测助手。",
  "prompt": "请用三句话说明 Go 后端候选人的核心评估维度。",
  "temperature": 0,
  "max_tokens": 512
}
```

多模型配置使用 `LLM_GATEWAY_MODELS`：

```bash
LLM_GATEWAY_MODELS='[{"name":"deepseek-chat","provider":"deepseek","api_key_env":"DEEPSEEK_API_KEY","base_url":"https://api.deepseek.com","model":"deepseek-chat","timeout_seconds":45},{"name":"openai-gpt-4o-mini","provider":"openai","api_key_env":"OPENAI_API_KEY","base_url":"https://api.openai.com/v1","model":"gpt-4o-mini","timeout_seconds":45}]'
```

如果不配置 `LLM_GATEWAY_MODELS`，网关会默认复用当前 `DEEPSEEK_*` / `OPENAI_*` 这组 OpenAI-compatible 模型配置。

业务推荐链路内部也会走 LLM Gateway，并按任务类型路由模型：

```text
simple_jd_query
  仅根据 JD 生成 RAG 检索 query 和基础过滤条件，非流式。

hr_search_plan
  综合 JD 与 HR 额外要求生成检索计划，能元数据过滤的生成 filter，不能过滤的进入语义 query / keywords，非流式。

resume_recommendation_result
  基于 RAG 证据生成最终候选人推荐判断，流式输出。
```

可以用以下变量为不同任务指定不同模型，复杂 HR 检索计划建议配置为更强、JSON 更稳定的模型：

```bash
LLM_SIMPLE_JD_QUERY_MODEL="qwen-turbo"
LLM_HR_SEARCH_PLAN_MODEL="qwen-max"
LLM_RECOMMENDATION_RESULT_MODEL="deepseek-chat"
```

## Redis Key 说明

```text
resume_recommendation:queue
  推荐任务队列，给 Worker 消费。

resume_recommendation:request:{fingerprint}
  短时防重复提交；默认 JD 推荐成功后和结果缓存同 TTL，带 HR 额外要求的任务完成后删除。

resume_recommendation:cache:{fingerprint}
  默认 JD 推荐结果缓存，不缓存带 HR 额外要求的推荐结果。

resume_recommendation:task:{task_id}
  任务状态 Hash，只保存 task_id、hr_id、status、created_at、运行中 lease 和失败原因。

resume_recommendation:task:{task_id}:checkpoint
  任务恢复 Hash，只保存 plan、rag_items、evidence_items、candidates。

resume_recommendation:task:{task_id}:events
  单个任务的最终回答输出 Stream，只保存 final_answer_streaming / done / failed。
```

## 环境变量

基础配置：

```bash
MYSQL_DSN="root:password@tcp(127.0.0.1:3306)/recruitment?charset=utf8mb4&parseTime=True&loc=Local"
JWT_SECRET="change-me"
UPLOAD_DIR="uploads"
```

Redis 和推荐队列：

```bash
REDIS_ADDR="127.0.0.1:6379"
REDIS_PASSWORD=""
REDIS_DB=0

RECOMMENDATION_QUEUE_ENABLED=true
RECOMMENDATION_QUEUE_STREAM="resume_recommendation:queue"
RECOMMENDATION_QUEUE_GROUP="resume_recommendation:workers"
RECOMMENDATION_QUEUE_CONSUMER="logic"
RECOMMENDATION_WORKER_COUNT=5
RECOMMENDATION_TASK_TTL_SECONDS=86400
RECOMMENDATION_TASK_TIMEOUT_SECONDS=300
```

模型配置：

```bash
DEEPSEEK_API_KEY=""
DEEPSEEK_BASE_URL="https://api.deepseek.com"
DEEPSEEK_MODEL="deepseek-chat"
DEEPSEEK_TIMEOUT_SECONDS=45
```

RAG 配置：

```bash
RAG_ENABLED=true
DASHSCOPE_API_KEY=""
RAG_EMBEDDING_ENDPOINT="https://dashscope.aliyuncs.com/compatible-mode/v1"
RAG_EMBEDDING_MODEL="text-embedding-v3"
RAG_EMBEDDING_DIMENSION=1024
RAG_EMBEDDING_TIMEOUT_SECONDS=15

MILVUS_ADDRESS="127.0.0.1:19530"
MILVUS_COLLECTION="resume_experience_chunk_vectors"
MILVUS_VECTOR_FIELD="dense_vector"
MILVUS_SPARSE_VECTOR_FIELD="sparse_vector"
MILVUS_TEXT_FIELD="search_text"
MILVUS_METRIC_TYPE="COSINE"
MILVUS_OUTPUT_FIELDS="chunk_id,hr_id,job_id,application_id,resume_id,candidate_id,education_degree,education_rank,experience_months,created_at"
RAG_TOP_K=8
```

更多示例见 [.env.example](./.env.example)。

## Docker Compose 启动

```bash
docker compose up --build -d
```

默认地址：

```text
Frontend:  http://localhost:5173
Web API:   http://localhost:8080
gRPC:      localhost:9002
MySQL:     localhost:3306
Redis:     localhost:6379
Milvus:    localhost:19530
MinIO UI:  http://localhost:9001
```

## 本地启动

启动 Logic：

```bash
cd logic-grpc-service
go run ./cmd/server
```

启动 Web：

```bash
cd web-gin-service
go run ./cmd/server
```

启动前端：

```bash
cd recruitment-frontend
npm install
npm run dev
```

## 验证

Go：

```bash
cd shared
GOCACHE=/tmp/codex-gocache go test ./...
GOCACHE=/tmp/codex-gocache go vet ./...

cd ../logic-grpc-service
GOCACHE=/tmp/codex-gocache go test ./...
GOCACHE=/tmp/codex-gocache go vet ./...

cd ../web-gin-service
GOCACHE=/tmp/codex-gocache go test ./...
GOCACHE=/tmp/codex-gocache go vet ./...
```

Frontend：

```bash
cd recruitment-frontend
npm run build
```
