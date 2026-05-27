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
  middleware/       RequestID / Logger / Recovery / CORS
  adaptor/          南向适配器 SPI（接口 + 注册表）
  relay/            统一 OpenAI 兼容 schema
  store/            GORM + Redis 初始化
  model/            持久化模型
  observability/    zap 日志
configs/            配置示例
deployments/        Dockerfile + docker-compose
docs/research/      技术调研与实施规划
```

## 路线

完整技术调研与分阶段规划见 [`docs/research/llm-gateway-research.md`](docs/research/llm-gateway-research.md)。

当前进度：

- ✅ Phase 0 脚手架
- ✅ Phase 1 OpenAI 兼容 `/v1/chat/completions`（非流式 + SSE 流式）
- ✅ Phase 2 适配器 SPI + 通用 OpenAI 适配器（数据驱动渠道、自定义供应商）
- ✅ Phase 3 Claude 原生适配器（OpenAI ↔ Anthropic Messages 互转）
- ✅ Phase 4 多渠道负载均衡 + 故障转移
- 🚧 Phase 5 鉴权（令牌）已做；配额/计费待办
- 🚧 Phase 6 内存限流已做；Redis 分布式限流 / Prometheus / 管理 API 待办
