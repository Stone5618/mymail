// engine_test.go 测试规则引擎主流程。
//
// 测试覆盖：
//   - 首匹配 break（高 priority 规则匹配后不检查低 priority）
//   - 无匹配无操作
//   - 无规则无操作
//   - 按 priority 降序执行（即使低 priority 规则先创建）
//   - 非活跃规则被跳过
//   - 多条件 AND 语义
//   - 加载失败降级（不阻断邮件投递）
//   - 动作失败不中断后续动作
//
// 使用真实 SQLite DB + 真实 RuleDAO + mock ActionApplier/Forwarder。
package rules

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// newRulesTestDB 创建临时 SQLite DB 并执行迁移。
// rules 包独立于 dao 包，无法复用 dao 包的 newTestDB，这里复制相同逻辑。
func newRulesTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

// createRulesTestUser 创建测试用户，返回 user_id。
func createRulesTestUser(t *testing.T, database *db.DB) int64 {
	t.Helper()
	ud := dao.NewUserDAO(database)
	id, err := ud.Create(context.Background(), dao.CreateUserInput{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return id
}

func TestEngine_NoRules_NoAction(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	// 用户无规则
	msg := baseMsg()
	err := engine.RunForMessage(context.Background(), msg.UserID, msg)
	if err != nil {
		t.Fatalf("无规则应无错误: %v", err)
	}
	if len(applier.moveCalls) != 0 || len(applier.markReadCalls) != 0 {
		t.Error("无规则时不应执行任何动作")
	}
}

func TestEngine_NoMatch_NoAction(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	uid := createRulesTestUser(t, database)
	// 创建一条不匹配的规则
	_, err := rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "rule1",
		Priority: 10,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpEquals, Value: "nobody@example.com"},
		},
		Actions: []dao.RuleAction{{Type: ActionMarkRead}},
	})
	if err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}

	msg := baseMsg()
	msg.UserID = uid
	err = engine.RunForMessage(context.Background(), uid, msg)
	if err != nil {
		t.Fatalf("无匹配应无错误: %v", err)
	}
	if len(applier.markReadCalls) != 0 {
		t.Error("无匹配时不应执行动作")
	}
}

func TestEngine_FirstMatchBreak(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	uid := createRulesTestUser(t, database)
	// 高 priority 规则（匹配）
	_, err := rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "high-priority-match",
		Priority: 100,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
		},
		Actions: []dao.RuleAction{{Type: ActionMarkRead}},
	})
	if err != nil {
		t.Fatalf("创建规则 1 失败: %v", err)
	}
	// 低 priority 规则（也匹配，但应被跳过）
	_, err = rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "low-priority-match",
		Priority: 50,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
		},
		Actions: []dao.RuleAction{{Type: ActionDelete}},
	})
	if err != nil {
		t.Fatalf("创建规则 2 失败: %v", err)
	}

	msg := baseMsg()
	msg.UserID = uid
	err = engine.RunForMessage(context.Background(), uid, msg)
	if err != nil {
		t.Fatalf("执行应无错误: %v", err)
	}
	// 高 priority 规则的 mark_read 应执行
	if len(applier.markReadCalls) != 1 {
		t.Errorf("高 priority 规则的 mark_read 应执行 1 次，实际 %d", len(applier.markReadCalls))
	}
	// 低 priority 规则的 delete 不应执行（首匹配 break）
	if len(applier.moveCalls) != 0 {
		t.Errorf("低 priority 规则不应执行（首匹配 break），move 调用 %d 次", len(applier.moveCalls))
	}
}

func TestEngine_PriorityOrder(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	uid := createRulesTestUser(t, database)
	// 低 priority 规则（先创建）
	_, err := rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "low",
		Priority: 10,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
		},
		Actions: []dao.RuleAction{{Type: ActionDelete}},
	})
	if err != nil {
		t.Fatalf("创建低 priority 规则失败: %v", err)
	}
	// 高 priority 规则（后创建）
	_, err = rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "high",
		Priority: 100,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
		},
		Actions: []dao.RuleAction{{Type: ActionMarkRead}},
	})
	if err != nil {
		t.Fatalf("创建高 priority 规则失败: %v", err)
	}

	msg := baseMsg()
	msg.UserID = uid
	err = engine.RunForMessage(context.Background(), uid, msg)
	if err != nil {
		t.Fatalf("执行应无错误: %v", err)
	}
	// 高 priority 规则的 mark_read 应执行（即使后创建）
	if len(applier.markReadCalls) != 1 {
		t.Errorf("高 priority 规则应执行 mark_read，实际 %d", len(applier.markReadCalls))
	}
	// 低 priority 规则的 delete 不应执行
	if len(applier.moveCalls) != 0 {
		t.Errorf("低 priority 规则不应执行")
	}
}

func TestEngine_InactiveRuleSkipped(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	uid := createRulesTestUser(t, database)
	// 创建非活跃规则（匹配）
	id, err := rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "inactive",
		Priority: 100,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
		},
		Actions: []dao.RuleAction{{Type: ActionMarkRead}},
	})
	if err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}
	// 停用规则
	inactive := false
	if err := rd.Update(context.Background(), id, dao.UpdateRuleInput{IsActive: &inactive}); err != nil {
		t.Fatalf("停用规则失败: %v", err)
	}

	msg := baseMsg()
	msg.UserID = uid
	err = engine.RunForMessage(context.Background(), uid, msg)
	if err != nil {
		t.Fatalf("执行应无错误: %v", err)
	}
	// 非活跃规则应被跳过
	if len(applier.markReadCalls) != 0 {
		t.Errorf("非活跃规则不应执行，mark_read 调用 %d 次", len(applier.markReadCalls))
	}
}

func TestEngine_MultipleConditions_And(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	uid := createRulesTestUser(t, database)
	// 两个条件：from 匹配 + subject 不匹配 → 整体不匹配
	_, err := rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "and-rule",
		Priority: 100,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
			{Field: FieldSubject, Op: OpEquals, Value: "nonexistent"},
		},
		Actions: []dao.RuleAction{{Type: ActionMarkRead}},
	})
	if err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}

	msg := baseMsg()
	msg.UserID = uid
	err = engine.RunForMessage(context.Background(), uid, msg)
	if err != nil {
		t.Fatalf("执行应无错误: %v", err)
	}
	// AND 语义：一个条件不匹配则整体不匹配
	if len(applier.markReadCalls) != 0 {
		t.Errorf("AND 条件部分不匹配时不应执行动作")
	}
}

func TestEngine_LoadFailure_Degrades(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	// 关闭 DB 模拟加载失败（Cleanup 会再次 Close，sql.DB.Close 幂等安全）
	_ = database.Close()

	msg := baseMsg()
	// 加载失败时应降级（不返回错误，不阻断邮件投递）
	err := engine.RunForMessage(context.Background(), 1, msg)
	if err != nil {
		t.Errorf("加载规则失败应降级返回 nil，实际: %v", err)
	}
	if len(applier.markReadCalls) != 0 || len(applier.moveCalls) != 0 {
		t.Error("加载失败时不应执行任何动作")
	}
}

func TestEngine_ActionFailureContinues(t *testing.T) {
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{
		markReadErr: errors.New("mark_read failed"),
	}
	exec := NewActionExecutor(applier, nil)
	engine := NewEngine(rd, NewMatcher(0), exec)

	uid := createRulesTestUser(t, database)
	_, err := rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "rule-with-multiple-actions",
		Priority: 100,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
		},
		Actions: []dao.RuleAction{
			{Type: ActionMarkRead},             // 失败
			{Type: ActionFlag, Flag: "\\Seen"}, // 应继续执行
		},
	})
	if err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}

	msg := baseMsg()
	msg.UserID = uid
	err = engine.RunForMessage(context.Background(), uid, msg)
	if err != nil {
		t.Fatalf("执行应无错误（动作失败不应上升）: %v", err)
	}
	// mark_read 失败，flag 应继续执行
	if len(applier.flagCalls) != 1 {
		t.Errorf("动作失败后 flag 仍应执行，flag 调用 %d 次", len(applier.flagCalls))
	}
}

func TestEngine_MatchAndExecute_FullFlow(t *testing.T) {
	// 端到端：规则匹配后执行多个动作
	database := newRulesTestDB(t)
	rd := dao.NewRuleDAO(database)
	applier := &mockActionApplier{}
	fwd := &mockForwarder{}
	exec := NewActionExecutor(applier, fwd)
	engine := NewEngine(rd, NewMatcher(0), exec)

	uid := createRulesTestUser(t, database)
	_, err := rd.Create(context.Background(), dao.CreateRuleInput{
		UserID:   uid,
		Name:     "full-flow",
		Priority: 100,
		Conditions: []dao.RuleCondition{
			{Field: FieldFrom, Op: OpContains, Value: "sender"},
			{Field: FieldSubject, Op: OpEquals, Value: "hello world"},
		},
		Actions: []dao.RuleAction{
			{Type: ActionMarkRead},
			{Type: ActionFlag, Flag: "\\Seen"},
			{Type: ActionForward, Address: "boss@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}

	msg := baseMsg()
	msg.UserID = uid
	err = engine.RunForMessage(context.Background(), uid, msg)
	if err != nil {
		t.Fatalf("执行应无错误: %v", err)
	}
	if len(applier.markReadCalls) != 1 {
		t.Errorf("mark_read 应执行 1 次，实际 %d", len(applier.markReadCalls))
	}
	if len(applier.flagCalls) != 1 {
		t.Errorf("flag 应执行 1 次，实际 %d", len(applier.flagCalls))
	}
	if len(fwd.calls) != 1 {
		t.Errorf("forward 应执行 1 次，实际 %d", len(fwd.calls))
	}
}
