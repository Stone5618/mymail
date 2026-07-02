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
	MaxAttachmentSize int64  `mapstructure:"MAX_ATTACHMENT_SIZE"`

	// 管理员
	AdminUsername string `mapstructure:"ADMIN_USERNAME"`
	AdminPassword string `mapstructure:"ADMIN_PASSWORD"`
	AdminEmail    string `mapstructure:"ADMIN_EMAIL"`

	// SMTP
	SMTPPort                int    `mapstructure:"SMTP_PORT"`
	SMTPSendHost            string `mapstructure:"SMTP_SEND_HOST"`
	SMTPSendPort            int    `mapstructure:"SMTP_SEND_PORT"`
	SMTPTLSRejectUnauthorized bool `mapstructure:"SMTP_TLS_REJECT_UNAUTHORIZED"`
	SMTPTLSCert             string `mapstructure:"SMTP_TLS_CERT"`
	SMTPTLSKey              string `mapstructure:"SMTP_TLS_KEY"`

	// 灰名单
	GreylistDelayMs int `mapstructure:"GREYLIST_DELAY_MS"`
	GreylistTtlMs   int `mapstructure:"GREYLIST_TTL_MS"`

	// 限流
	RateLimitMax int `mapstructure:"RATE_LIMIT_MAX"`
	SendRateLimitPerMin int `mapstructure:"SEND_RATE_LIMIT_PER_MIN"`

	// 反垃圾
	SpamThreshold          int `mapstructure:"SPAM_THRESHOLD"`
	SpamSuspiciousThreshold int `mapstructure:"SPAM_SUSPICIOUS_THRESHOLD"`

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
	v.SetDefault("MAX_ATTACHMENT_SIZE", 26214400) // 25MB

	// 管理员
	v.SetDefault("ADMIN_USERNAME", "admin")

	// SMTP
	v.SetDefault("SMTP_PORT", 25)
	v.SetDefault("SMTP_SEND_PORT", 587)
	v.SetDefault("SMTP_TLS_REJECT_UNAUTHORIZED", true)

	// 灰名单
	v.SetDefault("GREYLIST_DELAY_MS", 300000)   // 5 分钟
	v.SetDefault("GREYLIST_TTL_MS", 3600000)    // 1 小时

	// 限流
	v.SetDefault("RATE_LIMIT_MAX", 100)
	v.SetDefault("SEND_RATE_LIMIT_PER_MIN", 10)

	// 反垃圾
	v.SetDefault("SPAM_THRESHOLD", 10)
	v.SetDefault("SPAM_SUSPICIOUS_THRESHOLD", 5)

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
