// send_test.go — E2E 测试：发邮件（multipart/form-data）→ 验证 SENT 文件夹。
package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

// doMultipart 发送 multipart/form-data 请求，返回状态码与解析后的响应体。
// fields 为文本字段，files 为文件字段（fieldName → {filename, mimeType, content}）。
func (e *testEnv) doMultipart(t testing.TB, method, path, token string,
	fields map[string]string, files map[string]struct{ Filename, MimeType, Content string },
) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("写表单字段失败: %v", err)
		}
	}
	for field, f := range files {
		// 自定义 part 以设置正确的 Content-Type（CreateFormFile 硬编码 application/octet-stream）
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+f.Filename+`"`)
		h.Set("Content-Type", f.MimeType)
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("创建文件字段失败: %v", err)
		}
		if _, err := io.WriteString(w, f.Content); err != nil {
			t.Fatalf("写文件内容失败: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}

	req, err := http.NewRequest(method, e.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP 请求失败: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &result)
	}
	return resp.StatusCode, result
}

// TestE2E_SendMail 测试发信流程：
// 登录 → 发邮件到本地用户 → 验证 SENT 文件夹出现邮件。
func TestE2E_SendMail(t *testing.T) {
	env := newTestEnv(t)
	token := env.registerAndLogin(t, "sender", "SenderPass123!")

	// 先注册收件人（确保本地用户存在）
	env.registerAndLogin(t, "receiver", "ReceiverPass123!")

	// 1. 发送邮件（multipart/form-data）
	status, resp := env.doMultipart(t, "POST", "/api/mail/send", token,
		map[string]string{
			"to":       "receiver@example.com",
			"subject":  "E2E 发信测试",
			"bodyText": "这是 E2E 测试发送的邮件正文",
			"bodyHtml": "<p>这是 E2E 测试发送的邮件 HTML</p>",
		}, nil)
	if status != http.StatusOK {
		t.Fatalf("发信失败: status=%d, resp=%v", status, resp)
	}
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "成功") {
		t.Logf("发信响应 message: %v", resp["message"])
	}
	messageID, _ := resp["message_id"].(string)
	t.Logf("发信成功: message_id=%s", messageID)

	// 2. 验证 SENT 文件夹出现邮件
	status, resp = env.doJSON(t, "GET", "/api/mail/list?folder=SENT&page=1&limit=20", nil, token)
	if status != http.StatusOK {
		t.Fatalf("查询 SENT 列表失败: status=%d, resp=%v", status, resp)
	}
	items, _ := resp["messages"].([]any)
	if len(items) != 1 {
		t.Fatalf("SENT 期望 1 封邮件，实际 %d", len(items))
	}
	sent := items[0].(map[string]any)
	if subject, _ := sent["subject"].(string); subject != "E2E 发信测试" {
		t.Fatalf("SENT 邮件 subject 期望 'E2E 发信测试'，实际 %v", sent["subject"])
	}
	if toAddr, _ := sent["to_addr"].(string); !strings.Contains(toAddr, "receiver@example.com") {
		t.Fatalf("SENT 邮件 to_addr 期望包含 receiver@example.com，实际 %v", sent["to_addr"])
	}
	t.Logf("SENT 文件夹验证通过: subject=%v, to=%v", sent["subject"], sent["to_addr"])
}

// TestE2E_SendMail_WithAttachment 测试带附件发信。
func TestE2E_SendMail_WithAttachment(t *testing.T) {
	env := newTestEnv(t)
	token := env.registerAndLogin(t, "attach_sender", "AttachPass123!")
	env.registerAndLogin(t, "attach_receiver", "AttachRecvPass123!")

	// 发送带附件的邮件
	status, resp := env.doMultipart(t, "POST", "/api/mail/send", token,
		map[string]string{
			"to":       "attach_receiver@example.com",
			"subject":  "带附件的邮件",
			"bodyText": "请查收附件",
		},
		map[string]struct{ Filename, MimeType, Content string }{
			"attachments": {"test.txt", "text/plain", "这是附件内容"},
		})
	if status != http.StatusOK {
		t.Fatalf("带附件发信失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("带附件发信成功")

	// 验证 SENT 邮件 has_attach=true
	status, resp = env.doJSON(t, "GET", "/api/mail/list?folder=SENT&page=1&limit=20", nil, token)
	if status != http.StatusOK {
		t.Fatalf("查询 SENT 列表失败: status=%d", status)
	}
	items, _ := resp["messages"].([]any)
	if len(items) != 1 {
		t.Fatalf("SENT 期望 1 封邮件，实际 %d", len(items))
	}
	sent := items[0].(map[string]any)
	if hasAttach, _ := sent["has_attach"].(bool); !hasAttach {
		t.Fatalf("期望 has_attach=true，实际 %v", sent["has_attach"])
	}
	if count, _ := sent["attach_count"].(float64); count != 1 {
		t.Fatalf("期望 attach_count=1，实际 %v", sent["attach_count"])
	}
	t.Logf("附件验证通过: has_attach=true, attach_count=1")
}

// TestE2E_SendMail_Errors 测试发信错误场景。
func TestE2E_SendMail_Errors(t *testing.T) {
	env := newTestEnv(t)
	token := env.registerAndLogin(t, "err_sender", "ErrPass123!")

	// 1. 空收件人 → 400
	status, resp := env.doMultipart(t, "POST", "/api/mail/send", token,
		map[string]string{
			"to":       "",
			"subject":  "无收件人",
			"bodyText": "正文",
		}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("空收件人应返回 400，实际 %d, resp=%v", status, resp)
	}
	t.Logf("空收件人正确返回 400")

	// 2. 无效邮箱地址 → 400
	status, resp = env.doMultipart(t, "POST", "/api/mail/send", token,
		map[string]string{
			"to":       "not-an-email",
			"subject":  "无效邮箱",
			"bodyText": "正文",
		}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("无效邮箱应返回 400，实际 %d, resp=%v", status, resp)
	}
	t.Logf("无效邮箱正确返回 400")

	// 3. 超长主题 → 400
	longSubject := strings.Repeat("a", 501)
	status, _ = env.doMultipart(t, "POST", "/api/mail/send", token,
		map[string]string{
			"to":       "someone@example.com",
			"subject":  longSubject,
			"bodyText": "正文",
		}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("超长主题应返回 400，实际 %d", status)
	}
	t.Logf("超长主题正确返回 400")
}
