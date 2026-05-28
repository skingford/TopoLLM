package config

import (
	"fmt"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Config 是网关的全局配置。
type Config struct {
	Server         ServerConfig         `mapstructure:"server"`
	Log            LogConfig            `mapstructure:"log"`
	Database       DatabaseConfig       `mapstructure:"database"`
	Redis          RedisConfig          `mapstructure:"redis"`
	Auth           AuthConfig           `mapstructure:"auth"`
	RateLimit      RateLimitConfig      `mapstructure:"rate_limit"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	HealthCheck      HealthCheckConfig      `mapstructure:"health_check"`
	QuotaReset       QuotaResetConfig       `mapstructure:"quota_reset"`
	Billing          BillingConfig          `mapstructure:"billing"`
	Admin            AdminConfig            `mapstructure:"admin"`
	OutputModeration OutputModerationConfig `mapstructure:"output_moderation"`
	Plugins          []PluginConfig         `mapstructure:"plugins"`
	Channels         []ChannelConfig        `mapstructure:"channels"`
}

// OutputModerationConfig 控制输出内容审核（敏感词）。
type OutputModerationConfig struct {
	Enabled bool     `mapstructure:"enabled"`
	Words   []string `mapstructure:"words"`
}

// ServerConfig 控制 HTTP 服务行为。
type ServerConfig struct {
	Port            int    `mapstructure:"port"`
	Mode            string `mapstructure:"mode"`             // debug | release
	ShutdownTimeout int    `mapstructure:"shutdown_timeout"` // 秒
}

// LogConfig 控制日志输出。
type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug|info|warn|error
	Format string `mapstructure:"format"` // json|console
}

// DatabaseConfig 控制持久化数据库连接。
type DatabaseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	DSN     string `mapstructure:"dsn"`
}

// RedisConfig 控制 Redis 连接。
type RedisConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// AuthConfig 控制对外 API 令牌鉴权。
type AuthConfig struct {
	Enabled bool     `mapstructure:"enabled"`
	Tokens  []string `mapstructure:"tokens"` // 允许的对外 sk- 令牌
}

// RateLimitConfig 控制每令牌/IP 的请求限流。
type RateLimitConfig struct {
	Enabled bool `mapstructure:"enabled"`
	RPM     int  `mapstructure:"rpm"` // 每分钟请求数
}

// CircuitBreakerConfig 控制渠道熔断。
type CircuitBreakerConfig struct {
	Threshold       int `mapstructure:"threshold"`        // 触发熔断的连续失败数
	CooldownSeconds int `mapstructure:"cooldown_seconds"` // 熔断冷却时长（秒）
}

// HealthCheckConfig 控制渠道主动健康探测后台任务。
type HealthCheckConfig struct {
	Enabled         bool `mapstructure:"enabled"`
	IntervalSeconds int  `mapstructure:"interval_seconds"`
}

// QuotaResetConfig 控制配额定时重置任务（周期性把余额刷回 billing.quotas 配置的初始额度）。
type QuotaResetConfig struct {
	Enabled         bool `mapstructure:"enabled"`
	IntervalSeconds int  `mapstructure:"interval_seconds"`
}

// Price 是某模型的单价（每百万 token）。
type Price struct {
	Input  float64 `mapstructure:"input"`
	Output float64 `mapstructure:"output"`
}

// BillingConfig 控制计费与配额。Enabled=false 时仅记录用量、不强制配额。
type BillingConfig struct {
	Enabled bool               `mapstructure:"enabled"`
	Pricing map[string]Price   `mapstructure:"pricing"` // model -> 单价；"default" 为兜底
	Quotas  map[string]float64 `mapstructure:"quotas"`  // token -> 总额度
}

// AdminConfig 控制运维管理 API（渠道运行时 CRUD）。
type AdminConfig struct {
	Enabled               bool     `mapstructure:"enabled"`
	Token                 string   `mapstructure:"token"`                   // 管理接口 Bearer 令牌
	AllowPrivateUpstreams bool     `mapstructure:"allow_private_upstreams"` // 允许内网上游（自托管场景）
	AllowedUpstreamHosts  []string `mapstructure:"allowed_upstream_hosts"`  // SSRF 白名单主机
}

// PluginConfig 描述一个请求前置插件的启用与配置。
type PluginConfig struct {
	Name     string         `mapstructure:"name"`
	Enabled  bool           `mapstructure:"enabled"`
	Priority int            `mapstructure:"priority"` // 越大越先执行
	Options  map[string]any `mapstructure:"options"`
}

// ChannelConfig 描述一个上游供应商实例（数据驱动，支持自定义供应商）。
type ChannelConfig struct {
	Name     string   `mapstructure:"name" json:"name"`
	Adaptor  string   `mapstructure:"adaptor" json:"adaptor"` // openai | claude | gemini | ...
	BaseURL  string   `mapstructure:"base_url" json:"base_url"`
	APIKey   string   `mapstructure:"api_key" json:"api_key"`
	Models   []string `mapstructure:"models" json:"models"` // 该渠道对外暴露的模型名
	Weight   int      `mapstructure:"weight" json:"weight"`
	Priority int      `mapstructure:"priority" json:"priority"`
	Enabled  bool     `mapstructure:"enabled" json:"enabled"`
	// Extra 承载 adaptor 专属参数（如 azure 的 api_version / deployment）。
	Extra map[string]string `mapstructure:"extra" json:"extra,omitempty"`
}

// Load 从给定路径加载配置，并以环境变量覆盖（前缀 TOPOLLM_，点替换为下划线）。
// path 为空时仅使用默认值与环境变量。
func Load(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
	}

	v.SetEnvPrefix("TOPOLLM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.WeaklyTypedInput = true // 允许环境变量字符串转为 int/bool
	}); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "release")
	v.SetDefault("server.shutdown_timeout", 10)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("database.enabled", false)
	v.SetDefault("redis.enabled", false)
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("auth.enabled", false)
	v.SetDefault("rate_limit.enabled", false)
	v.SetDefault("rate_limit.rpm", 60)
	v.SetDefault("circuit_breaker.threshold", 5)
	v.SetDefault("circuit_breaker.cooldown_seconds", 30)
	v.SetDefault("health_check.interval_seconds", 60)
	v.SetDefault("quota_reset.interval_seconds", 86400)
	v.SetDefault("billing.enabled", false)
	v.SetDefault("admin.enabled", false)
}

func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port: %d", c.Server.Port)
	}
	if c.Database.Enabled && c.Database.DSN == "" {
		return fmt.Errorf("database.enabled but database.dsn is empty")
	}
	if c.Redis.Enabled && c.Redis.Addr == "" {
		return fmt.Errorf("redis.enabled but redis.addr is empty")
	}
	if c.RateLimit.Enabled && c.RateLimit.RPM <= 0 {
		return fmt.Errorf("rate_limit.enabled but rate_limit.rpm <= 0")
	}
	if c.Admin.Enabled && c.Admin.Token == "" {
		return fmt.Errorf("admin.enabled but admin.token is empty")
	}
	return nil
}
