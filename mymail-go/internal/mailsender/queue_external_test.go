// queue_external_test.go 测试队列 worker 外域发送分支（阶段 4）。
//
// 覆盖：
//   - 外域收件人通过 Sender 接口发送（成功 → MarkSent）
//   - 外域发送失败 → MarkFailed
//   - 本域 + 外域混合收件人
//   - smtpSender=nil 时跳过外域发送（仅 warn）
package mailsender

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// mockSender 模拟邮件发送器，记录调用次数与输入。
type mockSender struct {
	calls int
	err   error // 返回的错误（nil 表示成功）
	last  SendInput
}

func (m *mockSender) Send(ctx context.Context, in SendInput) error {
	m.calls++
	m.last = in
	return m.err
}

// TestQueueWorker_ExternalRecipient_SendSuccess 验证外域收件人发送成功。
func TestQueueWorker_ExternalRecipient_SendSuccess(t *testing.T) {
	env := newMailsenderTestEnv(t)
	ms := &mockSender{}
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, ms)

	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:   1,
		FromAddr: "alice@example.com",
		ToAddrs:  "bob@external.org", // 外域
		Subject:  "外域测试",
		BodyText: "hello",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	if !worker.ProcessOnce() {
		t.Fatal("ProcessOnce 应返回 true")
	}

	if ms.calls != 1 {
		t.Errorf("mockSender 应被调用 1 次，实际 %d", ms.calls)
	}
	if len(ms.last.To) != 1 || ms.last.To[0] != "bob@external.org" {
		t.Errorf("SendInput.To 应为 [bob@external.org]，实际 %v", ms.last.To)
	}
	if ms.last.Subject != "外域测试" {
		t.Errorf("Subject 期望 '外域测试'，实际 %s", ms.last.Subject)
	}

	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueueSent {
		t.Errorf("外域发送成功后状态应为 sent，实际 %s", item.Status)
	}
}

// TestQueueWorker_ExternalRecipient_SendFailure 验证外域发送失败 → MarkFailed。
// 设置 MaxAttempts=1 使首次失败即达终态 failed（否则会进入 pending 重试等待）。
func TestQueueWorker_ExternalRecipient_SendFailure(t *testing.T) {
	env := newMailsenderTestEnv(t)
	ms := &mockSender{err: errors.New("smtp connection refused")}
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, ms)

	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:      1,
		FromAddr:    "alice@example.com",
		ToAddrs:     "bob@external.org",
		Subject:     "失败测试",
		BodyText:    "x",
		MaxAttempts: 1, // 首次失败即终态
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	worker.ProcessOnce()

	if ms.calls != 1 {
		t.Errorf("mockSender 应被调用 1 次，实际 %d", ms.calls)
	}

	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueueFailed {
		t.Errorf("外域发送失败后状态应为 failed，实际 %s", item.Status)
	}
	if item.ErrorMsg == "" {
		t.Error("失败时应记录 error_msg")
	}
}

// TestQueueWorker_ExternalRecipient_SendFailure_Retry 验证外域发送失败但未达 max_attempts → pending 重试。
func TestQueueWorker_ExternalRecipient_SendFailure_Retry(t *testing.T) {
	env := newMailsenderTestEnv(t)
	ms := &mockSender{err: errors.New("smtp timeout")}
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 5, ms) // retryMin=5

	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:      1,
		FromAddr:    "alice@example.com",
		ToAddrs:     "bob@external.org",
		Subject:     "重试测试",
		BodyText:    "x",
		MaxAttempts: 3, // 默认值，首次失败后应 pending 等待重试
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	worker.ProcessOnce()

	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueuePending {
		t.Errorf("未达 max_attempts 时状态应为 pending（等待重试），实际 %s", item.Status)
	}
	if item.ErrorMsg == "" {
		t.Error("失败时应记录 error_msg")
	}
}

// TestQueueWorker_MixedRecipients 验证本域 + 外域混合收件人。
// 本域 alice 通过 LocalDeliverer 投递，外域 bob@external.org 通过 SMTPSender 发送。
func TestQueueWorker_MixedRecipients(t *testing.T) {
	env := newMailsenderTestEnv(t)
	ms := &mockSender{}
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, ms)

	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:   1,
		FromAddr: "sender@external.org",
		ToAddrs:  "alice@example.com, bob@external.org",
		Subject:  "混合收件人",
		BodyText: "x",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	worker.ProcessOnce()

	// 外域 1 个，应调用 1 次
	if ms.calls != 1 {
		t.Errorf("mockSender 应被调用 1 次（仅外域），实际 %d", ms.calls)
	}
	if len(ms.last.To) != 1 || ms.last.To[0] != "bob@external.org" {
		t.Errorf("SendInput.To 应为 [bob@external.org]，实际 %v", ms.last.To)
	}

	// 本域 alice 应收到邮件
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	list, _ := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if len(list.Messages) != 1 {
		t.Errorf("alice 应收到 1 封邮件，实际 %d", len(list.Messages))
	}

	// 队列状态为 sent（两者都成功）
	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueueSent {
		t.Errorf("混合收件人全部成功后状态应为 sent，实际 %s", item.Status)
	}
}

// TestQueueWorker_ExternalRecipient_NilSender 验证 smtpSender=nil 时跳过外域发送。
// 外域收件人存在但无 sender，应仅 warn 不失败，队列仍标记 sent（降级）。
func TestQueueWorker_ExternalRecipient_NilSender(t *testing.T) {
	env := newMailsenderTestEnv(t)
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, nil)

	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:   1,
		FromAddr: "alice@example.com",
		ToAddrs:  "bob@external.org",
		Subject:  "nil sender",
		BodyText: "x",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	worker.ProcessOnce()

	// smtpSender=nil：跳过外域发送，但本地投递无本域收件人，仍标记 sent
	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueueSent {
		t.Errorf("nil sender 时应降级标记 sent，实际 %s", item.Status)
	}
}

// TestQueueWorker_AllExternal_MultipleRecipients 验证多个外域收件人一次性发送。
func TestQueueWorker_AllExternal_MultipleRecipients(t *testing.T) {
	env := newMailsenderTestEnv(t)
	ms := &mockSender{}
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, ms)

	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:   1,
		FromAddr: "alice@example.com",
		ToAddrs:  "bob@external.org, charlie@other.net",
		Subject:  "多外域",
		BodyText: "x",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	worker.ProcessOnce()

	// 2 个外域收件人应一次性传入 SendInput.To
	if ms.calls != 1 {
		t.Errorf("mockSender 应被调用 1 次，实际 %d", ms.calls)
	}
	if len(ms.last.To) != 2 {
		t.Errorf("SendInput.To 应有 2 个收件人，实际 %d (%v)", len(ms.last.To), ms.last.To)
	}

	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueueSent {
		t.Errorf("多外域发送成功后状态应为 sent，实际 %s", item.Status)
	}
}
