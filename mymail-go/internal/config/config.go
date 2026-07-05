// Package config 负责应用配置的加载与校验。
// 配置来源优先级：环境变量 > .env 文件 > 默认值。
//
// P0-8 修复：JWT_SECRET 强制要求设置，未设置或为默认值时启动失败。
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 持有应用全部配置。
type Config struct {
	// 基本信息
	Env     string `mapstructure:"ENV"`
	Version string
	Debug   bool `mapstructure:"DEBUG"`

	// 构建信息（构建时通过 -ldflags 注入，非环境变量）
	BuildTime string
	CommitSHA string

	// 服务器
	Port int    `mapstructure:"PORT"`
	Host string `mapstructure:"HOST"`

	// 域名
	Domain   string `mapstructure:"DOMAIN"`
	MailHost string `mapstructure:"MAIL_HOST"`

	// JWT（P0-8：强制校验）
	JWTSecret            string `mapstructure:"JWT_SECRET"`
	JWTExpiresIn         string `mapstructure:"JWT_EXPIRES_IN"`
	JWTRememberExpiresIn string `mapstructure:"JWT_REMEMBER_EXPIRES_IN"`

	// 数据库
	DBPath string `mapstructure:"DB_PATH"`

	// 邮件存储
	MaildirPath       string `mapstructure:"MAILDIR_PATH"`
	AttachmentPath    string `mapstructure:"ATTACHMENT_PATH"`
	AvatarPath        string `mapstructure:"AVATAR_PATH"`
	MaxAttachmentSize int64  `mapstructure:"MAX_ATTACHMENT_SIZE"`
	MaxAvatarSize     int64  `mapstructure:"MAX_AVATAR_SIZE"`

	// 管理员
	AdminUsername string `mapstructure:"ADMIN_USERNAME"`
	AdminPassword string `mapstructure:"ADMIN_PASSWORD"`
	AdminEmail    string `mapstructure:"ADMIN_EMAIL"`

	// SMTP
	SMTPPort                  int    `mapstructure:"SMTP_PORT"`
	SMTPSendHost              string `mapstructure:"SMTP_SEND_HOST"`
	SMTPSendPort              int    `mapstructure:"SMTP_SEND_PORT"`
	SMTPSendUsername          string `mapstructure:"SMTP_SEND_USERNAME"`
	SMTPSendPassword          string `mapstructure:"SMTP_SEND_PASSWORD"`
	SMTPTLSRejectUnauthorized bool   `mapstructure:"SMTP_TLS_REJECT_UNAUTHORIZED"`
	SMTPTLSCert               string `mapstructure:"SMTP_TLS_CERT"`
	SMTPTLSKey                string `mapstructure:"SMTP_TLS_KEY"`
	// SMTP 连接限流（P1：连接级防护）
	SMTPMaxConnectionsPerIp int `mapstructure:"SMTP_MAX_CONNECTIONS_PER_IP"`
	SMTPRateWindowMs        int `mapstructure:"SMTP_RATE_WINDOW_MS"`

	// 灰名单
	GreylistDelayMs int `mapstructure:"GREYLIST_DELAY_MS"`
	GreylistTtlMs   int `mapstructure:"GREYLIST_TTL_MS"`

	// 限流
	RateLimitMax         int `mapstructure:"RATE_LIMIT_MAX"`
	SendRateLimitPerMin  int `mapstructure:"SEND_RATE_LIMIT_PER_MIN"`

	// 反垃圾（阶段 4 扩展）
	SpamThreshold           int      `mapstructure:"SPAM_THRESHOLD"`
	SpamSuspiciousThreshold int      `mapstructure:"SPAM_SUSPICIOUS_THRESHOLD"`
	SpfEnabled              bool     `mapstructure:"SPF_ENABLED"`       // SPF 校验开关
	SpfMaxDepth             int      `mapstructure:"SPF_MAX_DEPTH"`     // SPF include 递归深度
	DnsblEnabled            bool     `mapstructure:"DNSBL_ENABLED"`     // DNSBL 校验开关
	DnsblZones              []string `mapstructure:"DNSBL_ZONES"`       // DNSBL zone 列表
	DnsblQueryTimeoutMs     int      `mapstructure:"DNSBL_QUERY_TIMEOUT_MS"` // 单次 DNSBL 查询超时
	SpfQueryTimeoutMs       int      `mapstructure:"SPF_QUERY_TIMEOUT_MS"`   // 单次 SPF 查询超时

	// 出站 SMTP 熔断器（gobreaker）
	SMTPCircuitBreakerEnabled      bool    `mapstructure:"SMTP_CIRCUIT_BREAKER_ENABLED"`
	SMTPCircuitBreakerMaxRequests  uint32  `mapstructure:"SMTP_CIRCUIT_BREAKER_MAX_REQUESTS"`  // 半开状态最大请求数
	SMTPCircuitBreakerIntervalMs   int     `mapstructure:"SMTP_CIRCUIT_BREAKER_INTERVAL_MS"`   // closed 状态计数窗口
	SMTPCircuitBreakerTimeoutMs    int     `mapstructure:"SMTP_CIRCUIT_BREAKER_TIMEOUT_MS"`    // open 状态持续时间
	SMTPCircuitBreakerFailureRatio float64 `mapstructure:"SMTP_CIRCUIT_BREAKER_FAILURE_RATIO"` // 失败率阈值
	SMTPCircuitBreakerMinRequests  uint32  `mapstructure:"SMTP_CIRCUIT_BREAKER_MIN_REQUESTS"` // 触发熔断最小请求数

	// 出站 SMTP 重试（backoff）
	SMTPRetryEnabled          bool `mapstructure:"SMTP_RETRY_ENABLED"`
	SMTPRetryMaxAttempts      int  `mapstructure:"SMTP_RETRY_MAX_ATTEMPTS"`
	SMTPRetryInitialIntervalMs int `mapstructure:"SMTP_RETRY_INITIAL_INTERVAL_MS"`
	SMTPRetryMaxIntervalMs    int  `mapstructure:"SMTP_RETRY_MAX_INTERVAL_MS"`
	SMTPRetryMaxElapsedTimeMs int  `mapstructure:"SMTP_RETRY_MAX_ELAPSED_TIME_MS"`

	// 规则引擎（P1-7：ReDoS 防护）
	RulesMaxPatternLength int `mapstructure:"RULES_MAX_PATTERN_LENGTH"` // 正则 pattern 最大长度
	RulesCompileTimeoutMs int `mapstructure:"RULES_COMPILE_TIMEOUT_MS"` // 正则编译超时（毫秒）

	// CORS（P1-2：白名单）
	CORSAllowedOrigins []string `mapstructure:"CORS_ALLOWED_ORIGINS"`

	// 可观测性
	LogLevel           string  `mapstructure:"LOG_LEVEL"`
	LogFormat          string  `mapstructure:"LOG_FORMAT"`
	MetricsEnabled     bool    `mapstructure:"METRICS_ENABLED"`
	MetricsPath        string  `mapstructure:"METRICS_PATH"`
	TracingEnabled     bool    `mapstructure:"TRACING_ENABLED"`
	TracingEndpoint    string  `mapstructure:"TRACING_ENDPOINT"`
	TracingSampleRate  float64 `mapstructure:"TRACING_SAMPLE_RATE"`

	// 审计
	AuditEnabled       bool `mapstructure:"AUDIT_ENABLED"`
	AuditRetentionDays int  `mapstructure:"AUDIT_RETENTION_DAYS"`

	// 特性开关
	FeatureSpamFilter    bool `mapstructure:"FEATURE_SPAM_FILTER"`
	FeatureWSNotify      bool `mapstructure:"FEATURE_WS_NOTIFY"`
	FeatureGreylist      bool `mapstructure:"FEATURE_GREYLIST"`
	FeatureAPIKey        bool `mapstructure:"FEATURE_API_KEY"`
	FeatureRules         bool `mapstructure:"FEATURE_RULES"`
	FeatureAudit         bool `mapstructure:"FEATURE_AUDIT"`
	FeatureRegistration  bool `mapstructure:"FEATURE_REGISTRATION"`
}

// Load 加载配置，含 P0-8 修复（JWT_SECRET 强制校验）。
func Load() (*Config, error) {
	v := viper.New()

	// 设置默认值
	setDefaults(v)

	// 尝试读取 .env 文件（不存在不报错）
	v.SetConfigFile(".env")
	v.AutomaticEnv()
	// 环境变量不加前缀
	_ = v.ReadInConfig()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// P0-8 修复：强制校验 JWT_SECRET
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// setDefaults 设置各配置项的默认值。
func setDefaults(v *viper.Viper) {
	// 服务器
	v.SetDefault("ENV", "dev")
	v.SetDefault("DEBUG", false)
	v.SetDefault("PORT", 3000)
	v.SetDefault("HOST", "0.0.0.0")

	// JWT
	v.SetDefault("JWT_EXPIRES_IN", "24h")
	v.SetDefault("JWT_REMEMBER_EXPIRES_IN", "720h")

	// 数据库与存储
	v.SetDefault("DB_PATH", "./data/mymail.db")
	v.SetDefault("MAILDIR_PATH", "./data/maildir")
	v.SetDefault("ATTACHMENT_PATH", "./data/attachments")
	v.SetDefault("AVATAR_PATH", "./data/avatars")
	v.SetDefault("MAX_ATTACHMENT_SIZE", 26214400) // 25MB
	v.SetDefault("MAX_AVATAR_SIZE", 2097152)      // 2MB

	// 管理员
	v.SetDefault("ADMIN_USERNAME", "admin")

	// SMTP
	v.SetDefault("SMTP_PORT", 25)
	v.SetDefault("SMTP_SEND_PORT", 587)
	v.SetDefault("SMTP_TLS_REJECT_UNAUTHORIZED", true)
	v.SetDefault("SMTP_MAX_CONNECTIONS_PER_IP", 10)
	v.SetDefault("SMTP_RATE_WINDOW_MS", 60000) // 1 分钟窗口

	// 灰名单
	v.SetDefault("GREYLIST_DELAY_MS", 300000)   // 5 分钟
	v.SetDefault("GREYLIST_TTL_MS", 3600000)    // 1 小时

	// 限流
	v.SetDefault("RATE_LIMIT_MAX", 100)
	v.SetDefault("SEND_RATE_LIMIT_PER_MIN", 10)

	// 反垃圾（阶段 4 扩展）
	v.SetDefault("SPAM_THRESHOLD", 10)
	v.SetDefault("SPAM_SUSPICIOUS_THRESHOLD", 5)
	v.SetDefault("SPF_ENABLED", true)
	v.SetDefault("SPF_MAX_DEPTH", 10) // RFC 7208 建议 include 递归上限 10
	v.SetDefault("DNSBL_ENABLED", true)
	v.SetDefault("DNSBL_ZONES", []string{
		"zen.spamhaus.org",
		"bl.spamcop.net",
		"b.barracudacentral.org",
	})
	v.SetDefault("DNSBL_QUERY_TIMEOUT_MS", 3000)
	v.SetDefault("SPF_QUERY_TIMEOUT_MS", 3000)

	// 出站 SMTP 熔断器
	v.SetDefault("SMTP_CIRCUIT_BREAKER_ENABLED", true)
	v.SetDefault("SMTP_CIRCUIT_BREAKER_MAX_REQUESTS", uint32(5))   // 半开状态最多 5 个请求
	v.SetDefault("SMTP_CIRCUIT_BREAKER_INTERVAL_MS", 60000)        // closed 状态 60 秒计数窗口
	v.SetDefault("SMTP_CIRCUIT_BREAKER_TIMEOUT_MS", 30000)         // open 状态 30 秒后半开
	v.SetDefault("SMTP_CIRCUIT_BREAKER_FAILURE_RATIO", 0.6)        // 失败率 > 60% 触发熔断
	v.SetDefault("SMTP_CIRCUIT_BREAKER_MIN_REQUESTS", uint32(10))  // 至少 10 个请求才计算

	// 出站 SMTP 重试
	v.SetDefault("SMTP_RETRY_ENABLED", true)
	v.SetDefault("SMTP_RETRY_MAX_ATTEMPTS", 3)
	v.SetDefault("SMTP_RETRY_INITIAL_INTERVAL_MS", 500)
	v.SetDefault("SMTP_RETRY_MAX_INTERVAL_MS", 10000)
	v.SetDefault("SMTP_RETRY_MAX_ELAPSED_TIME_MS", 60000)

	// 规则引擎（P1-7：ReDoS 防护）
	v.SetDefault("RULES_MAX_PATTERN_LENGTH", 500)    // 正则 pattern 最多 500 字符
	v.SetDefault("RULES_COMPILE_TIMEOUT_MS", 2000)   // 编译超时 2 秒

	// 可观测性
	v.SetDefault("LOG_LEVEL", "info")
	v.SetDefault("LOG_FORMAT", "json")
	v.SetDefault("METRICS_ENABLED", true)
	v.SetDefault("METRICS_PATH", "/metrics")
	v.SetDefault("TRACING_ENABLED", false)
	v.SetDefault("TRACING_SAMPLE_RATE", 0.1)

	// 审计
	v.SetDefault("AUDIT_ENABLED", true)
	v.SetDefault("AUDIT_RETENTION_DAYS", 365)

	// 特性开关（默认全部开启）
	v.SetDefault("FEATURE_SPAM_FILTER", true)
	v.SetDefault("FEATURE_WS_NOTIFY", true)
	v.SetDefault("FEATURE_GREYLIST", true)
	v.SetDefault("FEATURE_API_KEY", true)
	v.SetDefault("FEATURE_RULES", true)
	v.SetDefault("FEATURE_AUDIT", true)
	v.SetDefault("FEATURE_REGISTRATION", true)
}

// Validate 校验配置合法性。
// P0-8 修复：JWT_SECRET 必须设置且不能为默认值，长度至少 32 字符。
func (c *Config) Validate() error {
	// P0-8：JWT_SECRET 强制校验
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET 必须设置，请用 `openssl rand -hex 32` 生成")
	}
	// 拒绝默认占位符
	lowerSecret := strings.ToLower(c.JWTSecret)
	if lowerSecret == "change-me-in-production" ||
		lowerSecret == "change_me" ||
		lowerSecret == "changeme" ||
		strings.Contains(lowerSecret, "change-me") {
		return fmt.Errorf("JWT_SECRET 不能使用默认占位符，请生成随机字符串")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET 长度必须 >= 32 字符（当前 %d）", len(c.JWTSecret))
	}

	// 域名必填
	if c.Domain == "" {
		return fmt.Errorf("DOMAIN 必须设置")
	}

	// 生产环境额外校验
	if c.Env == "prod" {
		if c.Debug {
			return fmt.Errorf("生产环境不能开启 DEBUG")
		}
		if len(c.CORSAllowedOrigins) == 0 {
			return fmt.Errorf("生产环境必须配置 CORS_ALLOWED_ORIGINS")
		}
		if !c.MetricsEnabled {
			return fmt.Errorf("生产环境必须开启 METRICS_ENABLED")
		}
	}

	return nil
}
