// action.go 实现规则动作执行。
//
// 动作类型（与原 Node.js rule-engine.js 兼容）：
//   - move: 移动到指定文件夹
//   - mark_read: 标记已读
//   - star: 加星
//   - delete: 移到回收站（TRASH）
//   - flag: 设置标志（P1-7 修复：原 Node.js 占位，Go 版完整实现）
//   - forward: 转发邮件（P1-7 修复：原 Node.js 占位，Go 版完整实现）
//
// 设计：
//   - 通过依赖注入接口避免循环依赖（rules 不直接依赖 service 包）
//   - MessageActionApplier 接口由 dao.MessageDAO 实现
//   - Forwarder 接口由 service.MailService 实现（外部注入）
package rules

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// 动作类型常量。
const (
	ActionMove     = "move"
	ActionMarkRead = "mark_read"
	ActionStar     = "star"
	ActionDelete   = "delete"
	ActionFlag     = "flag"
	ActionForward  = "forward"
)

// MessageActionApplier 邮件动作应用接口。
// *dao.MessageDAO 自动满足此接口。
type MessageActionApplier interface {
	MoveToFolder(ctx context.Context, id, userID int64, folder string) error
	MarkRead(ctx context.Context, id, userID int64) error
	ToggleStar(ctx context.Context, id, userID int64) error
	UpdateFlags(ctx context.Context, id, userID int64, flags string) error
}

// Forwarder 转发邮件接口（由 service.MailService 实现，避免 rules 依赖 service 包）。
type Forwarder interface {
	ForwardMessage(ctx context.Context, userID int64, msg MessageView, toAddr string) error
}

// ActionExecutor 动作执行器。
type ActionExecutor struct {
	msgApplier MessageActionApplier
	forwarder  Forwarder // 可空（nil 时 forward 动作只记录日志）
}

// NewActionExecutor 创建动作执行器。
//
// 参数：
//   - msgApplier: 邮件动作应用器（通常为 *dao.MessageDAO）
//   - forwarder: 转发器（可空，nil 时 forward 动作只记录日志）
func NewActionExecutor(msgApplier MessageActionApplier, forwarder Forwarder) *ActionExecutor {
	return &ActionExecutor{
		msgApplier: msgApplier,
		forwarder:  forwarder,
	}
}

// ExecuteAll 执行所有动作。
// 单个动作失败不影响其他动作（与原 Node.js 行为一致）。
// 返回所有发生的错误（可能为空）。
func (e *ActionExecutor) ExecuteAll(ctx context.Context, actions []dao.RuleAction, msg MessageView) []error {
	var errs []error
	for _, action := range actions {
		if err := e.ExecuteAction(ctx, action, msg); err != nil {
			slog.Warn("规则动作执行失败",
				"action", action.Type,
				"msg_id", msg.ID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	return errs
}

// ExecuteAction 执行单个动作。
func (e *ActionExecutor) ExecuteAction(ctx context.Context, action dao.RuleAction, msg MessageView) error {
	switch action.Type {
	case ActionMove:
		if action.Folder == "" {
			return fmt.Errorf("move 动作缺少 folder 参数")
		}
		return e.msgApplier.MoveToFolder(ctx, msg.ID, msg.UserID, action.Folder)

	case ActionMarkRead:
		return e.msgApplier.MarkRead(ctx, msg.ID, msg.UserID)

	case ActionStar:
		// 仅当未加星时加星（与原 Node.js 一致）
		if msg.IsStarred {
			return nil
		}
		return e.msgApplier.ToggleStar(ctx, msg.ID, msg.UserID)

	case ActionDelete:
		return e.msgApplier.MoveToFolder(ctx, msg.ID, msg.UserID, "TRASH")

	case ActionFlag:
		// P1-7 修复：原 Node.js 是占位，Go 版完整实现
		if action.Flag == "" {
			return fmt.Errorf("flag 动作缺少 flag 参数")
		}
		return e.msgApplier.UpdateFlags(ctx, msg.ID, msg.UserID, action.Flag)

	case ActionForward:
		// P1-7 修复：原 Node.js 是占位，Go 版完整实现
		if action.Address == "" {
			return fmt.Errorf("forward 动作缺少 address 参数")
		}
		if e.forwarder == nil {
			// 未注入 Forwarder：只记录日志（兼容性降级）
			slog.Info("forward 动作未注入 Forwarder，跳过实际转发",
				"msg_id", msg.ID,
				"to", action.Address,
			)
			return nil
		}
		return e.forwarder.ForwardMessage(ctx, msg.UserID, msg, action.Address)

	default:
		return fmt.Errorf("未知动作类型: %s", action.Type)
	}
}
