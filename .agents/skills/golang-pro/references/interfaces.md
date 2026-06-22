# 接口设计与组合

## 小而聚焦的接口

```go
// 单方法接口（惯用 Go 写法）
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}

type Closer interface {
    Close() error
}

// 接口组合
type ReadCloser interface {
    Reader
    Closer
}

type WriteCloser interface {
    Writer
    Closer
}

type ReadWriteCloser interface {
    Reader
    Writer
    Closer
}
```

## 接收接口，返回结构体

```go
package storage

import "io"

// Storage 是具体类型（结构体）
type Storage struct {
    baseDir string
}

// NewStorage 返回具体类型
func NewStorage(baseDir string) *Storage {
    return &Storage{baseDir: baseDir}
}

// SaveFile 接收接口以获得灵活性
func (s *Storage) SaveFile(filename string, data io.Reader) error {
    // 实现可以处理任意 Reader
    // （文件、网络、缓冲区等）
    return nil
}

// 使用方式支持依赖注入
type Uploader interface {
    SaveFile(filename string, data io.Reader) error
}

type Service struct {
    uploader Uploader // 接收接口
}

// NewService 接收接口，便于测试替换
func NewService(uploader Uploader) *Service {
    return &Service{uploader: uploader}
}
```

## io.Reader 和 io.Writer 模式

```go
import (
    "io"
    "strings"
)

// 使用 io.MultiReader 串联 reader
func combineReaders() io.Reader {
    r1 := strings.NewReader("Hello ")
    r2 := strings.NewReader("World")
    return io.MultiReader(r1, r2)
}

// Tee reader 用于复制读取内容
func duplicateRead(r io.Reader, w io.Writer) io.Reader {
    return io.TeeReader(r, w) // 从 r 读取时同时写入 w
}

// 限制 reader，防止读取过多数据
func limitedRead(r io.Reader, n int64) io.Reader {
    return io.LimitReader(r, n)
}

// 自定义 Reader 实现
type UppercaseReader struct {
    src io.Reader
}

func (u *UppercaseReader) Read(p []byte) (n int, err error) {
    n, err = u.src.Read(p)
    for i := 0; i < n; i++ {
        if p[i] >= 'a' && p[i] <= 'z' {
            p[i] = p[i] - 32
        }
    }
    return n, err
}

// 自定义 Writer 实现
type CountingWriter struct {
    w     io.Writer
    count int64
}

func (cw *CountingWriter) Write(p []byte) (n int, err error) {
    n, err = cw.w.Write(p)
    cw.count += int64(n)
    return n, err
}

func (cw *CountingWriter) BytesWritten() int64 {
    return cw.count
}
```

## 通过嵌入实现组合

```go
import "sync"

// 通过嵌入扩展行为
type SafeCounter struct {
    mu sync.Mutex
    m  map[string]int
}

func (sc *SafeCounter) Inc(key string) {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    sc.m[key]++
}

// 嵌入接口以添加默认行为
type Logger interface {
    Log(msg string)
}

type NoOpLogger struct{}

func (NoOpLogger) Log(msg string) {}

type Service struct {
    Logger // 嵌入接口（可提供默认实现）
}

func NewService(logger Logger) *Service {
    if logger == nil {
        logger = NoOpLogger{} // 提供默认值
    }
    return &Service{Logger: logger}
}

// 现在可以使用 Service.Log()
```

## 接口满足性验证

```go
import "io"

// 编译期接口验证
var _ io.Reader = (*MyReader)(nil)
var _ io.Writer = (*MyWriter)(nil)
var _ io.Closer = (*MyCloser)(nil)

type MyReader struct{}

func (m *MyReader) Read(p []byte) (n int, err error) {
    return 0, nil
}

type MyWriter struct{}

func (m *MyWriter) Write(p []byte) (n int, err error) {
    return len(p), nil
}

type MyCloser struct{}

func (m *MyCloser) Close() error {
    return nil
}
```

## 函数式选项模式

```go
package server

import "time"

type Server struct {
    host         string
    port         int
    timeout      time.Duration
    maxConns     int
    enableLogger bool
}

// Option 是用于配置 Server 的函数式选项
type Option func(*Server)

func WithHost(host string) Option {
    return func(s *Server) {
        s.host = host
    }
}

func WithPort(port int) Option {
    return func(s *Server) {
        s.port = port
    }
}

func WithTimeout(timeout time.Duration) Option {
    return func(s *Server) {
        s.timeout = timeout
    }
}

func WithMaxConnections(max int) Option {
    return func(s *Server) {
        s.maxConns = max
    }
}

func WithLogger(enabled bool) Option {
    return func(s *Server) {
        s.enableLogger = enabled
    }
}

// NewServer 使用函数式选项创建 server
func NewServer(opts ...Option) *Server {
    // 默认值
    s := &Server{
        host:     "localhost",
        port:     8080,
        timeout:  30 * time.Second,
        maxConns: 100,
    }

    // 应用选项
    for _, opt := range opts {
        opt(s)
    }

    return s
}

// 用法：
// server := NewServer(
//     WithHost("0.0.0.0"),
//     WithPort(9000),
//     WithTimeout(60 * time.Second),
//     WithLogger(true),
// )
```

## 接口隔离

```go
// 不好：臃肿接口
type BadRepository interface {
    Create(item Item) error
    Read(id string) (Item, error)
    Update(item Item) error
    Delete(id string) error
    List() ([]Item, error)
    Search(query string) ([]Item, error)
    Count() (int, error)
}

// 好：隔离后的接口
type Creator interface {
    Create(item Item) error
}

type Reader interface {
    Read(id string) (Item, error)
}

type Updater interface {
    Update(item Item) error
}

type Deleter interface {
    Delete(id string) error
}

type Lister interface {
    List() ([]Item, error)
}

// 只组合需要的能力
type ReadWriter interface {
    Reader
    Creator
}

type FullRepository interface {
    Creator
    Reader
    Updater
    Deleter
    Lister
}
```

## 类型断言与类型 switch

```go
import "fmt"

// 安全类型断言
func processValue(v interface{}) {
    // 双返回值断言（安全）
    if str, ok := v.(string); ok {
        fmt.Println("String:", str)
        return
    }

    // 类型 switch
    switch val := v.(type) {
    case int:
        fmt.Println("Int:", val)
    case string:
        fmt.Println("String:", val)
    case bool:
        fmt.Println("Bool:", val)
    default:
        fmt.Println("Unknown type")
    }
}

// 检查可选接口方法
type Flusher interface {
    Flush() error
}

func writeAndFlush(w io.Writer, data []byte) error {
    if _, err := w.Write(data); err != nil {
        return err
    }

    // 检查 Writer 是否也实现了 Flusher
    if flusher, ok := w.(Flusher); ok {
        return flusher.Flush()
    }

    return nil
}
```

## 通过接口进行依赖注入

```go
package app

import "context"

// 为依赖定义接口
type UserRepository interface {
    GetUser(ctx context.Context, id string) (*User, error)
    SaveUser(ctx context.Context, user *User) error
}

type EmailSender interface {
    SendEmail(ctx context.Context, to, subject, body string) error
}

// Service 依赖接口
type UserService struct {
    repo   UserRepository
    mailer EmailSender
}

func NewUserService(repo UserRepository, mailer EmailSender) *UserService {
    return &UserService{
        repo:   repo,
        mailer: mailer,
    }
}

func (s *UserService) RegisterUser(ctx context.Context, email string) error {
    user := &User{Email: email}
    if err := s.repo.SaveUser(ctx, user); err != nil {
        return err
    }
    return s.mailer.SendEmail(ctx, email, "Welcome", "Thanks for registering!")
}

// 在测试中很容易 mock
type MockUserRepository struct{}

func (m *MockUserRepository) GetUser(ctx context.Context, id string) (*User, error) {
    return &User{ID: id}, nil
}

func (m *MockUserRepository) SaveUser(ctx context.Context, user *User) error {
    return nil
}
```

## 快速参考

| 模式 | 使用场景 | 核心原则 |
|------|----------|----------|
| 小接口 | 灵活性 | 单方法接口 |
| 接收接口 | 可测试性 | 依赖抽象 |
| 返回结构体 | 清晰性 | 具体返回类型 |
| io.Reader/Writer | I/O 操作 | 标准库集成 |
| 嵌入 | 组合 | 不通过继承扩展行为 |
| 函数式选项 | 配置 | 灵活构造函数 |
| 类型断言 | 运行时检查 | 安全向下转型 |
