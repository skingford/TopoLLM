# TopoLLM 大模型聚合网关 · 技术调研与实施规划

> 状态：调研定稿 / 待进入编码授权
> 日期：2026-05-27
> 定位：企业级、自研（Go）、网关层为主、OpenAI 兼容统一对外、全覆盖国内外主流厂商

---

## 1. 需求与边界

构建一个**企业级、自研、Go 实现的大模型调用聚合网关**，核心定位在**网关层**。

- **对外**：统一暴露 **OpenAI 兼容协议**（`/v1/chat/completions`、`/v1/embeddings` 等），下游应用零改造接入。
- **对内**：适配国内外主流厂商。
  - 国内：DeepSeek / 通义千问 / 智谱 GLM / Kimi / 豆包 / 百川 / 讯飞星火 / 混元 / 文心。
  - 国外：OpenAI / Anthropic Claude / Google Gemini / Azure（AWS Bedrock 后期可选）。
- **网关核心能力**：多厂商适配、统一鉴权、限流、负载均衡、故障转移、用量统计。
- **可扩展性基线**：适配器 SPI（南向）+ 插件管线（横切），新增厂商/能力**不动核心代码**。
- **自定义供应商**：供应商**不限官方厂商**——支持运行时配置任意 OpenAI 兼容 / 自托管 / 第三方中转端点；预置厂商仅为模板。

**非目标（本阶段不做）**：多租户 SaaS 运营后台、计费账单出账系统、RAG/知识库、Agent 编排（属平台层/应用层，后续另立项）。

---

## 2. 厂商协议兼容性矩阵（调研结论）

> **关键洞察**：截至 2026 年，**绝大多数厂商已原生提供 OpenAI 兼容端点**，「反向代理 + 轻量参数映射」即可覆盖多数厂商；仅少数协议差异大的需专用适配器。这极大降低自研工作量。

| 厂商 | OpenAI 兼容 | 接入策略 | 注意点 |
|------|:----------:|---------|--------|
| DeepSeek | ✅ 原生 | 轻适配（首发打通） | `reasoning_content` 字段、价格分时 |
| 通义千问 Qwen | ✅ compatible-mode | 轻适配 | DashScope base_url，多模态走专端点 |
| 智谱 GLM | ✅ 基本兼容 | 轻适配 | tool_calls 行为细节差异 |
| Kimi / Moonshot | ✅ 原生 | 轻适配 | RPM 限制偏严 |
| 字节豆包 / 火山方舟 | ✅ 兼容 | 轻适配 | 用 endpoint_id 而非 model 名 |
| 百川 / 讯飞星火 / 混元 / 文心 | ✅ 新版兼容 | 轻适配 | 老协议端点逐步弃用 |
| OpenAI | ✅ 原生 | 直通 | 基准协议 |
| Azure OpenAI | ⚠️ 近似 | 中度适配 | `deployment` + `api-version`，URL 结构不同 |
| **Anthropic Claude** | ⚠️ 有兼容层但不建议生产 | **专用适配器** | 原生 Messages：`x-api-key` 头、`system` 独立字段、content blocks、stop_reason 映射 |
| **Google Gemini** | ✅ 有兼容端点 | 轻适配 / 可选专用 | 兼容端点功能受限，多模态/部分特性需原生适配器 |
| AWS Bedrock（可选） | ❌ | 专用适配器 | SigV4 签名，工作量大，列为后期 |

**结论**：采用**两档适配策略** —— ① OpenAI 兼容厂商走「通用透传适配器 + 配置化 Profile」；② Claude 原生 / Gemini 原生 / Azure / Bedrock 走「专用适配器」。

---

## 3. 自研 vs 借鉴：参考架构

路线为**完全自研**，但成熟开源网关的架构作为蓝本以规避重复踩坑：

- **One API（songquanpeng/one-api，Go）**：单二进制、适配器模式、key 管理与二次分发的经典实现。
- **New API（QuantumNous/new-api，Go）**：在 One API 基础上做了 OpenAI/Claude/Gemini **多协议互转**、加权随机分发 + 自动故障重试、三阶段配额计费、渠道健康自动测试——与本项目目标高度重合，作为**首选参考蓝本**。

New API 的端到端流程（借鉴，自研实现）：

```
客户端 → [中间件: RequestID/鉴权] → [Token鉴权] → [Quota预消费]
       → [Channel选择: 优先级/权重/健康] → [格式转换 Adaptor]
       → [转发上游 + 流式处理] → [消费结算/退款] → [响应转换返回] → [用量日志]
```

借鉴其分层与流程，**自研实现**以避开 AGPL/历史包袱，按 TopoLLM 需求重新设计模块边界。

---

## 4. 推荐架构分层（自研 Go）

```
┌─────────────────────────────────────────────────────┐
│ 接入层  Gin Router + 中间件                            │
│  RequestID / CORS / I18n / 鉴权 / 限流 / Recover      │
├─────────────────────────────────────────────────────┤
│ 鉴权与配额  API Key(sk-)管理 · Token鉴权 · 配额预扣    │
├─────────────────────────────────────────────────────┤
│ 路由调度层  模型→渠道映射 · 加权/优先级负载均衡          │
│            健康检查 · 故障转移 · 重试 · 熔断             │
├─────────────────────────────────────────────────────┤
│ 插件管线  Hook 链（横切）：审核/缓存/改写/审计…          │
├─────────────────────────────────────────────────────┤
│ 适配器层 Adaptor SPI（南向）                           │
│  ┌─ GenericOpenAIAdaptor(Profile驱动)  ┌─ Claude专用 │
│  └─ Gemini专用  └─ Azure专用           └─ ...         │
│  职责: Request/Response/Stream 三向 ↔ 统一OpenAI schema│
├─────────────────────────────────────────────────────┤
│ 上游调用层  HTTP Client · 超时/重试 · SSE流透传         │
├─────────────────────────────────────────────────────┤
│ 计费/用量  三阶段消费(预扣→执行→结算/退款) · 价格表       │
│           token计量 · 用量日志(独立日志库)              │
├─────────────────────────────────────────────────────┤
│ 数据层  GORM(MySQL/PG) · Redis(缓存/限流/分布式锁)      │
├─────────────────────────────────────────────────────┤
│ 后台任务  渠道健康自动测试 · 配额重置 · 缓存同步          │
├─────────────────────────────────────────────────────┤
│ 可观测  结构化日志(zap) · Prometheus指标 · OTel链路追踪  │
└─────────────────────────────────────────────────────┘
```

模块按领域拆分（高内聚低耦合，多个小文件）：
`router / middleware / auth / dispatch(channel) / plugin / relay / adaptor/<vendor> / billing / model(repo) / task / observability`。

---

## 5. 技术选型（Go 生态）

| 关注点 | 选型 | 理由 |
|--------|------|------|
| HTTP 框架 | **Gin** | 生态成熟、中间件丰富、参考项目同栈 |
| 流式转发 | `net/http` + `bufio` SSE 透传 | 精确控制 chunk 与 usage 注入 |
| OpenAI 类型 | `sashabaranov/go-openai`（类型参考） | 复用请求/响应结构体 |
| ORM / DB | **GORM** + MySQL/PostgreSQL | 渠道/密钥/用量持久化 |
| 缓存/限流/锁 | **go-redis** + 令牌桶（redis_rate） | 分布式一致性 |
| 配置 | **Viper** | 多源配置、热加载 |
| 日志 | **zap** 或标准库 `slog` | 高性能结构化日志 |
| 可观测 | **Prometheus + OpenTelemetry** | 指标 + 链路 |
| 部署 | **Docker 单二进制** | 一键部署，对齐参考项目 |

---

## 6. 可扩展性设计：适配器 SPI + 插件管线

两套互补但职责不同的扩展机制：

| 系统 | 方向 | 扩展什么 | 加一个新东西要做什么 |
|------|------|---------|---------------------|
| **适配器 SPI** | 南向（对接上游厂商） | 协议翻译（OpenAI ↔ 厂商） | 实现 `Adaptor` 接口 + `init()` 注册 / 或加一份厂商 Profile |
| **插件管线** | 横切（请求生命周期） | 审核/缓存/改写/审计等横切逻辑 | 实现某个 Hook 接口 + 配置启用 + 排序 |

### 6.1 插件机制选型（Go 特有决策）

Go 无好用的动态插件（`plugin` 包版本锁死、不支持 Windows、不能卸载），机制选型是关键：

| 方案 | 隔离/热插拔 | 性能 | 复杂度 | 适用 |
|------|:----------:|:----:|:------:|------|
| **编译期注册表（Caddy 式）** ✅推荐 | 无热插拔 | 最高 | 低 | 一二方插件、类型安全、最贴合 Go |
| Go `plugin`(.so) | 半热插拔 | 高 | 中(脆弱) | ❌ 不建议生产 |
| **WASM / Proxy-Wasm**（Higress 式） | 强隔离+热插拔 | 中 | 高 | 三方/不可信插件，**后期可选** |
| go-plugin(gRPC) | 进程隔离 | 低(IPC) | 中 | 跨语言外部插件，按需 |
| Lua/CEL 脚本（APISIX 式） | 配置级 | 中 | 低 | 轻量规则，可作为脚本插件补充 |

**决策**：**编译期注册表为核心**（Caddy/Traefik 模型，惯用、类型安全、零 IPC 开销）；若后续需**三方/不可信/热插拔**插件，再叠加 **WASM** 层（YAGNI，不提前造）。

### 6.2 适配器 SPI（南向）

```go
// 统一接口 —— 新厂商只需实现它并自注册
type Adaptor interface {
    Name() string
    Capabilities() Capabilities          // streaming/tools/vision/embeddings/reasoning
    ConvertRequest(ctx, *ChatRequest, *Channel) (*http.Request, error)
    ParseResponse(ctx, *http.Response) (*ChatResponse, *Usage, error)
    ParseStream(ctx, *http.Response) (StreamReader, error) // 产出统一 chunk
}

// 注册表（开闭原则核心）
func Register(name string, factory func() Adaptor)
func Get(name string) (Adaptor, bool)

// 厂商包内自注册，无需改动核心
func init() { adaptor.Register("deepseek", func() Adaptor { return &DeepSeek{} }) }
```

**两档落地**：

- **OpenAI 兼容厂商** → 共用一个 `GenericOpenAIAdaptor`，由 **`VendorProfile`（base_url / 鉴权头 / 字段映射 / quirks 开关）** 参数化。**加一个兼容厂商 = 加一份配置 Profile，零代码**；且 Profile 可由**用户运行时定义**（即自定义供应商，见 6.4），预置厂商只是内置模板。
- **协议差异厂商**（Claude 原生 / Gemini 原生 / Azure / Bedrock）→ 各写一个专用 `Adaptor` 实现。

`Capabilities()` 让调度层做**能力协商**（如请求带 vision 时自动过滤不支持的渠道）。

### 6.3 插件管线（横切）

按请求生命周期定义**有序 Hook 点**，插件按接口隔离原则**只实现关心的 Hook**，并可**短路返回**（如审核拦截、缓存命中直接出结果）：

```
解析 →[OnRequest]→ 鉴权 →[OnAuthenticated]→ 配额预扣
   →[BeforeDispatch](语义缓存查找 ⏹短路) → 选渠道 → Adaptor转换
   →[BeforeUpstream](输入审核/敏感词/Prompt注入 ⏹短路) → 上游调用
   →[OnStreamChunk](流式输出审核/PII脱敏) → 聚合
   →[AfterUpstream](输出审核/响应改写/写缓存) → 计费结算
   →[OnComplete](审计/指标) ／ [OnError](错误处理)
```

```go
type Plugin interface {
    Name() string
    Priority() int                 // 决定链内顺序
}
// 接口隔离：只实现需要的 Hook
type RequestFilter   interface { OnRequest(c *PluginCtx) (Decision, error) }
type UpstreamFilter  interface { BeforeUpstream(c *PluginCtx) (Decision, error) }
type StreamFilter    interface { OnStreamChunk(c *PluginCtx, chunk *Chunk) (*Chunk, error) }
type ResponseFilter  interface { AfterUpstream(c *PluginCtx) (Decision, error) }

// Decision = Continue | ShortCircuit(resp) | Reject(err)
```

**内建插件清单**（按需实现）：内容审核/Guardrails（输入+输出）、敏感词/PII 脱敏、**语义缓存**、Prompt 模板注入、模型别名/灰度 A/B 路由、自定义鉴权、请求/响应改写、审计日志、Mock/故障注入（测试用）。

**两条工程约束**：

- **失败策略可配**：每个插件声明 `fail-open`（出错跳过）或 `fail-closed`（出错拒绝），审核类默认 fail-closed。
- **不可变原则的务实平衡**：请求级改写**返回新对象**而非原地修改；流式 `OnStreamChunk` 对 chunk 副本操作，避免热路径上每帧深拷贝的开销——此取舍为有意为之。

### 6.4 自定义供应商（不限官方）

**核心区分**：

- **Adaptor（协议层 · 代码）**：`openai` / `anthropic` / `gemini` / `azure` / `custom-template`，封装「如何说某种协议」。数量少、稳定。
- **Provider / Channel（实例 · 数据）**：= adaptor 类型 + base_url + 密钥 + 模型映射 + quirks。**由用户在配置/管理后台定义，运行时增删改、无需改代码或重启。**

> 因此「自定义供应商」= 新建一个 Channel，选 `openai` 协议，填自己的 base_url / key / 模型列表即可。**预置厂商（DeepSeek/通义/…）只是预设模板（preset）**，与自定义供应商走**同一条数据驱动路径**，只是替用户省去填 base_url 的步骤。

**Provider 配置（数据模型草案，合成示意）**：

```yaml
- name: my-vllm
  adaptor: openai                 # 协议类型（内置 adaptor）
  base_url: http://10.0.0.5:8000/v1
  auth: { scheme: bearer, header: Authorization, key_ref: secret://vllm-key }
  models:
    - alias: qwen-local           # 对外模型名
      upstream: Qwen2.5-72B        # 上游真实模型 id
      capabilities: [chat, stream, tools]
      pricing: { input: 0, output: 0 }
  quirks: { stream_usage: false }  # 该端点流式不返回 usage
  weight: 10
  priority: 1
```

**覆盖场景**：

- **自托管 / 私有部署**：vLLM、Ollama、LM Studio、Xinference、LocalAI、TGI、SGLang（均 OpenAI 兼容）。
- **第三方聚合 / 中转**：OpenRouter、硅基流动等中转站。
- **企业私有网关 / 代理**。
- **非标准协议**：走 `custom-template` 声明式映射适配器（模板/JSONPath/表达式描述 request↔response），或写专用 Go adaptor / 插件。

**声明式 `custom-template` 适配器（可选，YAGNI）**：先做 OpenAI 兼容自定义供应商（覆盖约 95%）；非标准协议再引入模板映射，避免为每个怪异接口写代码。

**运维**：Provider 支持运行时 CRUD + **「测试连通」**后再启用（接健康检查与能力探测）。

---

## 7. 核心能力设计要点

1. **负载均衡 + 故障转移**：模型可绑定多渠道，按「优先级分组 + 组内加权随机」选择；上游 5xx/超时触发**重试到下一渠道**，配合**熔断**剔除不健康渠道。
2. **鉴权与配额**：对外发放 `sk-` 令牌，绑定用户/分组/模型白名单；**三阶段消费**保证故障转移不重复扣费（预扣 → 实际 usage 结算 → 失败退款）。
3. **限流**：基于 Redis 的多维令牌桶（按 key / 按用户 / 按模型 RPM+TPM）。
4. **用量与计费**：维护**价格表**（每厂商每模型 input/output 单价）；流式对不返回 usage 的厂商用 `include_usage` 或 tokenizer 估算；用量日志写**独立日志库**隔离高频写。
5. **协议归一化**：统一 `finish_reason` / `tool_calls` / `reasoning_content` / 错误码（OpenAI error schema）；流式统一 SSE `data:` 分帧与 `[DONE]`。
6. **可观测**：每请求记录 RequestID、上游渠道、延迟、token、状态码；暴露 Prometheus 指标（QPS、P99、渠道成功率、配额）。

---

## 8. 风险与对策

| 级别 | 风险 | 对策 |
|:----:|------|------|
| **HIGH** | OpenAI「兼容」程度参差（tool_calls / 多模态 / reasoning / usage 不一致） | 适配器分级 + 兼容性测试矩阵，按厂商打补丁映射 |
| **HIGH** | 流式 SSE 下 usage 缺失导致计费不准 | 强制 `include_usage`；缺失时本地 tokenizer 估算并标记 |
| **HIGH** | 故障转移引发重复计费/重复请求 | 三阶段消费 + 幂等 RequestID + 仅对未消费请求重试 |
| **MED** | 密钥泄露 | 上游密钥**加密存储**（KMS/AES）、最小权限、轮换机制 |
| **MED** | 自定义供应商 base_url 由用户填写 → **SSRF / 内网探测** | 出口域名 allowlist / 解析后禁止内网与保留地址（可配白名单例外）/ 独立出口代理 |
| **MED** | 高并发长连接（流式）内存/超时 | context 超时控制、连接数限制、背压 |
| **MED** | 各厂商 tokenizer/价格表维护成本 | 价格表配置化 + 定期同步任务 |
| **MED** | 错误码不归一导致下游难处理 | 统一映射到 OpenAI error schema + 内部错误码 |
| **LOW** | 厂商端点/协议迭代弃用 | 渠道健康自动测试 + 版本化适配器 |

---

## 9. 复杂度评估

**整体：HIGH**（多厂商协议归一 + 计费 + 高并发流式是主要难点）。
**MVP（Phase 0–1）为 MEDIUM**，可快速跑通端到端验证可行性。

---

## 10. 分阶段实施规划

| 阶段 | 目标 | 关键产出 | 粗估 |
|:----:|------|---------|:----:|
| **Phase 0** | 脚手架与基础设施 | go module、Gin、Viper 配置、zap 日志、GORM、Redis、Docker、目录骨架 | 1–2 天 |
| **Phase 1** | 端到端打通（MVP） | OpenAI 兼容接入层 + `Adaptor` 接口骨架（走注册表）+ **DeepSeek** 单厂商 `chat/completions`（非流式 + SSE 流式）跑通 | 2–3 天 |
| **Phase 2** | 适配器 SPI + 数据驱动 Provider | 注册表 + `GenericOpenAIAdaptor`（Profile 驱动）；**Provider/Channel 数据驱动**，预置厂商为模板，**支持运行时自定义供应商**（任意 OpenAI 兼容/自托管/中转端点）；接入通义/智谱/Kimi/豆包/百川/讯飞/混元 | 3–5 天 |
| **Phase 2.5** | **插件管线基础设施** | Hook 点、`Plugin` 接口、链编排、短路/失败策略、配置加载；内置示范插件（审计日志 + 敏感词） | 2–3 天 |
| **Phase 3** | 协议差异厂商 | **Claude 原生 Messages** + **Gemini 原生** + Azure 专用适配器 | 3–4 天 |
| **Phase 4** | 渠道调度 | 渠道管理、加权/优先级负载均衡、健康检查、故障转移、重试、熔断 | 3–4 天 |
| **Phase 5** | 鉴权与计费 | API Key 管理、Token 鉴权、三阶段消费、价格表、用量日志 | 4–5 天 |
| **Phase 6** | 限流与可观测 | Redis 多维限流（重构为插件）、Prometheus 指标、OTel 链路、**管理 API（Provider/Channel CRUD + 测试连通 + SSRF 出口管控）** | 3–4 天 |
| **Phase 7** | 扩展端点与加固 | embeddings / rerank / images / 多模态，**可选 `custom-template` 声明式适配器**，压测、安全加固、文档 | 4–5 天 |
| **Phase 8**（可选） | WASM 插件运行时 | 仅当需要三方/热插拔插件时启动 | — |

> **测试贯穿各阶段**：每个适配器配兼容性用例，核心逻辑（调度、计费、限流、插件链）单测覆盖 ≥ 80%（遵循 TDD 规范）。

---

## 11. 参考资料

- [songquanpeng/one-api（Go LLM 网关）](https://github.com/songquanpeng/one-api)
- [QuantumNous/new-api（多协议互转网关）](https://github.com/QuantumNous/new-api) · [DeepWiki 架构](https://deepwiki.com/QuantumNous/new-api)
- [Claude OpenAI SDK 兼容层](https://platform.claude.com/docs/en/api/openai-sdk)
- [Gemini OpenAI 兼容端点（Open WebUI 文档）](https://docs.openwebui.com/getting-started/quick-start/connect-a-provider/starting-with-openai-compatible/)
- [国内模型提供商选型指南](https://datawhalechina.github.io/hello-claw/cn/appendix/appendix-e.html)

---

## 12. 下一步

- 本文档为**调研定稿**，范围为「调研 + 实施规划」，**未写生产代码**。
- 进入 **Phase 0 编码**需单独授权。
- 可选补充：`docs/architecture/` 架构设计文档（接口定义、数据模型、流程时序图）。
