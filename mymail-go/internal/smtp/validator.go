// validator.go 实现 SMTP 邮件验证器。
//
// 职责：
//   - 调用 spam.Filter.Evaluate 进行反垃圾评估（SPF + DNSBL + 评分）
//   - 将评估结果持久化到 spam_log 表（修复原 Node.js 未落库缺陷）
//   - 返回验证结果供 handler.go 在 DATA 阶段决定投递/拒绝/标记
//
// 修复的缺陷：
//   - 原 Node.js spam-log-dao.js 是死代码，spam 评分只输出到日志未落库
//   - Go 版补上落库链路，支持管理员审计与监控
//
// 失败降级（验收标准 6）：
//   - spam.Filter 内部 DNS 查询失败时返回 allow（评分可能为 2 但不 reject）
//   - spam_log 写入失败不阻断投递（仅记录 warn 日志）
//   - validator 整体永不返回错误，始终返回 ValidationResult
package smtp

import (
	"context"
	"log/slog"

	"github.com/mymail/mymail-go/internal/spam"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// MessageValidator SMTP 邮件验证器。
//
// 整合 spam.Filter（反垃圾评估）与 SpamLogDAO（结果落库）。
type MessageValidator struct {
	filter     *spam.Filter
	spamLogDAO *dao.SpamLogDAO
	enabled    bool // 反垃圾总开关
}

// NewMessageValidator 创建邮件验证器。
//
// 参数：
//   - filter: 反垃圾过滤器（传 nil 则跳过评估，直接放行）
//   - spamLogDAO: spam_log DAO（传 nil 则不落库）
//   - enabled: 反垃圾总开关（false 时跳过评估）
func NewMessageValidator(filter *spam.Filter, spamLogDAO *dao.SpamLogDAO, enabled bool) *MessageValidator {
	return &MessageValidator{
		filter:     filter,
		spamLogDAO: spamLogDAO,
		enabled:    enabled,
	}
}

// ValidationResult 验证结果。
type ValidationResult struct {
	Verdict      spam.SpamVerdict // 反垃圾判决
	SpamScore    int              // 评分（冗余字段，方便调用方）
	SpamReasons  string           // 格式化后的原因字符串（用于持久化到 messages.spam_reasons）
	ShouldReject bool             // 是否应拒绝投递
	ShouldMark   bool             // 是否应标记可疑（投递到 JUNK）
}

// ValidateInput 验证输入参数。
type ValidateInput struct {
	IP            string // 发件人 IP
	Sender        string // 发件人邮箱（完整地址，如 user@example.com）
	SenderDomain  string // 发件人邮箱域名（如 example.com）
	HELODomain    string // HELO/EHLO 域名
	Recipient     string // 收件人邮箱（完整地址）
}

// Validate 执行反垃圾验证。
//
// 流程：
//  1. 若 enabled=false 或 filter=nil，返回 allow（评分 0）
//  2. 调用 spam.Filter.Evaluate（含 SPF + DNSBL + 评分，内部失败降级）
//  3. 异步记录 spam_log（失败不阻断）
//  4. 返回 ValidationResult（含 ShouldReject/ShouldMark）
func (v *MessageValidator) Validate(ctx context.Context, in ValidateInput) ValidationResult {
	// 总开关关闭或过滤器未初始化：直接放行
	if !v.enabled || v.filter == nil {
		return ValidationResult{
			Verdict:     spam.SpamVerdict{Score: 0, Action: spam.ActionAllow},
			SpamScore:   0,
			SpamReasons: "",
		}
	}

	// 调用反垃圾评估（spam.Filter 内部已处理失败降级）
	verdict := v.filter.Evaluate(ctx, in.IP, in.SenderDomain, in.HELODomain)

	// 持久化 spam_log（修复原 Node.js 未落库缺陷）
	if v.spamLogDAO != nil {
		v.recordSpamLog(ctx, in, verdict)
	}

	result := ValidationResult{
		Verdict:      verdict,
		SpamScore:    verdict.Score,
		SpamReasons:  verdict.FormatReasons(),
		ShouldReject: verdict.ShouldReject(),
		ShouldMark:   verdict.ShouldMark(),
	}

	slog.Info("SMTP 邮件验证完成",
		"ip", in.IP,
		"sender", in.Sender,
		"recipient", in.Recipient,
		"score", result.SpamScore,
		"action", verdict.Action,
		"reject", result.ShouldReject,
		"mark", result.ShouldMark,
	)

	return result
}

// recordSpamLog 记录 spam_log。
// 失败降级：写入失败仅记录 warn 日志，不阻断投递。
func (v *MessageValidator) recordSpamLog(ctx context.Context, in ValidateInput, verdict spam.SpamVerdict) {
	_, err := v.spamLogDAO.Create(ctx, dao.CreateSpamLogInput{
		Sender:    in.Sender,
		Recipient: in.Recipient,
		IP:        in.IP,
		Score:     verdict.Score,
		Reasons:   verdict.FormatReasons(),
		Action:    verdict.Action,
	})
	if err != nil {
		slog.Warn("spam_log 写入失败（不阻断投递）",
			"sender", in.Sender,
			"recipient", in.Recipient,
			"ip", in.IP,
			"score", verdict.Score,
			"error", err,
		)
	}
}
