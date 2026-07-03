// Package rules 实现邮件规则引擎（阶段 4）。
//
// 包含：
//   - matcher.go：条件匹配（P1-7：Go regexp RE2 防 ReDoS + pattern 长度限制 + 编译缓存）
//   - action.go：动作执行（move/mark_read/star/delete/flag/forward）
//   - engine.go：规则执行主流程
//
// 修复的缺陷：
//   - P1-7：原 Node.js rule-engine.js 用 `new RegExp(strVal, 'i')` 用户可控正则，存在 ReDoS 风险
//   - Go 版本使用标准库 regexp（基于 RE2），不回溯，天然防 ReDoS
//   - 额外限制 pattern 长度（RulesMaxPatternLength=500）
//   - 编译失败、超长 pattern 直接返回 false（不匹配）
package rules

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// MessageView 规则匹配用的邮件视图。
// 包含条件匹配与动作执行所需的全部字段。
type MessageView struct {
	ID        int64
	UserID    int64
	FromAddr  string
	ToAddr    string
	Subject   string
	HasAttach bool
	SizeBytes int64
	IsStarred bool
	Flags     string
	BodyHTML  string // forward 动作需要
	BodyText  string // forward 动作需要
}

// 字段名常量。
const (
	FieldFrom          = "from"
	FieldTo            = "to"
	FieldSubject       = "subject"
	FieldHasAttachment = "has_attachment"
	FieldSize          = "size"
)

// 操作符常量。
const (
	OpContains = "contains"
	OpEquals   = "equals"
	OpEndsWith = "ends_with"
	OpRegex    = "regex"
	OpGt       = "gt"
	OpLt       = "lt"
)

// Matcher 条件匹配器。
//
// P1-7 修复：regex 操作符使用 Go 标准库 regexp（RE2，不回溯）。
// 额外防护：
//   - pattern 长度限制（maxPatternLength，默认 500）
//   - 编译失败的 pattern 直接返回 false
//   - 编译成功的 pattern 缓存（避免重复编译开销）
type Matcher struct {
	maxPatternLength int
	cacheMu          sync.RWMutex
	compiledCache    map[string]*regexp.Regexp
}

// NewMatcher 创建匹配器。
//
// 参数：
//   - maxPatternLength: 正则 pattern 最大长度（<= 0 时默认 500）
func NewMatcher(maxPatternLength int) *Matcher {
	if maxPatternLength <= 0 {
		maxPatternLength = 500
	}
	return &Matcher{
		maxPatternLength: maxPatternLength,
		compiledCache:    make(map[string]*regexp.Regexp),
	}
}

// MatchAll 匹配所有条件（AND 语义）。
// 空条件列表返回 true（与原 Node.js every() 一致）。
func (m *Matcher) MatchAll(conds []dao.RuleCondition, msg MessageView) bool {
	for _, c := range conds {
		if !m.MatchCondition(c, msg) {
			return false
		}
	}
	return true
}

// MatchCondition 匹配单个条件。
func (m *Matcher) MatchCondition(cond dao.RuleCondition, msg MessageView) bool {
	switch cond.Field {
	case FieldFrom:
		return m.matchStringOp(cond, strings.ToLower(msg.FromAddr))
	case FieldTo:
		return m.matchStringOp(cond, strings.ToLower(msg.ToAddr))
	case FieldSubject:
		return m.matchStringOp(cond, strings.ToLower(msg.Subject))
	case FieldHasAttachment:
		return m.matchHasAttachment(cond, msg.HasAttach)
	case FieldSize:
		return m.matchSize(cond, msg.SizeBytes)
	default:
		return false
	}
}

// matchStringOp 字符串字段匹配。
// fieldValue 已转为小写。
func (m *Matcher) matchStringOp(cond dao.RuleCondition, fieldValue string) bool {
	strVal, ok := cond.Value.(string)
	if !ok {
		// JSON 解码后可能是其他类型，尝试转换
		strVal = fmt.Sprintf("%v", cond.Value)
	}
	strVal = strings.ToLower(strVal)

	switch cond.Op {
	case OpContains:
		return strings.Contains(fieldValue, strVal)
	case OpEquals:
		return fieldValue == strVal
	case OpEndsWith:
		return strings.HasSuffix(fieldValue, strVal)
	case OpRegex:
		return m.matchRegex(strVal, fieldValue)
	default:
		return false
	}
}

// matchRegex 正则匹配（P1-7：RE2 防护 + 长度限制 + 缓存）。
func (m *Matcher) matchRegex(pattern, fieldValue string) bool {
	// 长度限制
	if len(pattern) > m.maxPatternLength {
		return false
	}
	// 空模式不匹配
	if pattern == "" {
		return false
	}

	// 查缓存
	m.cacheMu.RLock()
	re, ok := m.compiledCache[pattern]
	m.cacheMu.RUnlock()

	if !ok {
		// 编译（RE2 不回溯，天然防 ReDoS）
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			// 编译失败：缓存 nil 防止重复编译，返回 false
			m.cacheMu.Lock()
			m.compiledCache[pattern] = nil
			m.cacheMu.Unlock()
			return false
		}
		// 缓存编译结果
		m.cacheMu.Lock()
		m.compiledCache[pattern] = re
		m.cacheMu.Unlock()
	}

	if re == nil {
		return false // 编译失败的 pattern
	}
	return re.MatchString(fieldValue)
}

// matchHasAttachment 匹配 has_attachment 字段。
// value 可为 bool 或字符串 "true"/"false"。
func (m *Matcher) matchHasAttachment(cond dao.RuleCondition, hasAttach bool) bool {
	wantAttach := false
	switch v := cond.Value.(type) {
	case bool:
		wantAttach = v
	case string:
		wantAttach = strings.EqualFold(v, "true")
	default:
		return false
	}
	if cond.Op != "" && cond.Op != OpEquals {
		return false // has_attachment 仅支持 equals
	}
	return hasAttach == wantAttach
}

// matchSize 匹配 size 字段。
// value 可为数字或字符串数字。
func (m *Matcher) matchSize(cond dao.RuleCondition, sizeBytes int64) bool {
	var numVal int64
	switch v := cond.Value.(type) {
	case float64:
		numVal = int64(v)
	case int:
		numVal = int64(v)
	case int64:
		numVal = v
	case string:
		var n int
		_, err := fmt.Sscanf(v, "%d", &n)
		if err != nil {
			return false
		}
		numVal = int64(n)
	default:
		return false
	}

	switch cond.Op {
	case OpGt:
		return sizeBytes > numVal
	case OpLt:
		return sizeBytes < numVal
	case OpEquals:
		return sizeBytes == numVal
	default:
		return false
	}
}

// MaxPatternLength 返回最大 pattern 长度（测试用）。
func (m *Matcher) MaxPatternLength() int { return m.maxPatternLength }

// CacheSize 返回缓存大小（测试用）。
func (m *Matcher) CacheSize() int {
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	return len(m.compiledCache)
}
