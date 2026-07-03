// rule_test.go 测试 RuleDAO 的 CRUD 操作。
//
// 测试覆盖：
//   - Create / FindByID / FindByUserID / FindActiveByUserID
//   - Update（name / priority / conditions / actions / is_active / 无字段）
//   - Delete
//   - JSON 序列化/反序列化往返（含 bool/float64/string value）
//   - priority 降序排序
//   - 空条件与空动作
//
// 使用真实 SQLite 临时数据库（复用 dao 包的 newTestDB / createTestUser）。
package dao

import (
	"context"
	"testing"
)

func TestRuleDAO_Create(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := rd.Create(ctx, CreateRuleInput{
		UserID:   uid,
		Name:     "rule1",
		Priority: 10,
		Conditions: []RuleCondition{
			{Field: "from", Op: "contains", Value: "spam@example.com"},
		},
		Actions: []RuleAction{
			{Type: "move", Folder: "JUNK"},
		},
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if id <= 0 {
		t.Errorf("ID 应 > 0")
	}
}

func TestRuleDAO_FindByID(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{
		UserID:   uid,
		Name:     "rule1",
		Priority: 10,
		Conditions: []RuleCondition{
			{Field: "from", Op: "contains", Value: "spam@example.com"},
		},
		Actions: []RuleAction{
			{Type: "move", Folder: "JUNK"},
		},
	})

	r, err := rd.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if r == nil {
		t.Fatal("FindByID 返回 nil")
	}
	if r.ID != id {
		t.Errorf("ID 期望 %d，实际 %d", id, r.ID)
	}
	if r.UserID != uid {
		t.Errorf("UserID 期望 %d，实际 %d", uid, r.UserID)
	}
	if r.Name != "rule1" {
		t.Errorf("Name 期望 rule1，实际 %s", r.Name)
	}
	if r.Priority != 10 {
		t.Errorf("Priority 期望 10，实际 %d", r.Priority)
	}
	if !r.IsActive {
		t.Error("新建规则 IsActive 应为 true")
	}
	if len(r.Conditions) != 1 {
		t.Fatalf("Conditions 长度期望 1，实际 %d", len(r.Conditions))
	}
	if r.Conditions[0].Field != "from" {
		t.Errorf("Condition[0].Field 期望 from，实际 %s", r.Conditions[0].Field)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("Actions 长度期望 1，实际 %d", len(r.Actions))
	}
	if r.Actions[0].Type != "move" {
		t.Errorf("Action[0].Type 期望 move，实际 %s", r.Actions[0].Type)
	}
}

func TestRuleDAO_FindByID_NotFound(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()

	_, err := rd.FindByID(ctx, 9999)
	if err == nil {
		t.Error("查询不存在的规则应返回错误")
	}
}

func TestRuleDAO_FindByUserID(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 创建 3 条规则，priority 不同
	if _, err := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "low", Priority: 10}); err != nil {
		t.Fatalf("Create low 失败: %v", err)
	}
	if _, err := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "high", Priority: 100}); err != nil {
		t.Fatalf("Create high 失败: %v", err)
	}
	if _, err := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "mid", Priority: 50}); err != nil {
		t.Fatalf("Create mid 失败: %v", err)
	}

	rules, err := rd.FindByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByUserID 失败: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("应返回 3 条规则，实际 %d", len(rules))
	}
	// 验证按 priority 降序
	if rules[0].Name != "high" || rules[0].Priority != 100 {
		t.Errorf("第 1 条应为 high(100)，实际 %s(%d)", rules[0].Name, rules[0].Priority)
	}
	if rules[1].Name != "mid" || rules[1].Priority != 50 {
		t.Errorf("第 2 条应为 mid(50)，实际 %s(%d)", rules[1].Name, rules[1].Priority)
	}
	if rules[2].Name != "low" || rules[2].Priority != 10 {
		t.Errorf("第 3 条应为 low(10)，实际 %s(%d)", rules[2].Name, rules[2].Priority)
	}
}

func TestRuleDAO_FindByUserID_Empty(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	rules, err := rd.FindByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByUserID 失败: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("无规则应返回空切片，实际 %d", len(rules))
	}
}

func TestRuleDAO_FindActiveByUserID(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 创建 2 条活跃 + 1 条非活跃
	if _, err := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "active1", Priority: 10}); err != nil {
		t.Fatalf("Create active1 失败: %v", err)
	}
	if _, err := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "active2", Priority: 20}); err != nil {
		t.Fatalf("Create active2 失败: %v", err)
	}
	id3, err := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "inactive", Priority: 30})
	if err != nil {
		t.Fatalf("Create inactive 失败: %v", err)
	}
	inactive := false
	if err := rd.Update(ctx, id3, UpdateRuleInput{IsActive: &inactive}); err != nil {
		t.Fatalf("Update inactive 失败: %v", err)
	}

	rules, err := rd.FindActiveByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("FindActiveByUserID 失败: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("应返回 2 条活跃规则，实际 %d", len(rules))
	}
	for _, r := range rules {
		if !r.IsActive {
			t.Errorf("规则 %s 应为活跃", r.Name)
		}
	}
}

func TestRuleDAO_Update_Name(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "old", Priority: 10})
	newName := "new-name"
	if err := rd.Update(ctx, id, UpdateRuleInput{Name: &newName}); err != nil {
		t.Fatalf("Update Name 失败: %v", err)
	}
	r, _ := rd.FindByID(ctx, id)
	if r.Name != "new-name" {
		t.Errorf("Name 期望 new-name，实际 %s", r.Name)
	}
}

func TestRuleDAO_Update_Priority(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "r", Priority: 10})
	newPri := 99
	if err := rd.Update(ctx, id, UpdateRuleInput{Priority: &newPri}); err != nil {
		t.Fatalf("Update Priority 失败: %v", err)
	}
	r, _ := rd.FindByID(ctx, id)
	if r.Priority != 99 {
		t.Errorf("Priority 期望 99，实际 %d", r.Priority)
	}
}

func TestRuleDAO_Update_Conditions(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{
		UserID: uid, Name: "r", Priority: 10,
		Conditions: []RuleCondition{{Field: "from", Op: "contains", Value: "old"}},
	})
	newConds := []RuleCondition{
		{Field: "subject", Op: "equals", Value: "new"},
		{Field: "size", Op: "gt", Value: float64(1024)},
	}
	if err := rd.Update(ctx, id, UpdateRuleInput{Conditions: &newConds}); err != nil {
		t.Fatalf("Update Conditions 失败: %v", err)
	}
	r, _ := rd.FindByID(ctx, id)
	if len(r.Conditions) != 2 {
		t.Fatalf("Conditions 长度期望 2，实际 %d", len(r.Conditions))
	}
	if r.Conditions[0].Field != "subject" {
		t.Errorf("Condition[0].Field 期望 subject，实际 %s", r.Conditions[0].Field)
	}
	// value 经 JSON 往返后为 float64
	if v, ok := r.Conditions[1].Value.(float64); !ok || v != 1024 {
		t.Errorf("Condition[1].Value 期望 float64(1024)，实际 %v", r.Conditions[1].Value)
	}
}

func TestRuleDAO_Update_Actions(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{
		UserID: uid, Name: "r", Priority: 10,
		Actions: []RuleAction{{Type: "mark_read"}},
	})
	newActs := []RuleAction{
		{Type: "move", Folder: "JUNK"},
		{Type: "flag", Flag: "\\Seen"},
	}
	if err := rd.Update(ctx, id, UpdateRuleInput{Actions: &newActs}); err != nil {
		t.Fatalf("Update Actions 失败: %v", err)
	}
	r, _ := rd.FindByID(ctx, id)
	if len(r.Actions) != 2 {
		t.Fatalf("Actions 长度期望 2，实际 %d", len(r.Actions))
	}
	if r.Actions[0].Type != "move" || r.Actions[0].Folder != "JUNK" {
		t.Errorf("Action[0] 期望 move/JUNK，实际 %s/%s", r.Actions[0].Type, r.Actions[0].Folder)
	}
}

func TestRuleDAO_Update_IsActive(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "r", Priority: 10})
	// 停用
	inactive := false
	if err := rd.Update(ctx, id, UpdateRuleInput{IsActive: &inactive}); err != nil {
		t.Fatalf("Update IsActive=false 失败: %v", err)
	}
	r, _ := rd.FindByID(ctx, id)
	if r.IsActive {
		t.Error("停用后 IsActive 应为 false")
	}
	// 重新激活
	active := true
	if err := rd.Update(ctx, id, UpdateRuleInput{IsActive: &active}); err != nil {
		t.Fatalf("Update IsActive=true 失败: %v", err)
	}
	r, _ = rd.FindByID(ctx, id)
	if !r.IsActive {
		t.Error("激活后 IsActive 应为 true")
	}
}

func TestRuleDAO_Update_NoFields(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "r", Priority: 10})
	// 无字段更新，应无操作无错误
	if err := rd.Update(ctx, id, UpdateRuleInput{}); err != nil {
		t.Errorf("无字段更新应返回 nil，实际: %v", err)
	}
}

func TestRuleDAO_Delete(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := rd.Create(ctx, CreateRuleInput{UserID: uid, Name: "r", Priority: 10})
	if err := rd.Delete(ctx, id); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	// 删除后查询应返回错误
	_, err := rd.FindByID(ctx, id)
	if err == nil {
		t.Error("删除后查询应返回错误")
	}
}

func TestRuleDAO_JSON_RoundTrip(t *testing.T) {
	// 验证复杂 conditions/actions 的 JSON 序列化/反序列化
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	in := CreateRuleInput{
		UserID:   uid,
		Name:     "complex-rule",
		Priority: 50,
		Conditions: []RuleCondition{
			{Field: "from", Op: "contains", Value: "spam@example.com"},
			{Field: "subject", Op: "regex", Value: "^lottery"},
			{Field: "size", Op: "gt", Value: float64(1048576)},
			{Field: "has_attachment", Op: "equals", Value: true},
		},
		Actions: []RuleAction{
			{Type: "move", Folder: "JUNK"},
			{Type: "flag", Flag: "\\Seen"},
			{Type: "mark_read"},
			{Type: "forward", Address: "admin@example.com"},
		},
	}
	id, err := rd.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	r, err := rd.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}

	// 验证 conditions 往返
	if len(r.Conditions) != len(in.Conditions) {
		t.Fatalf("Conditions 长度期望 %d，实际 %d", len(in.Conditions), len(r.Conditions))
	}
	for i, c := range in.Conditions {
		got := r.Conditions[i]
		if got.Field != c.Field || got.Op != c.Op {
			t.Errorf("Condition[%d] Field/Op 不匹配: got %s/%s, want %s/%s",
				i, got.Field, got.Op, c.Field, c.Op)
		}
	}
	// bool value 往返（JSON 解码为 bool）
	if v, ok := r.Conditions[3].Value.(bool); !ok || v != true {
		t.Errorf("Condition[3].Value 期望 bool(true)，实际 %v", r.Conditions[3].Value)
	}
	// float64 value 往返
	if v, ok := r.Conditions[2].Value.(float64); !ok || v != 1048576 {
		t.Errorf("Condition[2].Value 期望 float64(1048576)，实际 %v", r.Conditions[2].Value)
	}

	// 验证 actions 往返
	if len(r.Actions) != len(in.Actions) {
		t.Fatalf("Actions 长度期望 %d，实际 %d", len(in.Actions), len(r.Actions))
	}
	for i, a := range in.Actions {
		got := r.Actions[i]
		if got.Type != a.Type {
			t.Errorf("Action[%d].Type 期望 %s，实际 %s", i, a.Type, got.Type)
		}
	}
	if r.Actions[0].Folder != "JUNK" {
		t.Errorf("Action[0].Folder 期望 JUNK，实际 %s", r.Actions[0].Folder)
	}
	if r.Actions[1].Flag != "\\Seen" {
		t.Errorf("Action[1].Flag 期望 \\Seen，实际 %s", r.Actions[1].Flag)
	}
	if r.Actions[3].Address != "admin@example.com" {
		t.Errorf("Action[3].Address 期望 admin@example.com，实际 %s", r.Actions[3].Address)
	}
}

func TestRuleDAO_Create_EmptyConditionsAndActions(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 空 conditions 和 actions
	id, err := rd.Create(ctx, CreateRuleInput{
		UserID:     uid,
		Name:       "empty-rule",
		Priority:   10,
		Conditions: nil,
		Actions:    nil,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	r, _ := rd.FindByID(ctx, id)
	if r.Conditions != nil && len(r.Conditions) != 0 {
		t.Errorf("空 Conditions 应反序列化为 nil 或空切片，实际 %v", r.Conditions)
	}
	if r.Actions != nil && len(r.Actions) != 0 {
		t.Errorf("空 Actions 应反序列化为 nil 或空切片，实际 %v", r.Actions)
	}
}

// TestRuleDAO_DBError 覆盖各方法的 DB 错误分支。
// 关闭 DB 后所有操作应返回错误（不 panic）。
func TestRuleDAO_DBError(t *testing.T) {
	database := newTestDB(t)
	rd := NewRuleDAO(database)
	ctx := context.Background()

	// 关闭 DB 触发错误（Cleanup 会再次 Close，sql.DB.Close 幂等安全）
	_ = database.Close()

	// Create 应返回错误
	_, err := rd.Create(ctx, CreateRuleInput{UserID: 1, Name: "x"})
	if err == nil {
		t.Error("Create 在 DB 关闭后应返回错误")
	}

	// FindByID 应返回错误
	_, err = rd.FindByID(ctx, 1)
	if err == nil {
		t.Error("FindByID 在 DB 关闭后应返回错误")
	}

	// FindByUserID 应返回错误
	_, err = rd.FindByUserID(ctx, 1)
	if err == nil {
		t.Error("FindByUserID 在 DB 关闭后应返回错误")
	}

	// FindActiveByUserID 应返回错误
	_, err = rd.FindActiveByUserID(ctx, 1)
	if err == nil {
		t.Error("FindActiveByUserID 在 DB 关闭后应返回错误")
	}

	// Delete 应返回错误
	if err := rd.Delete(ctx, 1); err == nil {
		t.Error("Delete 在 DB 关闭后应返回错误")
	}

	// Update 应返回错误（带字段更新）
	name := "x"
	if err := rd.Update(ctx, 1, UpdateRuleInput{Name: &name}); err == nil {
		t.Error("Update 在 DB 关闭后应返回错误")
	}
}
