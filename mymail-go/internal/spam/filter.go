// filter.go 反垃圾主流程，整合 SPF + DNSBL + 评分。
//
// 职责：
//   - 根据特性开关决定是否执行 SPF/DNSBL 校验
//   - 整合 SPF 与 DNSBL 结果，调用 Scorer 计算评分
//   - 输出最终判决（allow/mark/reject）
//
// 失败降级（验收标准 6）：
//   - DNS 查询失败时，SPF 返回 none、DNSBL 返回未命中
//   - 邮件仍正常投递（评分可能为 2，但不会 reject）
//   - 不向调用方返回错误
package spam

import (
	"context"
	"log/slog"
)

// SpamVerdict 反垃圾判决结果。
type SpamVerdict struct {
	Score   int
	Reasons []string
	Action  string // allow / mark / reject
}

// SPFCheckerIface SPF 校验器接口（用于依赖注入与 mock 测试）。
// *SPFChecker 自动满足此接口。
type SPFCheckerIface interface {
	Check(ctx context.Context, ip, senderDomain, heloDomain string) SPFResult
}

// DNSBLCheckerIface DNSBL 查询器接口（用于依赖注入与 mock 测试）。
// *DNSBLChecker 自动满足此接口。
type DNSBLCheckerIface interface {
	Check(ctx context.Context, ip string) DNSBLResult
}

// Filter 反垃圾过滤器。
//
// 集成 SPFChecker + DNSBLChecker + Scorer，对外暴露 Evaluate 方法。
// 特性开关：
//   - spfEnabled=false 时跳过 SPF 校验
//   - dnsblEnabled=false 时跳过 DNSBL 校验
type Filter struct {
	spfChecker   SPFCheckerIface
	dnsblChecker DNSBLCheckerIface
	scorer       *Scorer
	spfEnabled   bool
	dnsblEnabled bool
}

// NewFilter 创建反垃圾过滤器。
//
// 参数：
//   - spf: SPF 校验器（传 nil 则不执行 SPF）
//   - dnsbl: DNSBL 查询器（传 nil 则不执行 DNSBL）
//   - scorer: 评分器
//   - spfEnabled: SPF 总开关
//   - dnsblEnabled: DNSBL 总开关
func NewFilter(spf SPFCheckerIface, dnsbl DNSBLCheckerIface, scorer *Scorer, spfEnabled, dnsblEnabled bool) *Filter {
	return &Filter{
		spfChecker:   spf,
		dnsblChecker: dnsbl,
		scorer:       scorer,
		spfEnabled:   spfEnabled,
		dnsblEnabled: dnsblEnabled,
	}
}

// Evaluate 评估邮件是否为垃圾邮件。
//
// 参数：
//   - ip: 发件人 IP
//   - senderDomain: 发件人邮箱域名
//   - heloDomain: HELO/EHLO 域名
//
// 返回：SpamVerdict（始终非 nil，即使内部错误也降级返回 allow）
func (f *Filter) Evaluate(ctx context.Context, ip, senderDomain, heloDomain string) SpamVerdict {
	// 兜底：如果 scorer 未初始化，直接放行
	if f.scorer == nil {
		return SpamVerdict{Score: 0, Reasons: nil, Action: ActionAllow}
	}

	// SPF 校验
	spfResult := SPFResult{Result: SPFResultSkip, Detail: "spf disabled"}
	if f.spfEnabled && f.spfChecker != nil {
		spfResult = f.spfChecker.Check(ctx, ip, senderDomain, heloDomain)
		slog.Debug("SPF 校验完成",
			"ip", ip,
			"domain", senderDomain,
			"result", spfResult.Result,
			"detail", spfResult.Detail,
		)
	}

	// DNSBL 校验
	dnsblResult := DNSBLResult{Listed: false, Zones: nil}
	if f.dnsblEnabled && f.dnsblChecker != nil {
		dnsblResult = f.dnsblChecker.Check(ctx, ip)
		slog.Debug("DNSBL 校验完成",
			"ip", ip,
			"listed", dnsblResult.Listed,
			"zones", dnsblResult.Zones,
		)
	}

	// 评分
	scoreResult := f.scorer.Calculate(spfResult, dnsblResult)
	action := f.scorer.Decide(scoreResult.Score)

	slog.Info("反垃圾评估完成",
		"ip", ip,
		"domain", senderDomain,
		"spf", spfResult.Result,
		"dnsbl_listed", dnsblResult.Listed,
		"score", scoreResult.Score,
		"action", action,
	)

	return SpamVerdict{
		Score:   scoreResult.Score,
		Reasons: scoreResult.Reasons,
		Action:  action,
	}
}

// ShouldReject 是否拒绝投递。
func (v SpamVerdict) ShouldReject() bool { return v.Action == ActionReject }

// ShouldMark 是否标记可疑（投递到 JUNK）。
func (v SpamVerdict) ShouldMark() bool { return v.Action == ActionMark }

// FormatReasons 将原因列表格式化为逗号分隔字符串（用于持久化到 spam_reasons 字段）。
func (v SpamVerdict) FormatReasons() string {
	if len(v.Reasons) == 0 {
		return ""
	}
	out := v.Reasons[0]
	for i := 1; i < len(v.Reasons); i++ {
		out += "; " + v.Reasons[i]
	}
	return out
}
