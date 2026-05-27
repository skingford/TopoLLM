# TopoLLM

LLM 企业级聚合平台 —— 自研（Go）大模型调用聚合网关。对外统一 OpenAI 兼容协议，对内适配国内外主流厂商，支持运行时自定义供应商。

## 快速开始

```bash
make tidy        # 拉取依赖
make test        # 运行单测
make run         # 启动网关（默认 :8080）
curl localhost:8080/healthz   # -> {"status":"ok"}
```

配置：复制 `configs/config.example.yaml` 为 `configs/config.yaml` 按需修改；所有字段可用环境变量覆盖（前缀 `TOPOLLM_`，点替换为下划线，如 `TOPOLLM_SERVER_PORT`）。

容器化：`make docker-up`（gateway + MySQL + Redis）。

## 目录结构

```
cmd/gateway/        入口
internal/
  config/           配置加载（Viper + 环境变量覆盖）
  server/           Gin 服务、路由、优雅关闭
  middleware/       RequestID / Logger / Metrics / Recovery / CORS / Auth / RateLimit
  adaptor/          南向适配器 SPI（接口 + 注册表）
    openaicompat/   OpenAI 兼容通用适配器（+ embeddings）
    anthropic/      Claude 原生适配器
    gemini/         Gemini 原生适配器
    azure/          Azure OpenAI 适配器（+ embeddings）
  dispatch/         渠道选择、负载均衡、故障转移、熔断、运行时 CRUD
  gateway/          /v1/chat/completions 与 /v1/embeddings 编排
  billing/          定价表、三阶段配额消费、用量记录
  plugin/           请求前置插件管线（+ builtin: 敏感词/审计）
  admin/            管理 API（渠道 CRUD + 连通性测试）
  security/         SSRF 出口防护
  relay/            统一 OpenAI 兼容 schema + 透传辅助
  store/            GORM + Redis 初始化
  model/            持久化模型（UsageLog 等）
  observability/    zap 日志 + Prometheus 指标
configs/            配置示例
deployments/        Dockerfile + docker-compose
docs/research/      技术调研与实施规划
```

## 路线

完整技术调研与分阶段规划见 [`docs/research/llm-gateway-research.md`](docs/research/llm-gateway-research.md)。

## 能力

- **统一接口**：OpenAI 兼容 `/v1/chat/completions`（非流式 + SSE 流式）、`/v1/embeddings`、`/v1/images/generations`、`/v1/rerank`
- **健康探测**：渠道主动连通性探测后台任务，失败反馈熔断器
- **适配器**：`openai`（OpenAI 兼容，含 DeepSeek/通义/智谱/Kimi/豆包/Grok/自托管/中转）、`claude`（Anthropic 原生）、`gemini`（Google 原生）、`azure`（Azure OpenAI）
- **自定义供应商**：数据驱动渠道，运行时增删，不限官方
- **调度**：优先级 + 加权随机负载均衡、故障转移、被动熔断
- **鉴权计费**：API 令牌鉴权、三阶段配额消费、定价表、用量落库
- **限流**：内存令牌桶 / Redis 分布式（固定窗口）
- **插件管线**：请求前置 Hook 链（敏感词、审计），可短路、可扩展
- **管理 API**：`/admin` 渠道 CRUD + 连通性测试 + SSRF 出口防护
- **可观测**：结构化日志、Prometheus `/metrics`、优雅关闭
