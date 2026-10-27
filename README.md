# 🚀 LLM-Edge-Gateway：面向大模型的高性能云原生边缘网关

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io/)
[![Prometheus](https://img.shields.io/badge/Monitoring-Prometheus-E6522C?style=flat&logo=prometheus)](https://prometheus.io/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**LLM-Edge-Gateway** 是一个用 Go 从零构建的高性能 API 网关，专为 **大模型（LLM）业务场景**量身定制。项目采用云原生架构设计，具备动态路由、流量治理、熔断降级、全链路可观测性与优雅下线能力，解决了传统网关在 AI 推理场景下**流式响应被缓冲卡顿**、**算力成本失控**的两大痛点。

>  **它同时也是一份适合初学者逐行读完的【云原生 × AI】入门样本**：可通过最基本的HTTP网关来搭建。全项目不引入任何 Web 框架，只用标准库 + 5 个成熟依赖，所有难点（SSE 透传、Token 计费、熔断、优雅停机）都是独立的小文件。不需要 K8s / Prometheus / 推理部署经验，会 Go 和 HTTP 就能读。

---

## 核心特性

- **零缓冲流式透传（SSE）**：不使用 `httputil.ReverseProxy` 的默认行为，自行实现读帧、写帧再到Flush的转发循环，底层数据帧实时下发，消除了大模型"打字机"效果的首字延迟（TTFT）积压。
- **Token 级细粒度配额**：自定义 `io.ReadCloser` 流式拦截器，在转发过程中用 `bufio.Scanner` 逐行解析 SSE 数据包，借助 `tiktoken`实时计算 Token 消耗。超额时立即向流中注入 `data:{"error":"quota exceeded"}` 并主动终止响应，实现"流式计费 + 防超卖"；同时请求进入时还会预扣 10 个估算 Token，避免高并发下的额度竞态。
- 高可用与流量治理**：基于 `gobreaker/v2` 实现**路由级**熔断（连续 10 次失败进入 `OPEN`，10 秒后转 `HALF-OPEN` 试探）；基于 `golang.org/x/time/rate` 令牌桶实现 **按 API Key 的 QPS 限流，并对空闲限流器做定期回收，防止内存无界增长。
- **全链路可观测性**：通过 Prometheus 暴露 4 类核心指标（TTFT 分布、Token 吞吐、请求 QPS、耗时分布）；`log/slog` 输出结构化 JSON 日志，自动生成并向下游透传 `X-Trace-ID`，一行日志即可串起整条调用链。
- **云原生生命周期管理**：多阶段 Docker 构建，运行层只保留 `alpine` + 一个静态编译的二进制；监听 `SIGTERM` / `SIGINT` 实现优雅停机，正在传输的 SSE 流不会被 Kubernetes 滚动更新直接掐断。

---

## 架构图

### 请求路径

```text
[ Client (Web / SSE) ]
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│                 LLM-Edge-Gateway (Port :8080)                │
│                                                             │
│  [ 治理层 ] Logging → Metrics → RateLimit(QPS) → Router      │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ [ LLM 专属处理层 ]                                     │  │
│  │                                                       │  │
│  │  1. Token 配额预扣 (TokenQuota,)                       │  │
│  │  2. CircuitBreaker 熔断保护 (gobreaker v2)             │  │
│  │  3. 流式透传引擎 (Write + http.Flusher)                │  │
│  │  4. Token 实时计算器 (tiktoken / cl100k_base)          │  │
│  └───────────────────────────────────────────────────────┘  │
└──────────────────────────────┬──────────────────────────────┘
                               │ (SSE 流式转发)
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
     ┌────────────────┐ ┌──────────────┐ ┌──────────────┐
     │ vLLM / Mock LLM│ │ Users :9001  │ │ Orders :9002 │
     │    :9003       │ │  (普通 HTTP) │ │  (普通 HTTP)  │
     └────────────────┘ └──────────────┘ └──────────────┘
```

### 一次 SSE 请求的时序

```text
Client            Gateway                                  Upstream(LLM)
  │  GET /api/llm    │                                             │
  ├─────────────────▶│ LoggingMiddleware  生成/继承 X-Trace-ID     │
  │                  │ MetricsMiddleware  开始计时                 │
  │                  │ RateLimitMiddleware 令牌桶扣减              │
  │                  │ Router.Match       最长前缀匹配            │
  │                  │ TokenQuota         预扣 10 tokens          │
  │                  ├───────────── HTTP ─────────────────────────▶│
  │                  │◀──────────── 200 text/event-stream ────────┤
  │◀── header 透传 ───┤ WriteHeader + 逐帧 Write/Flush            │
  │◀── data:{"text":"你"} ──┤ 记录首帧时间 → Observe(TTFT)        │
  │◀── data:{"text":"好"} ──┤ CountTokens → Counter += n          │
  │   ...            │                                            │
  │◀── data:{"error":"quota exceeded"} ──┤ 配额耗尽 → EOF → 断流  │
  │◀── EOF ──────────┤ slog: stream interrupted due to quota      │
```

---

## 项目结构

```text
cloud-native-go/
├── cmd/
│   └── gateway/              # 入口，负责依赖注入与启动
│       └── main.go           # 中间件装配、探针、优雅停机
├── internal/                 # 核心业务逻辑
│   ├── config/               # 配置加载（YAML 解析 + 路由规则）
│   ├── llm/                  # 大模型专属逻辑
│   │   ├── token_counter.go  # SSE 流式拦截器 + TTFT 观测
│   │   └── quota.go          # 按 API Key 的 Token 配额
│   ├── metrics/              # Prometheus 指标定义与注册
│   ├── middleware/           # 洋葱模型中间件（日志 / 监控 / 限流）
│   └── router/               # 路由匹配、反向代理、熔断器
│       └── breaker.go        # 熔断器配置与状态变更日志
├── pkg/
│   └── trace/                # 可复用的 TraceID 生成与 Context 传递
├── test/					# 测试集
│   ├── mock_llm/             # 无限流式输出的 Mock LLM 后端
│   └── mocks/                # 基础 HTTP Mock 服务
├── config.yaml               # 动态路由配置
├── Makefile                  # 快捷开发命令
└── Dockerfile                # 多阶段容器构建
```

---

## 快速开始

### 1. 本地运行

确保已安装 **Go 1.27+**。克隆仓库并拉取依赖：

```bash
git clone https://github.com/maergaici/llm-edge-gateway.git
cd llm-edge-gateway
go mod tidy
```

> 首次启动时 `tiktoken-go` 会下载 `cl100k_base` 词表并缓存，请保证网络可达；离线环境可提前设置 `TIKTOKEN_CACHE_DIR` 指向本地缓存目录。

启动 Mock LLM 后端（模拟无限打字机效果，每 100ms 输出一帧）：

```bash
go run ./test/mock_llm
```

另开两个终端，按需启动普通 HTTP Mock 服务，用于验证非流式路由：

```bash
go run ./test/mocks/users    # :9001
go run ./test/mocks/orders   # :9002
```

启动网关（监听 `:8080`）。**必须在仓库根目录执行**，因为 `config.yaml` 按相对路径加载：

```bash
go run ./cmd/gateway
```

看到 `{"msg":"loaded routes","count":3}` 即代表启动成功。

![启动网关](docs\images\启动网关.png)

### 2. 功能验证

**① 体验 SSE 流式透传**

```bash
curl -N -H "X-API-Key: user-1" http://localhost:8080/api/llm
```

*现象：终端每隔约 100ms 逐个输出 `data:{"text":"..."}`，而不是等整段响应结束后一次性吐出。*

**② 验证 Token 实时计费与断流**

同一个 `X-API-Key` 持续请求，当累计消耗超过配额阈值（默认 **100 tokens / 分钟**）时，网关会在传输**中途**返回并结束流：

```text
data:{"text":"!"}

data:{"error":"quota exceeded"}
```

此时网关日志会打印 `stream interrupted due to quota exceeded`，后续请求将直接收到 `429 Too Many Requests`，直到该 Key 的配额窗口重置。

![断流](docs\images\断流.png)

![断流日志](docs\images\断流日志.png)

**③ 验证熔断与高可用**

```bash
# 1. 停掉 mock_llm 进程
# 2. 连续发起 15 次请求
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: user-2" http://localhost:8080/api/llm
done
```

*现象：前 10 次返回 `502 Bad Gateway`，第 11 次起熔断器进入 `OPEN`，网关直接返回 `503 service unavailable (circuit open)`，不再把请求打到已经宕机的后端。*

*重启后端并等待 10 秒（`Timeout` 期满），状态自动流转 `OPEN → HALF-OPEN → CLOSED`，日志中可看到：*

```json
{"level":"INFO","msg":"circuit breaker state changed","name":"/api/llm","from":"closed","to":"open"}
{"level":"INFO","msg":"circuit breaker state changed","name":"/api/llm","from":"open","to":"half-open"}
{"level":"INFO","msg":"circuit breaker state changed","name":"/api/llm","from":"half-open","to":"closed"}
```

![熔断后开启](docs\images\熔断后开启.png)

④ 验证健康探针与优雅停机**

```bash
curl localhost:8080/healthz   # healthy          存活探针
curl localhost:8080/readyz    # 启动后约 5s 才返回 ready（依赖就绪检查）
```

向网关进程发送 `Ctrl+C`（或 `kill -TERM <pid>`），日志会依次输出 `shutdown signal received` 与 `server exited cleanly`；若有正在传输的 SSE 流，最多等待 30 秒让其自然收尾。

### 3. Docker 部署

```bash
# 构建镜像（多阶段，运行层基于 alpine:3.20）
docker build -t llm-edge-gateway:0.1 .

# 运行容器（config.yaml 已随镜像打入）
docker run --rm -p 8080:8080 --name gateway llm-edge-gateway:0.1
```

---

## 配置说明

路由表完全由 `config.yaml` 驱动，新增后端无需改代码、重新编译：

```yaml
routes:
  - path_prefix: /api/users     # 匹配前缀（按长度倒序匹配，最长优先）
    upstream: http://localhost:9001
    strip_prefix: false         # 保留前缀转发：/api/users/1 -> /api/users/1

  - path_prefix: /api/orders
    upstream: http://localhost:9002
    strip_prefix: true          # 剥离前缀转发：/api/orders/1 -> /1

  - path_prefix: /api/llm
    upstream: http://localhost:9003
    strip_prefix: true
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `path_prefix` | string | 前缀匹配规则，多条命中时取最长前缀 |
| `upstream` | string | 后端地址，支持 http/https |
| `strip_prefix` | bool | 是否在转发前剥离匹配到的前缀 |

其他关键参数位于 `cmd/gateway/main.go`：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| 监听端口 | `:8080` | HTTP 服务端口 |
| QPS 限流 | `10 QPS`，burst `20` | 按 `X-API-Key`（缺省用 `RemoteAddr`）维度 |
| Token 配额 | `100 tokens / 分钟` | 按 Key 累计，进入请求时预扣 10 tokens |
| 熔断阈值 | 连续失败 `10` 次 | `Timeout` 10s 后进入半开状态 |
| 停机超时 | `30s` | 等待在途请求完成的最长时间 |

---

## 监控与指标

网关在 `/metrics` 暴露标准 Prometheus 格式数据：

| 指标 | 类型 | 标签 | 说明 |
| --- | --- | --- | --- |
| `http_requests_total` | Counter | `method` `path` `status` | 请求总量，可算 QPS 与错误率 |
| `http_request_duration_seconds` | Histogram | `method` `path` | 请求耗时分布，桶覆盖 10ms ~ 5s |
| `llm_time_to_first_token_seconds` | Histogram | `model` | 首字延迟（TTFT），桶覆盖 100ms ~ 10s |
| `llm_total_tokens_generated` | Counter | `model` | 系统整体生成的 Token 总量，直接映射算力成本 |

---

## 技术栈

- **语言**：Go（Goroutine / Channel / Context / `atomic` / `signal.NotifyContext`）
- **网络与协议**：`net/http`、SSE、HTTP/1.1、`http.Flusher`
- **核心依赖**：`prometheus/client_golang`、`sony/gobreaker/v2`、`pkoukk/tiktoken-go`、`golang.org/x/time/rate`、`gopkg.in/yaml.v3`
- **架构模式**：洋葱模型中间件、依赖注入、策略化路由表、状态机熔断

---

## 为什么它适合拿来学

- **零框架、可通读**：没有 gin、没有 Istio、没有 Redis，只有标准库 + 5 个成熟依赖，全项目 15 个 `.go` 文件，每个都可通读。
- **每个重点都是独立小模块**：SSE 透传、Token 计量、QPS 限流、熔断、优雅停机各占一个文件，读一个懂一个，不用先啃整套框架。
- **对初学者友好**：会 Go 基础语法、懂 HTTP 的 method / header / status code 就能开始；K8s、Prometheus、vLLM 都不需要预先掌握，跑起来看现象、再回头读代码即可。项目可以带初学者一步一步了解学习云原生以及与AI的简单结合。
- **能看到真实的工程痕迹**：探针、结构化日志、TraceID、Prometheus 指标、优雅停机、多阶段构建——这些是玩具项目通常没有、但工作里天天用的东西。

> 打开方式很简单：`go run ./test/mock_llm` 与 `go run ./cmd/gateway` 分别起后端和网关，`curl -N -H "X-API-Key: user-1" localhost:8080/api/llm` 看一眼流式输出，再从 `cmd/gateway/main.go` 顺着读下去。读不懂的地方，下面的「关键设计取舍」基本都解释了。

---

## 关键设计取舍：5 个  [选 A 不选 B]

> 读不懂的地方可以看看，可以当做读代码时的注解——每一条都对应仓库里的真实实现。

### 1. 代理实现：手写转发循环，而不是 `httputil.ReverseProxy`

- **`Director` 已过时**：直接修改原始请求对象，语义混乱，`X-Forwarded-For` 等头容易被客户端伪造；Go 1.20 起官方推荐用 `Rewrite`（`In` / `Out` 隔离 + `SetURL`）。
- **但 `Rewrite` 也解决不了本项目的问题**：`ReverseProxy` 内部用固定大小的 `copyBuffer` 搬运响应体，而我们要在搬运过程中逐帧解析、计量、甚至中途截断，只能自己接管 `ResponseWriter`。
- **本项目做法**：手动构造 `outReq`（克隆 Header、透传 `X-Trace-ID`）→ `httpClient.Do` → `Write` + `Flush` 逐帧回写。代价是 body 关闭与错误分支要自己兜住，收益是全链路完全可控。

### 2. 限流算法：QPS 层用令牌桶，Token 配额层是固定窗口

- **QPS 层选令牌桶**（`golang.org/x/time/rate`）：支持 burst，瞬时突发能过、长期速率被平滑约束，比固定窗口的 [临界双倍] 更稳。
- **Token 配额层是固定窗口**（每分钟重置）：实现只有几十行，`CheckAndReserve` / `AddUsage` / `IsExceeded` 三个方法，语义最直观。
- **它的缺陷**：窗口边界可能出现约 2 倍突发（59s 用满 100 tokens，61s 又用满 100）。改进方向是滑动窗口或令牌桶，已列入下方 [已知问题] 。
- **最核心的差异不在算法**：传统网关按 [请求数] 限流，而大模型一个请求可能烧掉几千 Token。本项目在**流式传输中途**按真实 Token 消耗截断，卡住的是成本而不是连接数。

### 3. 流式计量：包装 `io.ReadCloser`，而不是 `ReverseProxy.ModifyResponse`

- **`ModifyResponse` 拿不到 [过程] **：入参是完整的 `*http.Response`，一旦在里面把 body 读完再返回，客户端要等整条流结束才拿到数据；若为了计算而全量缓冲，长流会直接把内存吃满。
- **本项目做法**：`TokenCounter` 包装 `resp.Body`（实现 `Read` / `Close`），用 `bufio.Scanner` 逐行切 SSE 帧，算完这一帧的 Token 就立刻 `Write` + `Flush` 出去。内存里永远只有一行。

### 4. 熔断器泛型：`*struct{}`，而不是 `*http.Response`

- **选 `*struct{}`**：`gobreaker.CircuitBreaker[T]` 的返回类型选空结构体，不占内存、无需序列化，只当 [成功哨兵] 。
- **不带响应体**：泛型若用完整响应，为保证状态码不丢失就必须把整条流缓冲下来，流式传输直接失效。
- **失败判定交给谁**：`httpClient.Do` 的 error 与上游 `>= 500` 的状态码，在 `Execute` 闭包里 `return nil, err` 触发计数，与 [不持有响应体] 完美兼容。

### 5. 状态存储：进程内存（`map` + `Mutex`），而不是一上来就上 Redis

- **延迟**：网关是流量入口，每次请求多一次 Redis 往返就多几毫秒；LLM 的 TTFT 本来就高，不该让限流器成为新瓶颈。单机规模下进程内加锁的竞争几乎不可观测。
- **无状态、零外部依赖**：不接 Redis，滚动更新与 HPA 扩容就不受外部中间件可用性牵连；一个二进制 和 一份 yaml 就能完整跑起来，也方便本地复现和教学。
- **可替换性**：`TokenQuota` 目前是具体类型，公开方法就三个。换成 Redis 只需按同样签名实现一个 `RedisTokenQuota`（Lua 脚本 + `INCRBY` 保证原子性），路由层调用点无需改动；抽成 interface 已列入 Roadmap。
- **结论与边界**：当前允许 [多副本间毫秒窗口内的少量超额] ，换取更低延迟与零依赖；如果场景变成面向 C 端、对成本强一致的计费，就该果断使用 Redis。这是业务对 [多扣几分钱] 的容忍度问题，不是技术偏好问题。

### 附：几个实现过程中的坑

- 包装 `ResponseWriter` 时必须转发 `Flush()`，否则 SSE 会**静默退化**成「整段结束后一次性吐出」，这是最难排查的一类问题。
- `http.Client{Timeout: 0}` 是刻意为之：全局超时会把长连接流切断，超时控制应交给 `context`。
- `bufio.Scanner` 默认单行上限 64KB，超长帧会中断流，需要 `scanner.Buffer()` 扩容。
- 熔断器闭包里不能提前 `Close` 上游 body，必须等转发循环结束后再关。

---

## 已知问题

下面这些都是代码里真实存在的待改进点，难度从 ⭐ 到 ⭐⭐⭐

| # | 问题 | 位置 | 难度 | 提 |
| --- | --- | --- | --- | --- |
| 1 | 单帧超过 64KB 会触发 `bufio.Scanner` 默认行长度上限，流直接中断 | `internal/llm/token_counter.go` | ⭐⭐ | 用 `scanner.Buffer()` 扩容 |
| 2 | 配额是固定窗口，窗口边界处可能出现约 2 倍突发（100 tokens 额度在 59s 和 61s 各用 100） | `internal/llm/quota.go` | ⭐⭐⭐ | 换滑动窗口或令牌桶 |
| 3 | `/slow` 调试接口直接暴露在公网路由上，可被用来占满连接 | `cmd/gateway/main.go` | ⭐⭐ | 用 build tag 隔离，或仅 debug 模式注册 |
| 4 | 单条流没有最长时长限制，异常客户端可以一直挂着连接 | `internal/router/router.go` | ⭐⭐⭐ | 用 `context.WithTimeout` 包住转发循环 |

欢迎提 PR。提交前请先 `go vet ./...`，并说明复现步骤。

---

## Roadmap

- [ ] 配额与限流状态从进程内存迁移到 Redis，支持多副本共享
- [ ] 基于 `client-go` 实现 CRD（`LlmRoute`）+ Reconciler，路由热更新
- [ ] 多上游加权负载均衡与健康检查
- [ ] OpenTelemetry 全链路追踪（Trace 已预留 `X-Trace-ID` 透传能力）

---

## 📄 License

本项目基于 [MIT License](LICENSE) 开源。
