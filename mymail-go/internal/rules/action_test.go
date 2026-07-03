// action_test.go 测试规则动作执行。
//
// 测试覆盖：
//   - 6 种动作（move/mark_read/star/delete/flag/forward）
//   - 参数缺失时的错误处理（move 缺 folder / flag 缺 flag / forward 缺 address）
//   - nil Forwarder 降级（只记录日志，不报错）
//   - star 已加星时不调用 ToggleStar
//   - ExecuteAll 单个失败不影响其他动作
//   - Forwarder 错误向上传播
package rules

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// mockActionApplier mock MessageActionApplier，记录所有调用并可强制返回错误。
type mockActionApplier struct {
	mu            sync.Mutex
	moveCalls     []moveCall
	markReadCalls []int64
	toggleCalls   []int64
	flagCalls     []flagCall
	moveErr       error
	markReadErr   error
	toggleErr     error
	flagErr       error
}

type moveCall struct {
	id, userID int64
	folder     string
}

type flagCall struct {
	id, userID int64
	flags      string
}

func (m *mockActionApplier) MoveToFolder(ctx context.Context, id, userID int64, folder string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.moveCalls = append(m.moveCalls, moveCall{id, userID, folder})
	if m.moveErr != nil {
		return m.moveErr
	}
	return nil
}

func (m *mockActionApplier) MarkRead(ctx context.Context, id, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markReadCalls = append(m.markReadCalls, id)
	if m.markReadErr != nil {
		return m.markReadErr
	}
	return nil
}

func (m *mockActionApplier) ToggleStar(ctx context.Context, id, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toggleCalls = append(m.toggleCalls, id)
	if m.toggleErr != nil {
		return m.toggleErr
	}
	return nil
}

func (m *mockActionApplier) UpdateFlags(ctx context.Context, id, userID int64, flags string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flagCalls = append(m.flagCalls, flagCall{id, userID, flags})
	if m.flagErr != nil {
		return m.flagErr
	}
	return nil
}

// mockForwarder mock Forwarder，记录调用并可强制返回错误。
type mockForwarder struct {
	mu     sync.Mutex
	calls  []forwardCall
	fwdErr error
}

type forwardCall struct {
	userID int64
	msg    MessageView
	toAddr string
}

func (m *mockForwarder) ForwardMessage(ctx context.Context, userID int64, msg MessageView, toAddr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, forwardCall{userID, msg, toAddr})
	if m.fwdErr != nil {
		return m.fwdErr
	}
	return nil
}

func TestActionExecutor_Move(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionMove, Folder: "JUNK",
	}, msg)
	if err != nil {
		t.Fatalf("move 动作应成功: %v", err)
	}
	if len(applier.moveCalls) != 1 {
		t.Fatalf("move 应调用 1 次，实际 %d", len(applier.moveCalls))
	}
	if applier.moveCalls[0].folder != "JUNK" {
		t.Errorf("folder 期望 JUNK，实际 %s", applier.moveCalls[0].folder)
	}
	if applier.moveCalls[0].id != msg.ID || applier.moveCalls[0].userID != msg.UserID {
		t.Errorf("move 调用参数错误: id=%d, userID=%d", applier.moveCalls[0].id, applier.moveCalls[0].userID)
	}
}

func TestActionExecutor_Move_EmptyFolder(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionMove,
	}, msg)
	if err == nil {
		t.Error("move 缺 folder 应返回错误")
	}
}

func TestActionExecutor_MarkRead(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionMarkRead,
	}, msg)
	if err != nil {
		t.Fatalf("mark_read 动作应成功: %v", err)
	}
	if len(applier.markReadCalls) != 1 {
		t.Errorf("mark_read 应调用 1 次，实际 %d", len(applier.markReadCalls))
	}
}

func TestActionExecutor_Star_NotStarred(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()
	msg.IsStarred = false

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionStar,
	}, msg)
	if err != nil {
		t.Fatalf("star 动作应成功: %v", err)
	}
	if len(applier.toggleCalls) != 1 {
		t.Errorf("未加星时应调用 ToggleStar 1 次，实际 %d", len(applier.toggleCalls))
	}
}

func TestActionExecutor_Star_AlreadyStarred(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()
	msg.IsStarred = true

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionStar,
	}, msg)
	if err != nil {
		t.Fatalf("star 动作应成功: %v", err)
	}
	if len(applier.toggleCalls) != 0 {
		t.Errorf("已加星时不应调用 ToggleStar，实际调用 %d 次", len(applier.toggleCalls))
	}
}

func TestActionExecutor_Delete(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionDelete,
	}, msg)
	if err != nil {
		t.Fatalf("delete 动作应成功: %v", err)
	}
	if len(applier.moveCalls) != 1 {
		t.Fatalf("delete 应调用 MoveToFolder 1 次，实际 %d", len(applier.moveCalls))
	}
	if applier.moveCalls[0].folder != "TRASH" {
		t.Errorf("delete 应移动到 TRASH，实际 %s", applier.moveCalls[0].folder)
	}
}

func TestActionExecutor_Flag(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionFlag, Flag: "\\Flagged",
	}, msg)
	if err != nil {
		t.Fatalf("flag 动作应成功: %v", err)
	}
	if len(applier.flagCalls) != 1 {
		t.Fatalf("flag 应调用 1 次，实际 %d", len(applier.flagCalls))
	}
	if applier.flagCalls[0].flags != "\\Flagged" {
		t.Errorf("flag 期望 \\Flagged，实际 %s", applier.flagCalls[0].flags)
	}
}

func TestActionExecutor_Flag_EmptyFlag(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionFlag,
	}, msg)
	if err == nil {
		t.Error("flag 缺 flag 参数应返回错误")
	}
}

func TestActionExecutor_Forward(t *testing.T) {
	applier := &mockActionApplier{}
	fwd := &mockForwarder{}
	exec := NewActionExecutor(applier, fwd)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionForward, Address: "boss@example.com",
	}, msg)
	if err != nil {
		t.Fatalf("forward 动作应成功: %v", err)
	}
	if len(fwd.calls) != 1 {
		t.Fatalf("forward 应调用 1 次，实际 %d", len(fwd.calls))
	}
	if fwd.calls[0].toAddr != "boss@example.com" {
		t.Errorf("forward 目标地址期望 boss@example.com，实际 %s", fwd.calls[0].toAddr)
	}
}

func TestActionExecutor_Forward_EmptyAddress(t *testing.T) {
	applier := &mockActionApplier{}
	fwd := &mockForwarder{}
	exec := NewActionExecutor(applier, fwd)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionForward,
	}, msg)
	if err == nil {
		t.Error("forward 缺 address 应返回错误")
	}
}

func TestActionExecutor_Forward_NilForwarder(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil) // forwarder 为 nil
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionForward, Address: "boss@example.com",
	}, msg)
	if err != nil {
		t.Errorf("nil Forwarder 时应降级为日志，不返回错误，实际: %v", err)
	}
}

func TestActionExecutor_Forward_PropagatesError(t *testing.T) {
	applier := &mockActionApplier{}
	fwd := &mockForwarder{fwdErr: errors.New("forward failed")}
	exec := NewActionExecutor(applier, fwd)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: ActionForward, Address: "boss@example.com",
	}, msg)
	if err == nil {
		t.Error("forward 失败应返回错误")
	}
}

func TestActionExecutor_UnknownAction(t *testing.T) {
	applier := &mockActionApplier{}
	exec := NewActionExecutor(applier, nil)
	msg := baseMsg()

	err := exec.ExecuteAction(context.Background(), dao.RuleAction{
		Type: "unknown_action",
	}, msg)
	if err == nil {
		t.Error("未知动作类型应返回错误")
	}
}

func TestActionExecutor_ExecuteAll_AllSuccess(t *testing.T) {
	applier := &mockActionApplier{}
	fwd := &mockForwarder{}
	exec := NewActionExecutor(applier, fwd)
	msg := baseMsg()

	actions := []dao.RuleAction{
		{Type: ActionMarkRead},
		{Type: ActionStar},
		{Type: ActionFlag, Flag: "\\Seen"},
	}
	errs := exec.ExecuteAll(context.Background(), actions, msg)
	if len(errs) != 0 {
		t.Errorf("全部成功应无错误，实际 %d 个", len(errs))
	}
}

func TestActionExecutor_ExecuteAll_SingleFailureContinues(t *testing.T) {
	applier := &mockActionApplier{
		markReadErr: errors.New("mark_read failed"),
	}
	fwd := &mockForwarder{}
	exec := NewActionExecutor(applier, fwd)
	msg := baseMsg()

	actions := []dao.RuleAction{
		{Type: ActionMarkRead},               // 失败
		{Type: ActionStar},                   // 应继续执行
		{Type: ActionFlag, Flag: "\\Seen"},   // 应继续执行
	}
	errs := exec.ExecuteAll(context.Background(), actions, msg)
	if len(errs) != 1 {
		t.Fatalf("应有 1 个错误，实际 %d", len(errs))
	}
	if len(applier.toggleCalls) != 1 {
		t.Errorf("mark_read 失败后 star 仍应执行，toggle 调用 %d 次", len(applier.toggleCalls))
	}
	if len(applier.flagCalls) != 1 {
		t.Errorf("mark_read 失败后 flag 仍应执行，flag 调用 %d 次", len(applier.flagCalls))
	}
}
