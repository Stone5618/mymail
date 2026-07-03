// scorer_test.go 测试评分规则。
//
// 覆盖：
//   - 各种 SPF + DNSBL 组合的评分
//   - Decide 阈值判断（allow/mark/reject）
//   - 默认值构造
package spam

import "testing"

func TestScorer_Calculate_AllSPFResults(t *testing.T) {
	s := NewScorer(5, 10)

	tests := []struct {
		name       string
		spf        SPFResult
		dnsbl      DNSBLResult
		wantScore  int
		wantReason int
	}{
		{
			name:       "SPF pass, no DNSBL",
			spf:        SPFResult{Result: SPFResultPass, Detail: "ip4:1.2.3.4"},
			dnsbl:      DNSBLResult{Listed: false},
			wantScore:  0,
			wantReason: 0,
		},
		{
			name:       "SPF fail, no DNSBL",
			spf:        SPFResult{Result: SPFResultFail, Detail: "-all"},
			dnsbl:      DNSBLResult{Listed: false},
			wantScore:  5,
			wantReason: 1,
		},
		{
			name:       "SPF softfail, no DNSBL",
			spf:        SPFResult{Result: SPFResultSoftFail, Detail: "~all"},
			dnsbl:      DNSBLResult{Listed: false},
			wantScore:  3,
			wantReason: 1,
		},
		{
			name:       "SPF none, no DNSBL",
			spf:        SPFResult{Result: SPFResultNone, Detail: "no record"},
			dnsbl:      DNSBLResult{Listed: false},
			wantScore:  2,
			wantReason: 1,
		},
		{
			name:       "SPF error, no DNSBL",
			spf:        SPFResult{Result: SPFResultError, Detail: "dns error"},
			dnsbl:      DNSBLResult{Listed: false},
			wantScore:  1,
			wantReason: 1,
		},
		{
			name:       "SPF temperror, no DNSBL",
			spf:        SPFResult{Result: SPFResultTempError, Detail: "timeout"},
			dnsbl:      DNSBLResult{Listed: false},
			wantScore:  1,
			wantReason: 1,
		},
		{
			name:       "SPF skip, no DNSBL",
			spf:        SPFResult{Result: SPFResultSkip, Detail: "disabled"},
			dnsbl:      DNSBLResult{Listed: false},
			wantScore:  0,
			wantReason: 0,
		},
		{
			name:       "SPF pass, DNSBL 1 zone",
			spf:        SPFResult{Result: SPFResultPass, Detail: "pass"},
			dnsbl:      DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
			wantScore:  5,
			wantReason: 1,
		},
		{
			name:       "SPF fail, DNSBL 1 zone",
			spf:        SPFResult{Result: SPFResultFail, Detail: "-all"},
			dnsbl:      DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
			wantScore:  10,
			wantReason: 2,
		},
		{
			name:       "SPF fail, DNSBL 2 zones",
			spf:        SPFResult{Result: SPFResultFail, Detail: "-all"},
			dnsbl:      DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org", "bl.spamcop.net"}},
			wantScore:  15,
			wantReason: 2,
		},
		{
			name:       "SPF softfail, DNSBL 1 zone",
			spf:        SPFResult{Result: SPFResultSoftFail, Detail: "~all"},
			dnsbl:      DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
			wantScore:  8,
			wantReason: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := s.Calculate(tt.spf, tt.dnsbl)
			if r.Score != tt.wantScore {
				t.Errorf("Score 期望 %d，实际 %d (reasons: %v)", tt.wantScore, r.Score, r.Reasons)
			}
			if len(r.Reasons) != tt.wantReason {
				t.Errorf("Reasons 数量期望 %d，实际 %d (%v)", tt.wantReason, len(r.Reasons), r.Reasons)
			}
		})
	}
}

func TestScorer_Decide(t *testing.T) {
	s := NewScorer(5, 10)

	tests := []struct {
		name  string
		score int
		want  string
	}{
		{"score 0 → allow", 0, ActionAllow},
		{"score 4 → allow", 4, ActionAllow},
		{"score 5 → mark", 5, ActionMark},
		{"score 9 → mark", 9, ActionMark},
		{"score 10 → reject", 10, ActionReject},
		{"score 15 → reject", 15, ActionReject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Decide(tt.score)
			if got != tt.want {
				t.Errorf("Decide(%d) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}

func TestScorer_Defaults(t *testing.T) {
	s := NewScorer(0, 0)
	if s.SuspiciousThreshold() != 5 {
		t.Errorf("默认 suspiciousThreshold 期望 5，实际 %d", s.SuspiciousThreshold())
	}
	if s.SpamThreshold() != 10 {
		t.Errorf("默认 spamThreshold 期望 10，实际 %d", s.SpamThreshold())
	}
}

func TestScorer_CustomThresholds(t *testing.T) {
	s := NewScorer(3, 8)
	if s.SuspiciousThreshold() != 3 {
		t.Errorf("custom suspiciousThreshold 期望 3，实际 %d", s.SuspiciousThreshold())
	}
	if s.SpamThreshold() != 8 {
		t.Errorf("custom spamThreshold 期望 8，实际 %d", s.SpamThreshold())
	}
	// 验证自定义阈值生效
	if got := s.Decide(3); got != ActionMark {
		t.Errorf("Decide(3) with custom threshold 期望 mark，实际 %s", got)
	}
	if got := s.Decide(8); got != ActionReject {
		t.Errorf("Decide(8) with custom threshold 期望 reject，实际 %s", got)
	}
}
