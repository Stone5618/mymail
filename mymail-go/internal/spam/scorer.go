// scorer.go 实现反垃圾评分规则。
//
// 评分依据（Go 重构方案）：
//   - SPF fail:      +5
//   - SPF softfail:  +3
//   - SPF none:      +2
//   - SPF error:     +1
//   - SPF pass:       0
//   - DNSBL 命中:    +5/zone
//
// 阈值（默认值，可配置）：
//   - score < suspiciousThreshold (5): allow（投递到 INBOX）
//   - suspiciousThreshold <= score < spamThreshold (10): mark（标记可疑，投递到 JUNK）
//   - score >= spamThreshold (10): reject（拒绝投递）
//
// 与原 Node.js smtp-validator.js 的差异：
//   - 原 Node.js：SPF fail +8、DNSBL +10
//   - Go 版本：评分更温和（+5/+5），避免单点误判直接拒绝
package spam

import "fmt"

// 评分动作类型。
const (
	ActionAllow  = "allow"  // 允许投递（INBOX）
	ActionMark   = "mark"   // 标记可疑（JUNK）
	ActionReject = "reject" // 拒绝投递
)

// ScoreResult 评分结果。
type ScoreResult struct {
	Score   int
	Reasons []string
}

// Scorer 评分器。
type Scorer struct {
	suspiciousThreshold int
	spamThreshold       int
}

// NewScorer 创建评分器。
func NewScorer(suspiciousThreshold, spamThreshold int) *Scorer {
	if suspiciousThreshold <= 0 {
		suspiciousThreshold = 5
	}
	if spamThreshold <= 0 {
		spamThreshold = 10
	}
	return &Scorer{
		suspiciousThreshold: suspiciousThreshold,
		spamThreshold:       spamThreshold,
	}
}

// Calculate 根据 SPF 和 DNSBL 结果计算评分。
func (s *Scorer) Calculate(spf SPFResult, dnsbl DNSBLResult) ScoreResult {
	score := 0
	reasons := make([]string, 0, 4)

	// SPF 评分
	switch spf.Result {
	case SPFResultFail:
		score += 5
		reasons = append(reasons, fmt.Sprintf("SPF hard fail (%s)", spf.Detail))
	case SPFResultSoftFail:
		score += 3
		reasons = append(reasons, fmt.Sprintf("SPF softfail (%s)", spf.Detail))
	case SPFResultNone:
		score += 2
		reasons = append(reasons, "No SPF record")
	case SPFResultError:
		score += 1
		reasons = append(reasons, fmt.Sprintf("SPF check error (%s)", spf.Detail))
	case SPFResultTempError:
		score += 1
		reasons = append(reasons, fmt.Sprintf("SPF temp error (%s)", spf.Detail))
	case SPFResultSkip:
		// 跳过：不评分
	}

	// DNSBL 评分
	if dnsbl.Listed {
		score += 5 * len(dnsbl.Zones)
		reasons = append(reasons, fmt.Sprintf("IP blacklisted (%v)", dnsbl.Zones))
	}

	return ScoreResult{Score: score, Reasons: reasons}
}

// Decide 根据评分决定动作。
func (s *Scorer) Decide(score int) string {
	if score >= s.spamThreshold {
		return ActionReject
	}
	if score >= s.suspiciousThreshold {
		return ActionMark
	}
	return ActionAllow
}

// SuspiciousThreshold 返回可疑阈值（测试用）。
func (s *Scorer) SuspiciousThreshold() int { return s.suspiciousThreshold }

// SpamThreshold 返回垃圾阈值（测试用）。
func (s *Scorer) SpamThreshold() int { return s.spamThreshold }
