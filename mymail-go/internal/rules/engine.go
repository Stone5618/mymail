// engine.go 实现规则引擎主流程。
//
// 流程：
//   1. 加载用户活跃规则（按 priority 降序）
//   2. 依次匹配每条规则的所有条件（AND 语义）
//   3. 首个匹配的规则执行所有动作（与原 Node.js 一致：break after first match）
//   4. 单个动作失败不影响其他动作
//
// 与原 Node.js rule-engine.js 的差异：
//   - P1-7：Go regexp 基于 RE2，防 ReDoS
//   - flag/forward 动作完整实现（原 Node.js 是占位）
//   - 错误隔离：单个动作失败不中断后续动作
package rules

import (
	"context"
	"log/slog"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// Engine 规则引擎。
type Engine struct {
	ruleDAO  *dao.RuleDAO
	matcher  *Matcher
	executor *ActionExecutor
}

// NewEngine 创建规则引擎。
//
// 参数：
//   - ruleDAO: 规则 DAO（查询用户规则）
//   - matcher: 条件匹配器
//   - executor: 动作执行器
func NewEngine(ruleDAO *dao.RuleDAO, matcher *Matcher, executor *ActionExecutor) *Engine {
	return &Engine{
		ruleDAO:  ruleDAO,
		matcher:  matcher,
		executor: executor,
	}
}

// RunForMessage 对邮件执行用户规则。
//
// 行为：
//   - 加载用户活跃规则（按 priority 降序）
//   - 依次匹配，首个匹配的规则执行所有动作后返回
//   - 无匹配规则：无操作
//   - 加载规则失败：记录日志，不返回错误（不阻断邮件投递）
func (e *Engine) RunForMessage(ctx context.Context, userID int64, msg MessageView) error {
	rules, err := e.ruleDAO.FindActiveByUserID(ctx, userID)
	if err != nil {
		slog.Error("加载用户规则失败",
			"user_id", userID,
			"msg_id", msg.ID,
			"error", err,
		)
		return nil // 不阻断邮件投递
	}
	if len(rules) == 0 {
		return nil
	}

	for _, rule := range rules {
		if e.matcher.MatchAll(rule.Conditions, msg) {
			slog.Info("规则匹配",
				"rule_id", rule.ID,
				"rule_name", rule.Name,
				"msg_id", msg.ID,
				"user_id", userID,
			)
			errs := e.executor.ExecuteAll(ctx, rule.Actions, msg)
			if len(errs) > 0 {
				slog.Warn("规则动作部分失败",
					"rule_id", rule.ID,
					"msg_id", msg.ID,
					"error_count", len(errs),
				)
			}
			// 首个匹配的规则执行后返回（与原 Node.js 一致）
			return nil
		}
	}
	return nil
}
