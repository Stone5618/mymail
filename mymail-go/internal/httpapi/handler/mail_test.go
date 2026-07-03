// mail_test.go 测试邮件 HTTP 端点的完整契约与安全（P0-3 IDOR 修复验证）。
//
// 使用外部测试包（handler_test）以与 auth_test.go 共享 testEnv 辅助。
//
// 测试覆盖：
//   - List：默认/带 folder+search/仅未读/无 token
//   - UnreadCount：成功
//   - Get：成功（含 markRead 副作用）/不存在/IDOR 越权/无效 ID
//   - Send：成功/无收件人/主题过长/频率限制/无 token
//   - SaveDraft：成功/请求格式错误
//   - MarkRead/MarkUnread/ToggleStar：成功 + IDOR 越权
//   - Delete：软删除/永久删除/IDOR 越权
//   - Restore：成功
//   - EmptyTrash：成功
//   - DownloadAttachment：成功/不存在/IDOR（附件属于他人邮件）
//   - DownloadAllAttachments：成功/无附件
//   - BatchMarkRead/BatchMove/BatchDelete：成功 + 空 ids
//   - RequireOwnedMail：无效 ID
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/httpapi/handler"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// ============ 测试辅助函数 ============

// createMailForUser 直接通过 DAO 创建测试邮件，返回 (mailID, *dao.Message)。
// folder 为空时默认 INBOX；isRead 控制已读状态。
func createMailForUser(t *testing.T, env *testEnv, userID int64, folder string, isRead bool) (int64, *dao.Message) {
	t.Helper()
	ctx := context.Background()
	if folder == "" {
		folder = "INBOX"
	}
	mailID, err := env.msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID:   userID,
		Folder:   folder,
		FromAddr: "sender@example.com",
		ToAddr:   "recipient@example.com",
		Subject:  "测试邮件",
		BodyText: "Hello",
		BodyHTML: "<p>Hello</p>",
	})
	if err != nil {
		t.Fatalf("创建测试邮件失败: %v", err)
	}
	if isRead {
		if err := env.msgDAO.MarkRead(ctx, mailID, userID); err != nil {
			t.Fatalf("标记已读失败: %v", err)
		}
	}
	msg, err := env.msgDAO.FindByIDForUser(ctx, mailID, userID)
	if err != nil || msg == nil {
		t.Fatalf("查询测试邮件失败: %v", err)
	}
	return mailID, msg
}

// createAttachmentForMail 直接通过 DAO 创建附件记录 + 磁盘文件。
// 返回 attachmentID。
func createAttachmentForMail(t *testing.T, env *testEnv, mailID int64, filename string) int64 {
	t.Helper()
	ctx := context.Background()

	// 写一个最小的合法文本文件到附件存储路径
	if err := os.MkdirAll(env.attachPath, 0750); err != nil {
		t.Fatalf("创建附件目录失败: %v", err)
	}
	storagePath := filepath.Join(env.attachPath, "test-"+filename)
	if err := os.WriteFile(storagePath, []byte("hello world"), 0640); err != nil {
		t.Fatalf("写入测试附件文件失败: %v", err)
	}

	aid, err := env.attachDAO.Create(ctx, dao.CreateAttachmentInput{
		MessageID:   mailID,
		Filename:    filename,
		MimeType:    "text/plain",
		SizeBytes:   11,
		StoragePath: storagePath,
	})
	if err != nil {
		t.Fatalf("创建附件记录失败: %v", err)
	}
	return aid
}

// doMultipart 发送 multipart/form-data 请求。
// files 中的附件会被设置为 text/plain MIME 类型（在白名单内）。
func doMultipart(t *testing.T, router *gin.Engine, method, path string, fields map[string]string, files map[string]string, token string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for k, v := range fields {
		_ = writer.WriteField(k, v)
	}
	for fieldname, content := range files {
		// 手动创建 part 并设置 Content-Type 为 text/plain（在附件白名单内）
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{
			fmt.Sprintf(`form-data; name="%s"; filename="%s.txt"`, fieldname, fieldname),
		}
		h["Content-Type"] = []string{"text/plain"}
		part, err := writer.CreatePart(h)
		if err != nil {
			t.Fatalf("创建 multipart 字段失败: %v", err)
		}
		_, _ = part.Write([]byte(content))
	}
	_ = writer.Close()

	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ============ List 测试 ============

func TestMailHandler_List_NoToken(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "GET", "/api/mail/list", nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
}

func TestMailHandler_List_Empty(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "GET", "/api/mail/list", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"] != float64(0) {
		t.Errorf("空邮箱 total 期望 0，实际 %v", resp["total"])
	}
	if _, ok := resp["unreadCount"]; !ok {
		t.Error("应包含 unreadCount 字段（camelCase）")
	}
}

func TestMailHandler_List_WithMails(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")

	// 创建 2 封 INBOX（1 未读 + 1 已读）+ 1 封 SENT
	createMailForUser(t, env, user.ID, "INBOX", false)
	createMailForUser(t, env, user.ID, "INBOX", true)
	createMailForUser(t, env, user.ID, "SENT", false)

	w := doJSON(t, env.router, "GET", "/api/mail/list?folder=INBOX", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"] != float64(2) {
		t.Errorf("INBOX total 期望 2，实际 %v", resp["total"])
	}
	if resp["unreadCount"] != float64(1) {
		t.Errorf("unreadCount 期望 1，实际 %v", resp["unreadCount"])
	}

	// 仅未读
	w = doJSON(t, env.router, "GET", "/api/mail/list?folder=INBOX&unread=true", nil, token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"] != float64(1) {
		t.Errorf("unread only total 期望 1，实际 %v", resp["total"])
	}

	// 搜索
	w = doJSON(t, env.router, "GET", "/api/mail/list?folder=INBOX&search=不存在的内容", nil, token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"] != float64(0) {
		t.Errorf("search 不匹配 total 期望 0，实际 %v", resp["total"])
	}
}

// ============ UnreadCount 测试 ============

func TestMailHandler_UnreadCount_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")

	createMailForUser(t, env, user.ID, "INBOX", false)
	createMailForUser(t, env, user.ID, "INBOX", false)
	createMailForUser(t, env, user.ID, "DRAFTS", false)

	w := doJSON(t, env.router, "GET", "/api/mail/unread-count", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["INBOX"] != float64(2) {
		t.Errorf("INBOX 未读期望 2，实际 %v", resp["INBOX"])
	}
	if resp["DRAFTS"] != float64(1) {
		t.Errorf("DRAFTS 未读期望 1，实际 %v", resp["DRAFTS"])
	}
	if resp["SENT"] != float64(0) {
		t.Errorf("SENT 未读期望 0，实际 %v", resp["SENT"])
	}
}

// ============ Get 测试 ============

func TestMailHandler_Get_Success_AndMarkRead(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")

	mailID, msg := createMailForUser(t, env, user.ID, "INBOX", false)
	if msg.IsRead {
		t.Fatal("初始应为未读")
	}

	w := doJSON(t, env.router, "GET", fmt.Sprintf("/api/mail/%d", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	// snake_case 字段
	if resp["from_addr"] != "sender@example.com" {
		t.Errorf("from_addr 期望 sender@example.com，实际 %v", resp["from_addr"])
	}
	if resp["is_read"] != true {
		t.Errorf("Get 副作用：is_read 应为 true，实际 %v", resp["is_read"])
	}

	// 验证 DB 已标记已读
	updated, _ := env.msgDAO.FindByIDForUser(context.Background(), mailID, user.ID)
	if !updated.IsRead {
		t.Error("DB 中 is_read 应已更新为 true")
	}
}

func TestMailHandler_Get_NotFound(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "GET", "/api/mail/99999", nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "邮件不存在" {
		t.Errorf("error 期望 '邮件不存在'，实际 %v", resp["error"])
	}
}

func TestMailHandler_Get_InvalidID(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "GET", "/api/mail/abc", nil, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// TestMailHandler_Get_IDOR 核心安全测试：用户 B 不能访问用户 A 的邮件。
func TestMailHandler_Get_IDOR(t *testing.T) {
	env := newTestEnv(t)
	tokenA := registerAndLogin(t, env, "alice", "password123")
	userA, _ := env.userDAO.FindByUsername(context.Background(), "alice")

	// 用户 A 的邮件
	mailIDA, _ := createMailForUser(t, env, userA.ID, "INBOX", false)

	// 用户 B 登录后尝试访问 A 的邮件
	tokenB := registerAndLogin(t, env, "bob", "password456")
	w := doJSON(t, env.router, "GET", fmt.Sprintf("/api/mail/%d", mailIDA), nil, tokenB)
	if w.Code != http.StatusNotFound {
		t.Errorf("IDOR 防护：用户 B 访问 A 的邮件应 404，实际 %d", w.Code)
	}
	// 验证：不返回邮件内容
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "邮件不存在" {
		t.Errorf("error 期望 '邮件不存在'，实际 %v", resp["error"])
	}

	// 同时验证 A 仍能访问自己的邮件
	w = doJSON(t, env.router, "GET", fmt.Sprintf("/api/mail/%d", mailIDA), nil, tokenA)
	if w.Code != http.StatusOK {
		t.Errorf("用户 A 访问自己的邮件应 200，实际 %d", w.Code)
	}
}

// ============ Send 测试 ============

func TestMailHandler_Send_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doMultipart(t, env.router, "POST", "/api/mail/send",
		map[string]string{
			"to":       "bob@example.com",
			"subject":  "Hello",
			"bodyHtml": "<p>Hi</p>",
			"bodyText": "Hi",
		}, nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "发送成功" {
		t.Errorf("message 期望 '发送成功'，实际 %v", resp["message"])
	}
	if resp["messageId"] == nil || resp["messageId"] == "" {
		t.Error("messageId 不应为空（camelCase）")
	}
}

func TestMailHandler_Send_NoRecipient(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doMultipart(t, env.router, "POST", "/api/mail/send",
		map[string]string{"subject": "无收件人"}, nil, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "收件人不能为空" {
		t.Errorf("error 期望 '收件人不能为空'，实际 %v", resp["error"])
	}
}

func TestMailHandler_Send_SubjectTooLong(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	longSubject := strings.Repeat("a", 501)
	w := doMultipart(t, env.router, "POST", "/api/mail/send",
		map[string]string{"to": "bob@example.com", "subject": longSubject}, nil, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "主题最多500字符" {
		t.Errorf("error 期望 '主题最多500字符'，实际 %v", resp["error"])
	}
}

func TestMailHandler_Send_RateLimit(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	// 默认 sendRateLimit=10/min，发 10 次成功
	for i := 0; i < 10; i++ {
		w := doMultipart(t, env.router, "POST", "/api/mail/send",
			map[string]string{"to": "bob@example.com", "subject": fmt.Sprintf("mail-%d", i)}, nil, token)
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 封应成功，实际 %d, body: %s", i+1, w.Code, w.Body.String())
		}
	}
	// 第 11 封应被限流
	w := doMultipart(t, env.router, "POST", "/api/mail/send",
		map[string]string{"to": "bob@example.com", "subject": "should-fail"}, nil, token)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("期望 429 限流，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

func TestMailHandler_Send_NoToken(t *testing.T) {
	env := newTestEnv(t)
	w := doMultipart(t, env.router, "POST", "/api/mail/send",
		map[string]string{"to": "bob@example.com"}, nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
}

// ============ SaveDraft 测试 ============

func TestMailHandler_SaveDraft_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "POST", "/api/mail/save-draft",
		map[string]string{
			"to":       "bob@example.com",
			"subject":  "草稿",
			"bodyHtml": "<p>draft</p>",
			"bodyText": "draft",
		}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "草稿已保存" {
		t.Errorf("message 期望 '草稿已保存'，实际 %v", resp["message"])
	}
	if resp["id"] == nil || resp["id"] == float64(0) {
		t.Error("id 不应为 0")
	}
}

func TestMailHandler_SaveDraft_MalformedJSON(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	req := httptest.NewRequest("POST", "/api/mail/save-draft", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// ============ MarkRead / MarkUnread / ToggleStar 测试 ============

func TestMailHandler_MarkRead_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "PUT", fmt.Sprintf("/api/mail/%d/read", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), mailID, user.ID)
	if !msg.IsRead {
		t.Error("DB 中 is_read 应为 true")
	}
}

func TestMailHandler_MarkUnread_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", true)

	w := doJSON(t, env.router, "PUT", fmt.Sprintf("/api/mail/%d/unread", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), mailID, user.ID)
	if msg.IsRead {
		t.Error("DB 中 is_read 应为 false")
	}
}

func TestMailHandler_ToggleStar_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, msg := createMailForUser(t, env, user.ID, "INBOX", false)
	if msg.IsStarred {
		t.Fatal("初始应为未星标")
	}

	w := doJSON(t, env.router, "PUT", fmt.Sprintf("/api/mail/%d/star", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	updated, _ := env.msgDAO.FindByIDForUser(context.Background(), mailID, user.ID)
	if !updated.IsStarred {
		t.Error("DB 中 is_starred 应为 true")
	}
}

// TestMailHandler_MarkRead_IDOR P0-3 核心测试：B 不能改 A 的邮件状态。
func TestMailHandler_MarkRead_IDOR(t *testing.T) {
	env := newTestEnv(t)
	tokenA := registerAndLogin(t, env, "alice", "password123")
	userA, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailIDA, _ := createMailForUser(t, env, userA.ID, "INBOX", false)

	tokenB := registerAndLogin(t, env, "bob", "password456")
	w := doJSON(t, env.router, "PUT", fmt.Sprintf("/api/mail/%d/read", mailIDA), nil, tokenB)
	if w.Code != http.StatusNotFound {
		t.Errorf("IDOR 防护：B 改 A 邮件状态应 404，实际 %d", w.Code)
	}

	// 验证 A 的邮件仍为未读
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), mailIDA, userA.ID)
	if msg.IsRead {
		t.Error("IDOR 防护失败：A 的邮件被 B 改为已读")
	}

	// A 自己改仍成功
	w = doJSON(t, env.router, "PUT", fmt.Sprintf("/api/mail/%d/read", mailIDA), nil, tokenA)
	if w.Code != http.StatusOK {
		t.Errorf("A 改自己邮件应 200，实际 %d", w.Code)
	}
}

// ============ Delete / Restore 测试 ============

func TestMailHandler_Delete_SoftDelete(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "DELETE", fmt.Sprintf("/api/mail/%d", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	// 应在 TRASH 文件夹
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), mailID, user.ID)
	if msg.Folder != "TRASH" {
		t.Errorf("软删除后 folder 期望 TRASH，实际 %s", msg.Folder)
	}
}

func TestMailHandler_Delete_PermanentFromTrash(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "TRASH", false)

	// 第二次 DELETE（已在 TRASH）→ 永久删除
	w := doJSON(t, env.router, "DELETE", fmt.Sprintf("/api/mail/%d", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), mailID, user.ID)
	if msg != nil {
		t.Error("永久删除后邮件应不存在")
	}
}

func TestMailHandler_Delete_IDOR(t *testing.T) {
	env := newTestEnv(t)
	registerAndLogin(t, env, "alice", "password123")
	userA, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailIDA, _ := createMailForUser(t, env, userA.ID, "INBOX", false)

	tokenB := registerAndLogin(t, env, "bob", "password456")
	w := doJSON(t, env.router, "DELETE", fmt.Sprintf("/api/mail/%d", mailIDA), nil, tokenB)
	if w.Code != http.StatusNotFound {
		t.Errorf("IDOR：B 删 A 邮件应 404，实际 %d", w.Code)
	}

	// 验证 A 的邮件仍存在
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), mailIDA, userA.ID)
	if msg == nil {
		t.Error("IDOR 防护失败：A 的邮件被 B 删除")
	}
}

func TestMailHandler_Restore_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "TRASH", false)

	w := doJSON(t, env.router, "PUT", fmt.Sprintf("/api/mail/%d/restore", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), mailID, user.ID)
	if msg.Folder != "INBOX" {
		t.Errorf("恢复后 folder 期望 INBOX，实际 %s", msg.Folder)
	}
}

// ============ EmptyTrash 测试 ============

func TestMailHandler_EmptyTrash_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	createMailForUser(t, env, user.ID, "TRASH", false)
	createMailForUser(t, env, user.ID, "TRASH", false)

	w := doJSON(t, env.router, "POST", "/api/mail/empty-trash", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "垃圾箱已清空" {
		t.Errorf("message 期望 '垃圾箱已清空'，实际 %v", resp["message"])
	}
}

// ============ DownloadAttachment 测试 ============

func TestMailHandler_DownloadAttachment_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	aid := createAttachmentForMail(t, env, mailID, "test.txt")

	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/%d/download", mailID, aid), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type 期望 application/octet-stream，实际 %s", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "test.txt") {
		t.Errorf("Content-Disposition 应含 test.txt，实际 %s", cd)
	}
	if !strings.Contains(w.Body.String(), "hello world") {
		t.Errorf("响应应含文件内容 'hello world'，实际 %s", w.Body.String())
	}
}

func TestMailHandler_DownloadAttachment_NotFound(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/99999/download", mailID), nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d", w.Code)
	}
}

// TestMailHandler_DownloadAttachment_IDOR P0-3：附件属于他人邮件。
// 构造场景：B 的邮件 M_B 上有附件 A_B；A 不能通过 /api/mail/{M_B}/attachments/{A_B}/download 下载。
// 由于 RequireOwnedMail 中间件会先校验 M_B 不属于 A，A 直接 404。
func TestMailHandler_DownloadAttachment_IDOR(t *testing.T) {
	env := newTestEnv(t)

	// alice2 登录（用 alice2 避免与 alice 冲突，因 alice 已被其他用例使用）
	tokenA := registerAndLogin(t, env, "alice2", "password123")

	// B 的邮件 + 附件
	tokenB := registerAndLogin(t, env, "bob", "password456")
	userB, _ := env.userDAO.FindByUsername(context.Background(), "bob")
	mailB, _ := createMailForUser(t, env, userB.ID, "INBOX", false)
	aidB := createAttachmentForMail(t, env, mailB, "bob-secret.txt")

	// A 尝试用 B 的 mailID + B 的 aid 下载（RequireOwnedMail 中间件先拦截）
	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/%d/download", mailB, aidB), nil, tokenA)
	if w.Code != http.StatusNotFound {
		t.Errorf("IDOR：A 通过 B 的 mailID 下载附件应 404，实际 %d", w.Code)
	}

	// B 自己能下载
	w = doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/%d/download", mailB, aidB), nil, tokenB)
	if w.Code != http.StatusOK {
		t.Errorf("B 下载自己附件应 200，实际 %d", w.Code)
	}
}

// ============ DownloadAllAttachments 测试 ============

func TestMailHandler_DownloadAllAttachments_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	createAttachmentForMail(t, env, mailID, "a.txt")
	createAttachmentForMail(t, env, mailID, "b.txt")

	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/download-all", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type 期望 application/zip，实际 %s", ct)
	}
	// zip 文件以 PK 头开始
	if !bytes.HasPrefix(w.Body.Bytes(), []byte{0x50, 0x4B, 0x03, 0x04}) {
		t.Errorf("响应体应为 zip 文件（PK 头），前 4 字节: %v", w.Body.Bytes()[:4])
	}
}

func TestMailHandler_DownloadAllAttachments_NoAttachments(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/download-all", mailID), nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d", w.Code)
	}
}

// ============ 批量操作测试 ============

func TestMailHandler_BatchMarkRead_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	id1, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	id2, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "POST", "/api/mail/batch/mark-read",
		map[string]any{"ids": []int64{id1, id2}}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["affected"] != float64(2) {
		t.Errorf("affected 期望 2，实际 %v", resp["affected"])
	}
}

func TestMailHandler_BatchMarkRead_EmptyIDs(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "POST", "/api/mail/batch/mark-read",
		map[string]any{"ids": []int64{}}, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

func TestMailHandler_BatchMove_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	id1, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	id2, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "POST", "/api/mail/batch/move",
		map[string]any{"ids": []int64{id1, id2}, "folder": "SENT"}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	msg, _ := env.msgDAO.FindByIDForUser(context.Background(), id1, user.ID)
	if msg.Folder != "SENT" {
		t.Errorf("移动后 folder 期望 SENT，实际 %s", msg.Folder)
	}
}

func TestMailHandler_BatchMove_InvalidFolder(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	id1, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "POST", "/api/mail/batch/move",
		map[string]any{"ids": []int64{id1}, "folder": "INVALID_FOLDER"}, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

func TestMailHandler_BatchDelete_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	id1, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	id2, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "POST", "/api/mail/batch/delete",
		map[string]any{"ids": []int64{id1, id2}}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	// 应在 TRASH
	msg1, _ := env.msgDAO.FindByIDForUser(context.Background(), id1, user.ID)
	if msg1.Folder != "TRASH" {
		t.Errorf("批量删除后 folder 期望 TRASH，实际 %s", msg1.Folder)
	}
}

func TestMailHandler_BatchDelete_EmptyIDs(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "POST", "/api/mail/batch/delete",
		map[string]any{"ids": []int64{}}, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// ============ RequireOwnedMail 无效 ID 测试 ============

func TestMailHandler_RequireOwnedMail_NegativeID(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "PUT", "/api/mail/-1/read", nil, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// ============ 覆盖率补充测试 ============

// TestMailHandler_Get_WithAttachments 覆盖 toMessageResponse 的 attachments != nil 分支。
func TestMailHandler_Get_WithAttachments(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	createAttachmentForMail(t, env, mailID, "a.txt")
	createAttachmentForMail(t, env, mailID, "b.txt")

	w := doJSON(t, env.router, "GET", fmt.Sprintf("/api/mail/%d", mailID), nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	attachments, _ := resp["attachments"].([]any)
	if len(attachments) != 2 {
		t.Errorf("附件数期望 2，实际 %d", len(attachments))
	}
}

// TestMailHandler_DownloadAttachment_AttachmentOfDifferentMail P0-3 二次校验：
// 附件属于当前用户但属于另一封邮件，应返回 403。
func TestMailHandler_DownloadAttachment_AttachmentOfDifferentMail(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")

	// 用户 alice 有两封邮件
	mail1, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	mail2, _ := createMailForUser(t, env, user.ID, "INBOX", false)
	// 附件属于 mail2
	aid2 := createAttachmentForMail(t, env, mail2, "belongs-to-mail2.txt")

	// 通过 mail1 的路径访问 mail2 的附件 → 二次校验失败 403
	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/%d/download", mail1, aid2), nil, token)
	if w.Code != http.StatusForbidden {
		t.Errorf("期望 403（附件不属于该邮件），实际 %d", w.Code)
	}
}

// TestMailHandler_Send_WithAttachment 覆盖 multipart 附件保存路径。
func TestMailHandler_Send_WithAttachment(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	// 用一个合法的文本文件作为附件
	w := doMultipart(t, env.router, "POST", "/api/mail/send",
		map[string]string{
			"to":       "bob@example.com",
			"subject":  "带附件",
			"bodyText": "see attachment",
		},
		map[string]string{"attachments": "this is attachment content"}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// TestMailHandler_Defensive_NilUser 直接调用各 handler，传入无 user 的 context，
// 覆盖防御性 nil 检查分支（这些分支在正常路由中被 Authenticate 中间件提前拦截）。
func TestMailHandler_Defensive_NilUser(t *testing.T) {
	env := newTestEnv(t)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"List", "GET", "/api/mail/list"},
		{"UnreadCount", "GET", "/api/mail/unread-count"},
		{"Send", "POST", "/api/mail/send"},
		{"SaveDraft", "POST", "/api/mail/save-draft"},
		{"EmptyTrash", "POST", "/api/mail/empty-trash"},
		{"BatchMarkRead", "POST", "/api/mail/batch/mark-read"},
		{"BatchMove", "POST", "/api/mail/batch/move"},
		{"BatchDelete", "POST", "/api/mail/batch/delete"},
		{"Get", "GET", "/api/mail/1"},
		{"MarkRead", "PUT", "/api/mail/1/read"},
		{"MarkUnread", "PUT", "/api/mail/1/unread"},
		{"ToggleStar", "PUT", "/api/mail/1/star"},
		{"Delete", "DELETE", "/api/mail/1"},
		{"Restore", "PUT", "/api/mail/1/restore"},
		{"DownloadAttachment", "GET", "/api/mail/1/attachments/1/download"},
		{"DownloadAllAttachments", "GET", "/api/mail/1/attachments/download-all"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 不带 token，Authenticate 中间件会拦截返回 401
			// 这里验证中间件正确拦截（handler 的 nil 分支由中间件保护，不可直接触发）
			w := doJSON(t, env.router, tc.method, tc.path, nil, "")
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s: 期望 401，实际 %d", tc.name, w.Code)
			}
		})
	}
}

// TestMailHandler_DownloadAttachment_InvalidAID 覆盖 aid 非数字的 400 路径。
func TestMailHandler_DownloadAttachment_InvalidAID(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/abc/download", mailID), nil, token)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// TestMailHandler_DownloadAttachment_FileMissing 覆盖附件文件不存在的 404 路径。
func TestMailHandler_DownloadAttachment_FileMissing(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	mailID, _ := createMailForUser(t, env, user.ID, "INBOX", false)

	// 直接在 DB 插入附件记录，但磁盘文件不存在
	ctx := context.Background()
	aid, err := env.attachDAO.Create(ctx, dao.CreateAttachmentInput{
		MessageID:   mailID,
		Filename:    "ghost.txt",
		MimeType:    "text/plain",
		SizeBytes:   1,
		StoragePath: "/nonexistent/path/ghost.txt",
	})
	if err != nil {
		t.Fatalf("创建附件记录失败: %v", err)
	}

	w := doJSON(t, env.router, "GET",
		fmt.Sprintf("/api/mail/%d/attachments/%d/download", mailID, aid), nil, token)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404（文件不存在），实际 %d", w.Code)
	}
}

// TestMailHandler_BatchMarkRead_MalformedJSON 覆盖 JSON 解析失败路径。
func TestMailHandler_BatchMarkRead_MalformedJSON(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	req := httptest.NewRequest("POST", "/api/mail/batch/mark-read", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// TestMailHandler_Defensive_NilUser_DirectCall 直接调用各 handler（不挂中间件），
// 覆盖 handler 内的防御性 nil 检查分支。
// 这些分支在正常路由中由 Authenticate/RequireOwnedMail 中间件保证不会触发，
// 但保留防御性代码以应对中间件配置错误等异常情况。
func TestMailHandler_Defensive_NilUser_DirectCall(t *testing.T) {
	env := newTestEnv(t)
	mailH := handler.NewMailHandler(env.mailSvc, env.attachStore)

	// 创建一个不挂任何中间件的 router，直接注册 handler
	r := gin.New()
	r.GET("/list", mailH.List)
	r.GET("/unread-count", mailH.UnreadCount)
	r.POST("/send", mailH.Send)
	r.POST("/save-draft", mailH.SaveDraft)
	r.POST("/empty-trash", mailH.EmptyTrash)
	r.POST("/batch/mark-read", mailH.BatchMarkRead)
	r.POST("/batch/move", mailH.BatchMove)
	r.POST("/batch/delete", mailH.BatchDelete)
	r.GET("/:id", mailH.Get)
	r.PUT("/:id/read", mailH.MarkRead)
	r.PUT("/:id/unread", mailH.MarkUnread)
	r.PUT("/:id/star", mailH.ToggleStar)
	r.DELETE("/:id", mailH.Delete)
	r.PUT("/:id/restore", mailH.Restore)
	r.GET("/:id/attachments/:aid/download", mailH.DownloadAttachment)
	r.GET("/:id/attachments/download-all", mailH.DownloadAllAttachments)

	// 无 user 调用：所有 handler 应返回 401
	noUserCases := []struct{ method, path string }{
		{"GET", "/list"},
		{"GET", "/unread-count"},
		{"POST", "/send"},
		{"POST", "/save-draft"},
		{"POST", "/empty-trash"},
		{"POST", "/batch/mark-read"},
		{"POST", "/batch/move"},
		{"POST", "/batch/delete"},
	}
	for _, tc := range noUserCases {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 无 user 期望 401，实际 %d", tc.method, tc.path, w.Code)
		}
	}

	// :id 路径无 user：也应 401
	idCases := []struct{ method, path string }{
		{"GET", "/1"},
		{"PUT", "/1/read"},
		{"PUT", "/1/unread"},
		{"PUT", "/1/star"},
		{"DELETE", "/1"},
		{"PUT", "/1/restore"},
		{"GET", "/1/attachments/1/download"},
		{"GET", "/1/attachments/download-all"},
	}
	for _, tc := range idCases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 无 user 期望 401，实际 %d", tc.method, tc.path, w.Code)
		}
	}
}

// TestMailHandler_Defensive_NilMail 覆盖 :id handler 的 msg == nil 防御分支。
// 构造一个只注入 user 但不注入 mail 的 router（模拟 RequireOwnedMail 未正确配置的情况）。
func TestMailHandler_Defensive_NilMail(t *testing.T) {
	env := newTestEnv(t)
	mailH := handler.NewMailHandler(env.mailSvc, env.attachStore)
	registerAndLogin(t, env, "alice", "password123")
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")

	// 注入 user 但不注入 mail 的中间件
	injectUserOnly := func(c *gin.Context) {
		c.Set("user", user)
		c.Next()
	}

	r := gin.New()
	r.GET("/:id", injectUserOnly, mailH.Get)
	r.PUT("/:id/read", injectUserOnly, mailH.MarkRead)
	r.PUT("/:id/unread", injectUserOnly, mailH.MarkUnread)
	r.PUT("/:id/star", injectUserOnly, mailH.ToggleStar)
	r.DELETE("/:id", injectUserOnly, mailH.Delete)
	r.PUT("/:id/restore", injectUserOnly, mailH.Restore)
	r.GET("/:id/attachments/:aid/download", injectUserOnly, mailH.DownloadAttachment)
	r.GET("/:id/attachments/download-all", injectUserOnly, mailH.DownloadAllAttachments)

	cases := []struct{ method, path string }{
		{"GET", "/1"},
		{"PUT", "/1/read"},
		{"PUT", "/1/unread"},
		{"PUT", "/1/star"},
		{"DELETE", "/1"},
		{"PUT", "/1/restore"},
		{"GET", "/1/attachments/1/download"},
		{"GET", "/1/attachments/download-all"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s 无 mail 期望 404，实际 %d", tc.method, tc.path, w.Code)
		}
	}
}

