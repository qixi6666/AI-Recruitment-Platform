# AI Recruitment Platform

一个招聘业务系统，包含候选人投递、HR 岗位管理、简历附件上传、投递台账和简历推荐。

当前版本已经去掉通用 AI 对话和长期记忆，只保留一个核心智能能力：**基于 JD 推荐候选人简历**。

## 核心能力

- 候选人注册、登录、完善档案、上传简历、投递岗位。
- HR 发布、编辑、下架岗位，查看投递台账，下载候选人简历。
- HR 按岗位或 JD 发起简历推荐。
- 推荐模块使用 RAG 检索候选人经历证据，再由推荐 agent 输出结构化推荐结果。
- 推荐任务通过 Redis Stream 异步排队，避免大模型调用阻塞 Gin 网关。
- 最终推荐回答通过 SSE 流式推送给前端。

## 技术栈

| 模块 | 技术 |
| --- | --- |
| Frontend | Vue 3, TypeScript, Vite |
| HTTP Gateway | Go, Gin, JWT, SSE |
| Logic Service | Go, gRPC, GORM |
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
    └── ResumeRecommendationService
            |
            +--> Redis Stream Queue
            +--> MySQL
            +--> Milvus
            +--> LLM Recommendation Agent
```

说明：

- `web-gin-service` 是 HTTP 网关，负责 JWT、角色校验、REST API、文件上传和 SSE。
- `logic-grpc-service` 是核心业务进程，内部注册多个 gRPC service，但仍是一个部署单元。
- `shared` 保存 Web 和 Logic 共用的 RPC DTO、service descriptor 和 JSON codec。
- `recruitment-frontend` 是 Vue 前端。

## gRPC Service 拆分

Logic 进程内注册了 5 个业务 service：

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

系统保证至少一次投递。业务侧通过以下字段做幂等控制：

```text
task_id
status
owner_id
lease_until
```

Redis 只维护 task 的基础幂等租约，用来证明某个 `task_id` 正在被哪个 Worker 执行。重复投递到达时，如果已有有效租约，后来的 Worker 会跳过并 ACK；旧 Worker 后续恢复时会发现自己不再持有 `owner_id`，停止写入，避免重复输出。重试和 RAG 兜底属于 Worker 内部执行策略，不写入 Redis 状态机。

## 大模型兜底

大模型失败不会直接导致任务失败。

以下情况会降级为 RAG-only 推荐：

- 模型超时
- 模型返回 429 / rate limit
- 模型调用失败
- 模型输出 JSON 解析失败
- 未配置模型 API key

降级时会清理可能已经生成的半截最终回答 Stream，然后返回 RAG 检索候选人的兜底推荐结果，并以 `done` 结束任务。

## Redis Key 说明

```text
resume_recommendation:queue
  推荐任务队列，给 Worker 消费。

resume_recommendation:task:{task_id}
  任务状态 Hash，保存 hr_id、status、attempt 等。

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
