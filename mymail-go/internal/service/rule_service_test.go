// Package service
// rule_service_test.go 测试 RuleService 完整业务逻辑。
// 使用真实 SQLite 临时数据库，覆盖所有成功与错误路径。
//
// 测试覆盖：
//   - Create + List
//   - Update：成功、校验归属（403）、不存在（404）
//   - Delete：成功、校验归属（403）、不存在（404）
package service

import (
	"context"
	"testing"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// sampleConditions 构造测试用条件。
func sampleConditions() []dao.RuleCondition {
	return []dao.RuleCondition{
		{Field: "from", Op: "contains", Value: "spam@example.com"},
	}
}

// sampleActions 构造测试用动作。
func sampleActions() []dao.RuleAction {
	return []dao.RuleAction{
		{Type: "move", Folder: "JUNK"},
	}
}

func TestRuleService_CreateAndList(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	// 创建 2 条规则
	id1, err := svc.Create(ctx, dao.CreateRuleInput{
		UserID:     uid,
		Name:       "rule1",
		Priority:   10,
		Conditions: sampleConditions(),
		Actions:    sampleActions(),
	})
	if err != nil {
		t.Fatalf("Create rule1 失败: %v", err)
	}
	if id1 <= 0 {
		t.Error("ID 应 > 0")
	}

	id2, err := svc.Create(ctx, dao.CreateRuleInput{
		UserID:   uid,
		Name:     "rule2",
		Priority: 5,
	})
	if err != nil {
		t.Fatalf("Create rule2 失败: %v", err)
	}

	// List 应返回 2 条（按 priority 降序：rule1(10) 在前）
	rules, err := svc.List(ctx, uid)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("期望 2 条规则，实际 %d", len(rules))
	}
	// priority 降序：第一条应为 priority=10
	if rules[0].Priority != 10 {
		t.Errorf("第一条 Priority 期望 10，实际 %d", rules[0].Priority)
	}
	if rules[0].Name != "rule1" {
		t.Errorf("第一条 Name 期望 rule1，实际 %q", rules[0].Name)
	}
	if rules[1].ID != id2 {
		t.Errorf("第二条 ID 期望 %d，实际 %d", id2, rules[1].ID)
	}
	// 默认 IsActive 应为 true
	if !rules[0].IsActive {
		t.Error("新建规则 IsActive 应默认 true")
	}
}

func TestRuleService_Update_Success(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	id, _ := svc.Create(ctx, dao.CreateRuleInput{
		UserID:   uid,
		Name:     "old-name",
		Priority: 5,
	})

	newName := "new-name"
	newPriority := 20
	if err := svc.Update(ctx, id, uid, dao.UpdateRuleInput{
		Name:     &newName,
		Priority: &newPriority,
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}

	// 验证更新
	rules, _ := svc.List(ctx, uid)
	if len(rules) != 1 {
		t.Fatalf("期望 1 条规则，实际 %d", len(rules))
	}
	if rules[0].Name != "new-name" {
		t.Errorf("Name 期望 new-name，实际 %q", rules[0].Name)
	}
	if rules[0].Priority != 20 {
		t.Errorf("Priority 期望 20，实际 %d", rules[0].Priority)
	}
}

func TestRuleService_Update_OtherUserForbidden(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")
	other := createTestUserForService(t, database, "bob")

	id, _ := svc.Create(ctx, dao.CreateRuleInput{
		UserID: uid,
		Name:   "rule",
	})

	newName := "hacked"
	err := svc.Update(ctx, id, other, dao.UpdateRuleInput{Name: &newName})
	if err == nil {
		t.Fatal("其他用户更新应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T: %v", err, err)
	}
	if ae.Status != 403 {
		t.Errorf("Status 期望 403，实际 %d", ae.Status)
	}
	if ae.Message != "无权操作此规则" {
		t.Errorf("Message 期望 '无权操作此规则'，实际 %q", ae.Message)
	}

	// 验证未被修改
	rules, _ := svc.List(ctx, uid)
	if rules[0].Name != "rule" {
		t.Errorf("归属校验失败后 Name 应不变，实际 %q", rules[0].Name)
	}
}

func TestRuleService_Update_NotExist(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	newName := "x"
	err := svc.Update(ctx, 99999, uid, dao.UpdateRuleInput{Name: &newName})
	if err == nil {
		t.Fatal("不存在应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T: %v", err, err)
	}
	if ae.Status != 404 {
		t.Errorf("Status 期望 404，实际 %d", ae.Status)
	}
	if ae.Message != "规则不存在" {
		t.Errorf("Message 期望 '规则不存在'，实际 %q", ae.Message)
	}
}

func TestRuleService_Update_IsActive(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	id, _ := svc.Create(ctx, dao.CreateRuleInput{UserID: uid, Name: "rule"})

	// 停用
	inactive := false
	if err := svc.Update(ctx, id, uid, dao.UpdateRuleInput{IsActive: &inactive}); err != nil {
		t.Fatalf("Update IsActive 失败: %v", err)
	}
	rules, _ := svc.List(ctx, uid)
	if rules[0].IsActive {
		t.Error("IsActive 期望 false")
	}

	// 重新启用
	active := true
	svc.Update(ctx, id, uid, dao.UpdateRuleInput{IsActive: &active})
	rules, _ = svc.List(ctx, uid)
	if !rules[0].IsActive {
		t.Error("IsActive 期望 true")
	}
}

func TestRuleService_Delete_Success(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	id, _ := svc.Create(ctx, dao.CreateRuleInput{UserID: uid, Name: "rule"})

	if err := svc.Delete(ctx, id, uid); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}

	// 验证已删除
	rules, _ := svc.List(ctx, uid)
	if len(rules) != 0 {
		t.Errorf("删除后应 0 条规则，实际 %d", len(rules))
	}
}

func TestRuleService_Delete_OtherUserForbidden(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")
	other := createTestUserForService(t, database, "bob")

	id, _ := svc.Create(ctx, dao.CreateRuleInput{UserID: uid, Name: "rule"})

	err := svc.Delete(ctx, id, other)
	if err == nil {
		t.Fatal("其他用户删除应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T", err)
	}
	if ae.Status != 403 {
		t.Errorf("Status 期望 403，实际 %d", ae.Status)
	}

	// 验证未被删除
	rules, _ := svc.List(ctx, uid)
	if len(rules) != 1 {
		t.Errorf("归属校验失败后应保留规则，实际 %d 条", len(rules))
	}
}

func TestRuleService_Delete_NotExist(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewRuleService(dao.NewRuleDAO(database))
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	err := svc.Delete(ctx, 99999, uid)
	if err == nil {
		t.Fatal("不存在应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T", err)
	}
	if ae.Status != 404 {
		t.Errorf("Status 期望 404，实际 %d", ae.Status)
	}
}
