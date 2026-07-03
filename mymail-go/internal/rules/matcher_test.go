// matcher_test.go 测试条件匹配器。
//
// 测试覆盖：
//   - 5 个字段（from/to/subject/has_attachment/size）× 多个操作符的组合
//   - has_attachment / size 字段的多种 value 类型（bool/string/float64/int）
//   - P1-7 ReDoS 防护：超长 pattern、编译失败、病态回溯不卡死、编译缓存
//   - 并发安全（编译缓存的 RWMutex）
package rules

import (
	"strings"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// baseMsg 测试用的基础邮件视图，各用例按需覆盖字段。
func baseMsg() MessageView {
	return MessageView{
		ID:        1,
		UserID:    100,
		FromAddr:  "sender@example.com",
		ToAddr:    "recipient@example.com",
		Subject:   "Hello World",
		HasAttach: false,
		SizeBytes: 1024,
		IsStarred: false,
		Flags:     "[]",
		BodyHTML:  "<p>body</p>",
		BodyText:  "body",
	}
}

func TestNewMatcher_DefaultMaxPatternLength(t *testing.T) {
	m := NewMatcher(0)
	if m.MaxPatternLength() != 500 {
		t.Errorf("默认 maxPatternLength 期望 500，实际 %d", m.MaxPatternLength())
	}

	m2 := NewMatcher(-1)
	if m2.MaxPatternLength() != 500 {
		t.Errorf("负数 maxPatternLength 期望 500，实际 %d", m2.MaxPatternLength())
	}

	m3 := NewMatcher(100)
	if m3.MaxPatternLength() != 100 {
		t.Errorf("自定义 maxPatternLength 期望 100，实际 %d", m3.MaxPatternLength())
	}
}

func TestMatcher_MatchAll_EmptyConditions(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	if !m.MatchAll(nil, msg) {
		t.Error("空条件列表应返回 true")
	}
	if !m.MatchAll([]dao.RuleCondition{}, msg) {
		t.Error("空条件切片应返回 true")
	}
}

func TestMatcher_MatchAll_AllMatch(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	conds := []dao.RuleCondition{
		{Field: FieldFrom, Op: OpContains, Value: "sender"},
		{Field: FieldSubject, Op: OpEquals, Value: "hello world"},
	}
	if !m.MatchAll(conds, msg) {
		t.Error("所有条件匹配时应返回 true")
	}
}

func TestMatcher_MatchAll_AnyFail(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	conds := []dao.RuleCondition{
		{Field: FieldFrom, Op: OpContains, Value: "sender"},
		{Field: FieldSubject, Op: OpEquals, Value: "nonexistent"},
	}
	if m.MatchAll(conds, msg) {
		t.Error("任一条件不匹配时应返回 false")
	}
}

func TestMatcher_MatchCondition_UnknownField(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	cond := dao.RuleCondition{Field: "unknown_field", Op: OpEquals, Value: "x"}
	if m.MatchCondition(cond, msg) {
		t.Error("未知字段应返回 false")
	}
}

// ===== 字符串字段测试 =====

func TestMatcher_From_Contains(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	// 大小写不敏感（匹配器内部转小写）
	cond := dao.RuleCondition{Field: FieldFrom, Op: OpContains, Value: "SENDER"}
	if !m.MatchCondition(cond, msg) {
		t.Error("from + contains 应匹配（大小写不敏感）")
	}

	cond2 := dao.RuleCondition{Field: FieldFrom, Op: OpContains, Value: "nonexistent"}
	if m.MatchCondition(cond2, msg) {
		t.Error("不包含的子串不应匹配")
	}
}

func TestMatcher_From_Equals(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	cond := dao.RuleCondition{Field: FieldFrom, Op: OpEquals, Value: "SENDER@EXAMPLE.COM"}
	if !m.MatchCondition(cond, msg) {
		t.Error("from + equals 应匹配（大小写不敏感）")
	}

	cond2 := dao.RuleCondition{Field: FieldFrom, Op: OpEquals, Value: "other@example.com"}
	if m.MatchCondition(cond2, msg) {
		t.Error("不等于的值不应匹配")
	}
}

func TestMatcher_From_EndsWith(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	cond := dao.RuleCondition{Field: FieldFrom, Op: OpEndsWith, Value: "@EXAMPLE.COM"}
	if !m.MatchCondition(cond, msg) {
		t.Error("from + ends_with 应匹配")
	}

	cond2 := dao.RuleCondition{Field: FieldFrom, Op: OpEndsWith, Value: "@other.com"}
	if m.MatchCondition(cond2, msg) {
		t.Error("不以后缀结尾不应匹配")
	}
}

func TestMatcher_To_Contains(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	cond := dao.RuleCondition{Field: FieldTo, Op: OpContains, Value: "recipient"}
	if !m.MatchCondition(cond, msg) {
		t.Error("to + contains 应匹配")
	}
}

func TestMatcher_Subject_Regex(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "^hello"}
	if !m.MatchCondition(cond, msg) {
		t.Error("subject + regex ^hello 应匹配")
	}

	cond2 := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "^world"}
	if m.MatchCondition(cond2, msg) {
		t.Error("subject + regex ^world 不应匹配（world 在中间）")
	}
}

func TestMatcher_StringOp_NonStringValue(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	// value 为数字，应转字符串后匹配（from 不含 "123"）
	cond := dao.RuleCondition{Field: FieldFrom, Op: OpContains, Value: 123}
	if m.MatchCondition(cond, msg) {
		t.Error("数字 value 转 '123' 后 from 不包含，应返回 false")
	}
}

func TestMatcher_StringOp_InvalidOp(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	// 字符串字段不支持 gt
	cond := dao.RuleCondition{Field: FieldFrom, Op: OpGt, Value: "x"}
	if m.MatchCondition(cond, msg) {
		t.Error("字符串字段 + gt 应返回 false")
	}
}

// ===== has_attachment 字段测试 =====

func TestMatcher_HasAttachment_True(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.HasAttach = true

	cond := dao.RuleCondition{Field: FieldHasAttachment, Op: OpEquals, Value: true}
	if !m.MatchCondition(cond, msg) {
		t.Error("has_attachment=true + equals + true 应匹配")
	}

	cond2 := dao.RuleCondition{Field: FieldHasAttachment, Op: OpEquals, Value: false}
	if m.MatchCondition(cond2, msg) {
		t.Error("has_attachment=true + equals + false 不应匹配")
	}
}

func TestMatcher_HasAttachment_False(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.HasAttach = false

	cond := dao.RuleCondition{Field: FieldHasAttachment, Op: OpEquals, Value: false}
	if !m.MatchCondition(cond, msg) {
		t.Error("has_attachment=false + equals + false 应匹配")
	}
}

func TestMatcher_HasAttachment_StringValue(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.HasAttach = true

	// value 为字符串 "TRUE"（大小写不敏感）
	cond := dao.RuleCondition{Field: FieldHasAttachment, Op: OpEquals, Value: "TRUE"}
	if !m.MatchCondition(cond, msg) {
		t.Error("has_attachment + equals + 'TRUE'（字符串，大小写不敏感）应匹配")
	}
}

func TestMatcher_HasAttachment_InvalidValue(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.HasAttach = true

	// value 为数字，应返回 false
	cond := dao.RuleCondition{Field: FieldHasAttachment, Op: OpEquals, Value: 123}
	if m.MatchCondition(cond, msg) {
		t.Error("has_attachment + 数字 value 应返回 false")
	}
}

func TestMatcher_HasAttachment_InvalidOp(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.HasAttach = true

	// has_attachment 仅支持 equals
	cond := dao.RuleCondition{Field: FieldHasAttachment, Op: OpContains, Value: true}
	if m.MatchCondition(cond, msg) {
		t.Error("has_attachment + contains 应返回 false（仅支持 equals）")
	}
}

// ===== size 字段测试 =====

func TestMatcher_Size_Gt(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.SizeBytes = 1024

	cond := dao.RuleCondition{Field: FieldSize, Op: OpGt, Value: float64(500)}
	if !m.MatchCondition(cond, msg) {
		t.Error("size=1024 + gt + 500 应匹配")
	}

	cond2 := dao.RuleCondition{Field: FieldSize, Op: OpGt, Value: float64(2000)}
	if m.MatchCondition(cond2, msg) {
		t.Error("size=1024 + gt + 2000 不应匹配")
	}
}

func TestMatcher_Size_Lt(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.SizeBytes = 1024

	cond := dao.RuleCondition{Field: FieldSize, Op: OpLt, Value: float64(2000)}
	if !m.MatchCondition(cond, msg) {
		t.Error("size=1024 + lt + 2000 应匹配")
	}
}

func TestMatcher_Size_Equals(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.SizeBytes = 1024

	cond := dao.RuleCondition{Field: FieldSize, Op: OpEquals, Value: float64(1024)}
	if !m.MatchCondition(cond, msg) {
		t.Error("size=1024 + equals + 1024 应匹配")
	}
}

func TestMatcher_Size_StringValue(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.SizeBytes = 1024

	// value 为字符串数字
	cond := dao.RuleCondition{Field: FieldSize, Op: OpGt, Value: "500"}
	if !m.MatchCondition(cond, msg) {
		t.Error("size=1024 + gt + '500'（字符串数字）应匹配")
	}
}

func TestMatcher_Size_InvalidStringValue(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.SizeBytes = 1024

	// value 为无效字符串
	cond := dao.RuleCondition{Field: FieldSize, Op: OpGt, Value: "abc"}
	if m.MatchCondition(cond, msg) {
		t.Error("size + gt + 'abc'（无效字符串）应返回 false")
	}
}

func TestMatcher_Size_InvalidValue(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.SizeBytes = 1024

	// value 为 bool
	cond := dao.RuleCondition{Field: FieldSize, Op: OpGt, Value: true}
	if m.MatchCondition(cond, msg) {
		t.Error("size + gt + bool 应返回 false")
	}
}

func TestMatcher_Size_InvalidOp(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	msg.SizeBytes = 1024

	// size 不支持 contains
	cond := dao.RuleCondition{Field: FieldSize, Op: OpContains, Value: float64(1024)}
	if m.MatchCondition(cond, msg) {
		t.Error("size + contains 应返回 false")
	}
}

// ===== P1-7 ReDoS 防护测试 =====

func TestMatcher_Regex_PatternTooLong(t *testing.T) {
	m := NewMatcher(50) // 限制 50 字符
	msg := baseMsg()
	msg.Subject = "aaaaaaaaaa"

	// 超长 pattern（100 字符）
	longPattern := strings.Repeat("a", 100)
	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: longPattern}
	if m.MatchCondition(cond, msg) {
		t.Error("超长 pattern 应返回 false")
	}
}

func TestMatcher_Regex_EmptyPattern(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()

	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: ""}
	if m.MatchCondition(cond, msg) {
		t.Error("空 pattern 应返回 false")
	}
}

func TestMatcher_Regex_InvalidPattern(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()

	// 未闭合括号
	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "(unclosed"}
	if m.MatchCondition(cond, msg) {
		t.Error("编译失败的 pattern 应返回 false")
	}
}

// TestMatcher_Regex_ReDoSPattern_NotStuck 验证 P1-7 修复：
// 病态回溯 pattern（如 (a+)+$）在 Node.js 中匹配长字符串会指数级卡死，
// Go 标准库 regexp 基于 RE2 不回溯，应快速返回。
func TestMatcher_Regex_ReDoSPattern_NotStuck(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()
	// 构造长字符串 "aaaa...ab"（不匹配结尾 $）
	msg.Subject = strings.Repeat("a", 30) + "b"

	// 经典 ReDoS pattern
	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "(a+)+$"}

	done := make(chan bool, 1)
	var result bool
	go func() {
		result = m.MatchCondition(cond, msg)
		done <- true
	}()

	select {
	case <-done:
		// RE2 应快速返回 false（不匹配，因为以 b 结尾）
		if result {
			t.Error("ReDoS pattern 不应匹配")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("P1-7 修复失败：ReDoS pattern 导致卡死（RE2 应不回溯）")
	}
}

func TestMatcher_Regex_CompileCache(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()

	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "^hello"}

	// 首次匹配，编译并缓存
	m.MatchCondition(cond, msg)
	if m.CacheSize() != 1 {
		t.Errorf("首次匹配后缓存大小期望 1，实际 %d", m.CacheSize())
	}

	// 再次匹配，不增加缓存
	m.MatchCondition(cond, msg)
	if m.CacheSize() != 1 {
		t.Errorf("重复匹配不应增加缓存，期望 1，实际 %d", m.CacheSize())
	}

	// 不同 pattern
	cond2 := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "world$"}
	m.MatchCondition(cond2, msg)
	if m.CacheSize() != 2 {
		t.Errorf("新 pattern 后缓存大小期望 2，实际 %d", m.CacheSize())
	}
}

func TestMatcher_Regex_FailedCompileCache(t *testing.T) {
	m := NewMatcher(0)
	msg := baseMsg()

	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "(unclosed"}

	// 首次匹配，编译失败，缓存 nil
	m.MatchCondition(cond, msg)
	if m.CacheSize() != 1 {
		t.Errorf("编译失败后应缓存 nil，缓存大小期望 1，实际 %d", m.CacheSize())
	}

	// 再次匹配，不重复编译（缓存大小不变）
	m.MatchCondition(cond, msg)
	if m.CacheSize() != 1 {
		t.Errorf("重复编译失败不应增加缓存，期望 1，实际 %d", m.CacheSize())
	}
}

func TestMatcher_Regex_ConcurrentSafe(t *testing.T) {
	// 验证编译缓存的并发安全性（RWMutex 保护）
	m := NewMatcher(0)
	msg := baseMsg()
	cond := dao.RuleCondition{Field: FieldSubject, Op: OpRegex, Value: "^hello"}

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				m.MatchCondition(cond, msg)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	// 并发后缓存大小仍应为 1（同一 pattern）
	if m.CacheSize() != 1 {
		t.Errorf("并发后缓存大小期望 1，实际 %d", m.CacheSize())
	}
}
