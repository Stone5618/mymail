// rule.go 实现 RuleDAO，对应 mail_rules 表。
//
// 用途：
//   - 用户邮件规则 CRUD（条件 + 动作以 JSON 存储）
//   - 规则引擎查询用户活跃规则（按 priority 降序）
//
// 数据模型：
//   - conditions: JSON 数组 [{field, op, value}, ...]
//   - actions: JSON 数组 [{type, folder, address}, ...]
package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// RuleCondition 规则条件。
// Field 取值：from / to / subject / has_attachment / size
// Op 取值：
//   - 字符串字段：contains / equals / ends_with / regex
//   - size 字段：gt / lt / equals
//   - has_attachment 字段：equals（value 为 true/false）
type RuleCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// RuleAction 规则动作。
// Type 取值：move / mark_read / star / delete / flag / forward
// Folder：move 动作的目标文件夹（INBOX/SENT/DRAFTS/TRASH/JUNK/ARCHIVE）
// Address：forward 动作的目标地址
// Flag：flag 动作的标志名（如 \\Flagged / \\Seen）
type RuleAction struct {
	Type    string `json:"type"`
	Folder  string `json:"folder,omitempty"`
	Address string `json:"address,omitempty"`
	Flag    string `json:"flag,omitempty"`
}

// Rule 表示 mail_rules 表的完整行。
type Rule struct {
	ID         int64           `json:"id"`
	UserID     int64           `json:"user_id"`
	Name       string          `json:"name"`
	Priority   int             `json:"priority"`
	Conditions []RuleCondition `json:"conditions"`
	Actions    []RuleAction    `json:"actions"`
	IsActive   bool            `json:"is_active"`
	CreatedAt  string          `json:"created_at"`
}

// RuleDAO 封装 mail_rules 表的所有数据库操作。
type RuleDAO struct {
	db *db.DB
}

// NewRuleDAO 创建 RuleDAO。
func NewRuleDAO(database *db.DB) *RuleDAO {
	return &RuleDAO{db: database}
}

// CreateRuleInput 创建规则的输入参数。
type CreateRuleInput struct {
	UserID     int64
	Name       string
	Priority   int
	Conditions []RuleCondition
	Actions    []RuleAction
	// IsActive 创建时默认 active=1（true）。
	// 若需创建 inactive 规则，调用 Update 方法。
}

// Create 插入新规则，返回规则 ID。
// 新建规则默认 is_active=1。
func (d *RuleDAO) Create(ctx context.Context, in CreateRuleInput) (int64, error) {
	condJSON, err := json.Marshal(in.Conditions)
	if err != nil {
		return 0, fmt.Errorf("序列化 conditions 失败: %w", err)
	}
	actJSON, err := json.Marshal(in.Actions)
	if err != nil {
		return 0, fmt.Errorf("序列化 actions 失败: %w", err)
	}

	res, err := d.db.ExecContext(ctx,
		`INSERT INTO mail_rules (user_id, name, priority, conditions, actions, is_active)
		 VALUES (?, ?, ?, ?, ?, 1)`,
		in.UserID, in.Name, in.Priority, string(condJSON), string(actJSON),
	)
	if err != nil {
		return 0, fmt.Errorf("插入规则失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取规则 ID 失败: %w", err)
	}
	return id, nil
}

// FindByID 按 ID 查询规则。
func (d *RuleDAO) FindByID(ctx context.Context, id int64) (*Rule, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, priority, conditions, actions, is_active, created_at
		 FROM mail_rules WHERE id = ?`, id)
	r, err := scanRule(row)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// FindByUserID 查询用户所有规则（按 priority 降序、created_at 降序）。
func (d *RuleDAO) FindByUserID(ctx context.Context, userID int64) ([]*Rule, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, user_id, name, priority, conditions, actions, is_active, created_at
		 FROM mail_rules WHERE user_id = ?
		 ORDER BY priority DESC, created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("查询用户规则失败: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

// FindActiveByUserID 查询用户的活跃规则（按 priority 降序）。
// 规则引擎执行时调用此方法。
func (d *RuleDAO) FindActiveByUserID(ctx context.Context, userID int64) ([]*Rule, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, user_id, name, priority, conditions, actions, is_active, created_at
		 FROM mail_rules WHERE user_id = ? AND is_active = 1
		 ORDER BY priority DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("查询活跃规则失败: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

// UpdateRuleInput 更新规则的输入参数。
// 所有字段为指针，nil 表示不更新。
type UpdateRuleInput struct {
	Name       *string
	Priority   *int
	Conditions *[]RuleCondition
	Actions    *[]RuleAction
	IsActive   *bool
}

// Update 更新规则。
func (d *RuleDAO) Update(ctx context.Context, id int64, in UpdateRuleInput) error {
	fields := make([]string, 0, 5)
	args := make([]any, 0, 6)
	if in.Name != nil {
		fields = append(fields, "name = ?")
		args = append(args, *in.Name)
	}
	if in.Priority != nil {
		fields = append(fields, "priority = ?")
		args = append(args, *in.Priority)
	}
	if in.Conditions != nil {
		condJSON, err := json.Marshal(*in.Conditions)
		if err != nil {
			return fmt.Errorf("序列化 conditions 失败: %w", err)
		}
		fields = append(fields, "conditions = ?")
		args = append(args, string(condJSON))
	}
	if in.Actions != nil {
		actJSON, err := json.Marshal(*in.Actions)
		if err != nil {
			return fmt.Errorf("序列化 actions 失败: %w", err)
		}
		fields = append(fields, "actions = ?")
		args = append(args, string(actJSON))
	}
	if in.IsActive != nil {
		fields = append(fields, "is_active = ?")
		v := 0
		if *in.IsActive {
			v = 1
		}
		args = append(args, v)
	}
	if len(fields) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := d.db.ExecContext(ctx,
		`UPDATE mail_rules SET `+joinFields(fields)+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("更新规则失败: %w", err)
	}
	return nil
}

// Delete 删除规则。
func (d *RuleDAO) Delete(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM mail_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除规则失败: %w", err)
	}
	return nil
}

// ============ 内部辅助 ============

// joinFields 用逗号连接字段（避免使用 strings.Join 引入额外 import）。
func joinFields(fields []string) string {
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += ", "
		}
		out += f
	}
	return out
}

// ruleScanner 抽象 *sql.Row 和 *sql.Rows 的 Scan 方法。
type ruleScanner interface {
	Scan(dest ...any) error
}

// scanRule 扫描单行规则数据。
func scanRule(s ruleScanner) (*Rule, error) {
	var r Rule
	var (
		condJSON  string
		actJSON   string
		isActive  int
		priority  int
	)
	if err := s.Scan(&r.ID, &r.UserID, &r.Name, &priority, &condJSON, &actJSON, &isActive, &r.CreatedAt); err != nil {
		return nil, fmt.Errorf("扫描规则行失败: %w", err)
	}
	r.Priority = priority
	r.IsActive = isActive == 1
	if err := json.Unmarshal([]byte(condJSON), &r.Conditions); err != nil {
		return nil, fmt.Errorf("解析 conditions JSON 失败: %w", err)
	}
	if err := json.Unmarshal([]byte(actJSON), &r.Actions); err != nil {
		return nil, fmt.Errorf("解析 actions JSON 失败: %w", err)
	}
	return &r, nil
}

// scanRules 扫描多行规则数据。
func scanRules(rows interface {
	Next() bool
	Scan(dest ...any) error
}) ([]*Rule, error) {
	var rules []*Rule
	for {
		if !rows.Next() {
			break
		}
		r, err := scanRule(rows.(ruleScanner))
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}
