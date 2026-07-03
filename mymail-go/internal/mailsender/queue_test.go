// queue_test.go 测试持久化队列 worker。
//
// 测试覆盖：
//   - QueueWorker.ProcessOnce 端到端：入队 → 处理 → 标记 sent
//   - QueueWorker.Start/Stop 生命周期
//   - recoverStale 崩溃恢复
//   - 空队列处理
package mailsender

import (
	"context"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// ============ QueueWorker 测试 ============

// TestQueueWorker_ProcessOnce_Success 验证单次处理成功。
func TestQueueWorker_ProcessOnce_Success(t *testing.T) {
	env := newMailsenderTestEnv(t)
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, nil)

	// 入队一条邮件
	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:   1,
		FromAddr: "sender@external.org",
		ToAddrs:  "alice@example.com",
		Subject:  "队列测试",
		BodyText: "正文",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	// 处理一次
	if !worker.ProcessOnce() {
		t.Fatal("ProcessOnce 应返回 true（处理了一项）")
	}

	// 验证状态为 sent
	item, err := env.queueDAO.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("查询队列项失败: %v", err)
	}
	if item.Status != dao.QueueSent {
		t.Errorf("状态期望 sent，实际 %s", item.Status)
	}

	// 验证 alice 收到邮件
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	list, _ := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if len(list.Messages) != 1 {
		t.Errorf("alice 应收到 1 封邮件，实际 %d", len(list.Messages))
	}
	if len(list.Messages) > 0 && list.Messages[0].Subject != "队列测试" {
		t.Errorf("subject 期望 '队列测试'，实际 %s", list.Messages[0].Subject)
	}
}

// TestQueueWorker_ProcessOnce_Empty 验证空队列返回 false。
func TestQueueWorker_ProcessOnce_Empty(t *testing.T) {
	env := newMailsenderTestEnv(t)
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, nil)

	if worker.ProcessOnce() {
		t.Error("空队列 ProcessOnce 应返回 false")
	}
}

// TestQueueWorker_StartStop 验证 Start/Stop 生命周期。
func TestQueueWorker_StartStop(t *testing.T) {
	env := newMailsenderTestEnv(t)
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, nil)

	worker.Start()
	// 短暂运行
	time.Sleep(200 * time.Millisecond)
	worker.Stop()
}

// TestQueueWorker_Start_AutoProcess 验证 Start 后自动处理队列项。
func TestQueueWorker_Start_AutoProcess(t *testing.T) {
	env := newMailsenderTestEnv(t)
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, nil)

	// 入队
	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:   1,
		FromAddr: "sender@external.org",
		ToAddrs:  "alice@example.com",
		Subject:  "自动处理",
		BodyText: "x",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	worker.Start()
	defer worker.Stop()

	// 等待 worker 轮询处理（轮询间隔 50ms，等待 1s 足够）
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		item, _ := env.queueDAO.FindByID(context.Background(), id)
		if item != nil && item.Status == dao.QueueSent {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item == nil || item.Status != dao.QueueSent {
		t.Errorf("队列项应被自动处理为 sent，实际 status=%v", item)
	}
}

// TestQueueWorker_RecoverStale 验证崩溃恢复：卡死的 sending 被重置为 pending。
func TestQueueWorker_RecoverStale(t *testing.T) {
	env := newMailsenderTestEnv(t)

	// 入队一条
	id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
		UserID:   1,
		FromAddr: "sender@external.org",
		ToAddrs:  "alice@example.com",
		Subject:  "崩溃恢复",
		BodyText: "x",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	// 手动模拟卡死：Claim 后不处理（状态变 sending）
	if _, err := env.queueDAO.ClaimNextPending(context.Background()); err != nil {
		t.Fatalf("Claim 失败: %v", err)
	}

	// 验证当前为 sending
	item, _ := env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueueSending {
		t.Fatalf("期望 sending，实际 %s", item.Status)
	}

	// 手动将 created_at 设为 15 分钟前，模拟卡死已久的项
	if _, err := env.database.ExecContext(context.Background(),
		`UPDATE mail_queue SET created_at = datetime('now', '-15 minutes') WHERE id = ?`, id); err != nil {
		t.Fatalf("更新 created_at 失败: %v", err)
	}

	// 调用 recoverStale（阈值 10 分钟，应能重置）
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, nil)
	worker.recoverStale()

	item, _ = env.queueDAO.FindByID(context.Background(), id)
	if item.Status != dao.QueuePending {
		t.Errorf("期望 pending（已恢复），实际 %s", item.Status)
	}
}

// TestQueueWorker_MultipleItems 验证连续处理多个队列项。
func TestQueueWorker_MultipleItems(t *testing.T) {
	env := newMailsenderTestEnv(t)
	worker := NewQueueWorker(env.local, env.queueDAO, 50*time.Millisecond, 1, nil)

	// 入队 3 条
	var ids []int64
	for i := 0; i < 3; i++ {
		id, err := env.queueDAO.Enqueue(context.Background(), dao.EnqueueInput{
			UserID:   1,
			FromAddr: "sender@external.org",
			ToAddrs:  "alice@example.com",
			Subject:  "多队列项",
			BodyText: "x",
		})
		if err != nil {
			t.Fatalf("入队 %d 失败: %v", i, err)
		}
		ids = append(ids, id)
	}

	// 连续处理
	count := 0
	for i := 0; i < 5; i++ { // 多调一次确保空队列
		if worker.ProcessOnce() {
			count++
		}
	}

	if count != 3 {
		t.Errorf("应处理 3 项，实际 %d", count)
	}

	// 全部应为 sent
	for _, id := range ids {
		item, _ := env.queueDAO.FindByID(context.Background(), id)
		if item.Status != dao.QueueSent {
			t.Errorf("队列项 %d 状态期望 sent，实际 %s", id, item.Status)
		}
	}

	// alice 应收到 3 封邮件
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	list, _ := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if len(list.Messages) != 3 {
		t.Errorf("alice 应收到 3 封邮件，实际 %d", len(list.Messages))
	}
}
