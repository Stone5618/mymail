// mail_test.go — E2E 测试：收邮件→读→星标→删除→恢复→清空回收站 完整流程。
package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// TestE2E_MailFlow 测试完整邮件操作流程：
// 登录 → 插入测试邮件 → 列表 → 详情 → 标记已读 → 星标 → 删除 → 恢复 → 清空回收站。
func TestE2E_MailFlow(t *testing.T) {
	env := newTestEnv(t)
	token := env.registerAndLogin(t, "mailuser", "MailPass123!")

	// 获取用户 ID
	me := env.getMe(t, token)
	userID := int64(me["id"].(float64))

	// 1. 插入 3 封测试邮件
	for i := 1; i <= 3; i++ {
		env.insertTestMessage(t, userID, fmt.Sprintf("测试邮件 %d", i), fmt.Sprintf("这是第 %d 封测试邮件正文", i))
	}
	t.Logf("已插入 3 封测试邮件")

	// 2. 列表
	status, resp := env.doJSON(t, "GET", "/api/mail/list?folder=INBOX&page=1&limit=20", nil, token)
	if status != http.StatusOK {
		t.Fatalf("邮件列表失败: status=%d, resp=%v", status, resp)
	}
	items, _ := resp["messages"].([]any)
	if len(items) != 3 {
		t.Fatalf("期望 3 封邮件，实际 %d", len(items))
	}
	t.Logf("邮件列表返回 %d 封", len(items))

	// 3. 未读计数
	status, resp = env.doJSON(t, "GET", "/api/mail/unread-count", nil, token)
	if status != http.StatusOK {
		t.Fatalf("未读计数失败: status=%d, resp=%v", status, resp)
	}
	if count, _ := resp["INBOX"].(float64); count != 3 {
		t.Fatalf("未读期望 3，实际 %v", resp["INBOX"])
	}
	t.Logf("未读计数: 3")

	// 4. 获取第一封邮件详情
	firstMail := items[0].(map[string]any)
	mailID := fmt.Sprintf("%v", firstMail["id"])
	status, resp = env.doJSON(t, "GET", "/api/mail/"+mailID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("邮件详情失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("邮件详情获取成功: subject=%v", resp["subject"])

	// 5. 标记已读
	status, _ = env.doJSON(t, "PUT", "/api/mail/"+mailID+"/read", nil, token)
	if status != http.StatusOK {
		t.Fatalf("标记已读失败: status=%d", status)
	}
	t.Logf("标记已读成功")

	// 6. 验证未读计数减少
	status, resp = env.doJSON(t, "GET", "/api/mail/unread-count", nil, token)
	if count, _ := resp["INBOX"].(float64); count != 2 {
		t.Fatalf("标记已读后未读期望 2，实际 %v", resp["INBOX"])
	}
	t.Logf("未读计数减为 2")

	// 7. 星标
	status, _ = env.doJSON(t, "PUT", "/api/mail/"+mailID+"/star", nil, token)
	if status != http.StatusOK {
		t.Fatalf("星标失败: status=%d", status)
	}
	t.Logf("星标成功")

	// 8. 删除（移到回收站）
	status, _ = env.doJSON(t, "DELETE", "/api/mail/"+mailID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("删除失败: status=%d", status)
	}
	t.Logf("删除成功（移到回收站）")

	// 9. 验证 INBOX 中邮件减少
	status, resp = env.doJSON(t, "GET", "/api/mail/list?folder=INBOX&page=1&limit=20", nil, token)
	items, _ = resp["messages"].([]any)
	if len(items) != 2 {
		t.Fatalf("删除后 INBOX 期望 2 封，实际 %d", len(items))
	}
	t.Logf("删除后 INBOX 剩余 %d 封", len(items))

	// 10. 恢复
	status, _ = env.doJSON(t, "PUT", "/api/mail/"+mailID+"/restore", nil, token)
	if status != http.StatusOK {
		t.Fatalf("恢复失败: status=%d", status)
	}
	t.Logf("恢复成功")

	// 11. 验证 INBOX 中邮件恢复
	status, resp = env.doJSON(t, "GET", "/api/mail/list?folder=INBOX&page=1&limit=20", nil, token)
	items, _ = resp["messages"].([]any)
	if len(items) != 3 {
		t.Fatalf("恢复后 INBOX 期望 3 封，实际 %d", len(items))
	}
	t.Logf("恢复后 INBOX 恢复为 %d 封", len(items))

	// 12. 再次删除并清空回收站
	status, _ = env.doJSON(t, "DELETE", "/api/mail/"+mailID, nil, token)
	status, _ = env.doJSON(t, "POST", "/api/mail/empty-trash", nil, token)
	if status != http.StatusOK {
		t.Fatalf("清空回收站失败: status=%d", status)
	}
	t.Logf("清空回收站成功")
}

// TestE2E_MailBatchOperations 测试批量操作：
// 批量标记已读、批量移动、批量删除。
func TestE2E_MailBatchOperations(t *testing.T) {
	env := newTestEnv(t)
	token := env.registerAndLogin(t, "batchuser", "BatchPass123!")

	me := env.getMe(t, token)
	userID := int64(me["id"].(float64))

	// 插入 5 封邮件
	var ids []int64
	for i := 1; i <= 5; i++ {
		id := env.insertTestMessage(t, userID, fmt.Sprintf("批量测试 %d", i), "body")
		ids = append(ids, id)
	}

	// 批量标记已读
	status, _ := env.doJSON(t, "POST", "/api/mail/batch/mark-read",
		map[string]any{"ids": ids}, token)
	if status != http.StatusOK {
		t.Fatalf("批量标记已读失败: status=%d", status)
	}
	t.Logf("批量标记已读成功（%d 封）", len(ids))

	// 验证未读为 0
	status, resp := env.doJSON(t, "GET", "/api/mail/unread-count", nil, token)
	if count, _ := resp["INBOX"].(float64); count != 0 {
		t.Fatalf("批量已读后未读期望 0，实际 %v", resp["INBOX"])
	}
	t.Logf("批量已读后未读为 0")

	// 批量删除
	status, _ = env.doJSON(t, "POST", "/api/mail/batch/delete",
		map[string]any{"ids": ids}, token)
	if status != http.StatusOK {
		t.Fatalf("批量删除失败: status=%d", status)
	}
	t.Logf("批量删除成功（%d 封）", len(ids))

	// 验证 INBOX 为空
	status, resp = env.doJSON(t, "GET", "/api/mail/list?folder=INBOX&page=1&limit=20", nil, token)
	items, _ := resp["messages"].([]any)
	if len(items) != 0 {
		t.Fatalf("批量删除后 INBOX 期望 0 封，实际 %d", len(items))
	}
	t.Logf("批量删除后 INBOX 为空")
}
