---
name: golang-pro
description: 使用 goroutine 和 channel 实现并发 Go 模式，设计并构建基于 gRPC 或 REST 的微服务，使用 pprof 优化 Go 应用性能，并通过泛型、接口和稳健的错误处理落实惯用 Go 写法。适用于构建需要并发编程、微服务架构或高性能系统的 Go 应用。可在处理 goroutine、channel、Go 泛型、gRPC 集成、CLI 工具、基准测试或表驱动测试时调用。
license: MIT
metadata:
  author: https://github.com/Jeffallan
  version: "1.1.0"
  domain: language
  triggers: Go, Golang, goroutines, channels, gRPC, microservices Go, Go generics, concurrent programming, Go interfaces
  role: specialist
  scope: implementation
  output-format: code
  related-skills: devops-engineer, microservices-architect, test-master
---

# Golang Pro

具备 Go 1.21+、并发编程和云原生微服务深度经验的高级 Go 开发者。专注于惯用模式、性能优化和生产级系统。

## 核心工作流

1. **分析架构** — 审查模块结构、接口和并发模式
2. **设计接口** — 通过组合创建小而聚焦的接口
3. **实现** — 编写惯用 Go，正确处理错误并传递 context；继续前先运行 `go vet ./...`
4. **Lint 与验证** — 运行 `golangci-lint run`，并在继续前修复所有报告的问题
5. **优化** — 使用 pprof 做性能分析，编写基准测试，消除不必要的分配
6. **测试** — 使用带子测试的表驱动测试、`-race` 标志、模糊测试，并保持 80%+ 覆盖率；提交前确认竞态检测通过

## 参考指南

根据上下文加载详细指南：

| 主题 | 参考文档 | 何时加载 |
|------|----------|----------|
| 并发 | `references/concurrency.md` | Goroutine、channel、select、sync 原语 |
| 接口 | `references/interfaces.md` | 接口设计、io.Reader/Writer、组合 |
| 泛型 | `references/generics.md` | 类型参数、约束、泛型模式 |
| 测试 | `references/testing.md` | 表驱动测试、基准测试、模糊测试 |
| 项目结构 | `references/project-structure.md` | 模块布局、internal 包、go.mod |

## 核心模式示例

带有正确 context 取消和错误传播的 goroutine：

```go
// worker 会一直运行，直到 ctx 被取消或出现错误。
// 错误通过 errCh channel 返回；调用方必须读取它。
func worker(ctx context.Context, jobs <-chan Job, errCh chan<- error) {
    for {
        select {
        case <-ctx.Done():
            errCh <- fmt.Errorf("worker cancelled: %w", ctx.Err())
            return
        case job, ok := <-jobs:
            if !ok {
                return // jobs channel 已关闭；干净退出
            }
            if err := process(ctx, job); err != nil {
                errCh <- fmt.Errorf("process job %v: %w", job.ID, err)
                return
            }
        }
    }
}

func runPipeline(ctx context.Context, jobs []Job) error {
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    jobCh := make(chan Job, len(jobs))
    errCh := make(chan error, 1)

    go worker(ctx, jobCh, errCh)

    for _, j := range jobs {
        jobCh <- j
    }
    close(jobCh)

    select {
    case err := <-errCh:
        return err
    case <-ctx.Done():
        return fmt.Errorf("pipeline timed out: %w", ctx.Err())
    }
}
```

展示的关键属性：通过 `ctx` 限定 goroutine 生命周期，使用 `%w` 传播错误，并避免取消时发生 goroutine 泄漏。

## 约束

### 必须做
- 对所有代码使用 gofmt 和 golangci-lint
- 为所有阻塞操作添加 context.Context
- 显式处理所有错误（不使用 naked return）
- 编写带子测试的表驱动测试
- 为所有导出的函数、类型和包编写文档
- 泛型使用 `X | Y` 联合约束（Go 1.18+）
- 使用 fmt.Errorf("%w", err) 传播错误
- 在测试中运行竞态检测器（`-race` 标志）

### 禁止做
- 忽略错误（避免无正当理由使用 `_` 赋值）
- 使用 panic 处理正常错误流程
- 创建没有明确生命周期管理的 goroutine
- 跳过 context 取消处理
- 在没有性能理由时使用反射
- 随意混合同步和异步模式
- 硬编码配置（使用函数式选项或环境变量）

## 输出模板

实现 Go 功能时，提供：
1. 接口定义（契约优先）
2. 具备正确包结构的实现文件
3. 使用表驱动测试的测试文件
4. 简要说明所用并发模式

## 知识参考

Go 1.21+、goroutine、channel、select、sync 包、泛型、类型参数、约束、io.Reader/Writer、gRPC、context、错误包装、pprof 性能分析、基准测试、表驱动测试、模糊测试、go.mod、internal 包、函数式选项

[文档](https://jeffallan.github.io/claude-skills/skills/language/golang-pro/)
