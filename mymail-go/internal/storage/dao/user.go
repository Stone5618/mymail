// Package dao 提供数据访问对象，封装 SQL 操作。
// 修复 P1-1：关键多步操作支持事务（通过 db.RunInTransaction）。
package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// User 表示 users 表的完整行。
// 字段名与数据库列名一致（snake_case）。
type User struct {
	ID                int64
	Username          string
	Email             string
	PasswordHash      string
	DovecotPasswordHash string
	DisplayName       string // 可空，但 Scan 时若 NULL 则保持空字符串
	Role              string
	StorageLimit      int64
	StorageUsed       int64
	IsActive          bool
	LoginFails        int
	LockedUntil       sql.NullTime
	IsDefaultPassword bool
	Signature         sql.NullString
	Preferences       string // JSON 字符串，默认 '{}'
	CreatedAt         string
	UpdatedAt         string
}

// userColumns 是 users 表的所有列（顺序须与 scanUser 一致）。
const userColumns = `id, username, email, password_hash, dovecot_password_hash, display_name, role,
	storage_limit, storage_used, is_active, login_fails, locked_until,
	is_default_password, signature, preferences, created_at, updated_at`

// scanUser 将一行数据扫描到 User。
func scanUser(s interface {
	Scan(dest ...any) error
}) (*User, error) {
	var u User
	var displayName sql.NullString
	var preferences sql.NullString
	var isActive int
	var isDefault int
	err := s.Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.DovecotPasswordHash, &displayName, &u.Role,
		&u.StorageLimit, &u.StorageUsed, &isActive, &u.LoginFails, &u.LockedUntil,
		&isDefault, &u.Signature, &preferences, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.DisplayName = displayName.String
	u.Preferences = preferences.String
	u.IsActive = isActive == 1
	u.IsDefaultPassword = isDefault == 1
	return &u, nil
}

// UserDAO 封装 users 表的所有数据库操作。
type UserDAO struct {
	db *db.DB
}

// NewUserDAO 创建 UserDAO。
func NewUserDAO(database *db.DB) *UserDAO {
	return &UserDAO{db: database}
}

// FindByID 根据 ID 查询用户（返回完整行）。
func (d *UserDAO) FindByID(ctx context.Context, id int64) (*User, error) {
	q := fmt.Sprintf("SELECT %s FROM users WHERE id = ?", userColumns)
	row := d.db.QueryRowContext(ctx, q, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询用户 by id 失败: %w", err)
	}
	return u, nil
}

// FindByEmail 根据邮箱查询用户。
func (d *UserDAO) FindByEmail(ctx context.Context, email string) (*User, error) {
	q := fmt.Sprintf("SELECT %s FROM users WHERE email = ?", userColumns)
	row := d.db.QueryRowContext(ctx, q, email)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询用户 by email 失败: %w", err)
	}
	return u, nil
}

// FindByUsername 根据用户名查询用户。
func (d *UserDAO) FindByUsername(ctx context.Context, username string) (*User, error) {
	q := fmt.Sprintf("SELECT %s FROM users WHERE username = ?", userColumns)
	row := d.db.QueryRowContext(ctx, q, username)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询用户 by username 失败: %w", err)
	}
	return u, nil
}

// CreateUserInput 创建用户的输入参数。
type CreateUserInput struct {
	Username     string
	Email        string
	PasswordHash string
	DovecotPasswordHash string
	DisplayName  string // 空则用 username
	Role         string // 空则 "user"
	StorageLimit int64  // 0 表示使用默认值 100MB（P1-14 修复：管理员创建用户直接设置配额）
}

// DefaultStorageLimit 默认存储配额（100MB），与 001_initial_schema 一致。
const DefaultStorageLimit int64 = 104857600

// Create 插入新用户，返回新用户 ID。
//
// P1-14 修复：直接在 INSERT 中设置 storage_limit，无需后续 updateStorageUsed 调用。
func (d *UserDAO) Create(ctx context.Context, in CreateUserInput) (int64, error) {
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	if in.Role == "" {
		in.Role = "user"
	}
	if in.StorageLimit == 0 {
		in.StorageLimit = DefaultStorageLimit
	}
	const q = `INSERT INTO users (username, email, password_hash, dovecot_password_hash, display_name, role, storage_limit)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := d.db.ExecContext(ctx, q, in.Username, in.Email, in.PasswordHash, in.DovecotPasswordHash, in.DisplayName, in.Role, in.StorageLimit)
	if err != nil {
		return 0, fmt.Errorf("创建用户失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取用户 ID 失败: %w", err)
	}
	return id, nil
}

// UpdateStorageLimit 更新用户存储配额（管理员操作）。
func (d *UserDAO) UpdateStorageLimit(ctx context.Context, userID int64, limit int64) error {
	const q = `UPDATE users SET storage_limit = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, limit, userID)
	if err != nil {
		return fmt.Errorf("更新 storage_limit 失败: %w", err)
	}
	return nil
}

// UpdateRole 更新用户角色（管理员操作）。
func (d *UserDAO) UpdateRole(ctx context.Context, userID int64, role string) error {
	const q = `UPDATE users SET role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, role, userID)
	if err != nil {
		return fmt.Errorf("更新 role 失败: %w", err)
	}
	return nil
}

// UpdateLoginFails 更新登录失败次数。
func (d *UserDAO) UpdateLoginFails(ctx context.Context, userID int64, fails int) error {
	const q = `UPDATE users SET login_fails = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, fails, userID)
	if err != nil {
		return fmt.Errorf("更新 login_fails 失败: %w", err)
	}
	return nil
}

// LockUser 锁定用户至指定时间。
// until 为字符串形式的 DATETIME（YYYY-MM-DD HH:MM:SS），传 NULL 表示解锁。
func (d *UserDAO) LockUser(ctx context.Context, userID int64, until string) error {
	const q = `UPDATE users SET locked_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, until, userID)
	if err != nil {
		return fmt.Errorf("锁定用户失败: %w", err)
	}
	return nil
}

// ResetLoginFails 重置登录失败计数与锁定时间（密码正确后调用）。
// 修复 P1-1：使用事务保证原子性。
func (d *UserDAO) ResetLoginFails(ctx context.Context, userID int64) error {
	return d.db.RunInTransaction(ctx, func(tx *sql.Tx) error {
		const q = `UPDATE users SET login_fails = 0, locked_until = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
		if _, err := tx.ExecContext(ctx, q, userID); err != nil {
			return fmt.Errorf("重置 login_fails 失败: %w", err)
		}
		return nil
	})
}

// UpdateProfileInput 更新个人资料输入。
type UpdateProfileInput struct {
	DisplayName *string // nil 表示不更新
	Signature   *string // nil 表示不更新
	Preferences *string // nil 表示不更新（JSON 字符串）
}

// UpdateProfile 动态更新个人资料字段。
func (d *UserDAO) UpdateProfile(ctx context.Context, userID int64, in UpdateProfileInput) error {
	fields := []string{}
	args := []any{}
	if in.DisplayName != nil {
		fields = append(fields, "display_name = ?")
		args = append(args, *in.DisplayName)
	}
	if in.Signature != nil {
		fields = append(fields, "signature = ?")
		args = append(args, *in.Signature)
	}
	if in.Preferences != nil {
		fields = append(fields, "preferences = ?")
		args = append(args, *in.Preferences)
	}
	if len(fields) == 0 {
		return nil
	}
	fields = append(fields, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, userID)
	q := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(fields, ", "))
	if _, err := d.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("更新 profile 失败: %w", err)
	}
	return nil
}

// UpdatePreferences 更新用户偏好设置（JSON 字符串）。
func (d *UserDAO) UpdatePreferences(ctx context.Context, userID int64, preferences string) error {
	const q = `UPDATE users SET preferences = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, preferences, userID)
	if err != nil {
		return fmt.Errorf("更新 preferences 失败: %w", err)
	}
	return nil
}

// UpdatePassword 更新密码哈希。
// UpdatePasswordAndDovecot 同时更新 bcrypt 和 dovecot 密码哈希。
func (d *UserDAO) UpdatePasswordAndDovecot(ctx context.Context, userID int64, passwordHash string, dovecotHash string) error {
	const q = `UPDATE users SET password_hash = ?, dovecot_password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, passwordHash, dovecotHash, userID)
	if err != nil {
		return fmt.Errorf("更新 password 失败: %w", err)
	}
	return nil
}

// SetDefaultPassword 设置/清除默认密码标志。
// value=true 表示是默认密码（需强制改密），false 表示已修改。
func (d *UserDAO) SetDefaultPassword(ctx context.Context, userID int64, value bool) error {
	v := 0
	if value {
		v = 1
	}
	const q = `UPDATE users SET is_default_password = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, v, userID)
	if err != nil {
		return fmt.Errorf("更新 is_default_password 失败: %w", err)
	}
	return nil
}

// UpdateStorageUsed 增量更新已用存储（可为负数，表示释放）。
func (d *UserDAO) UpdateStorageUsed(ctx context.Context, userID int64, delta int64) error {
	const q = `UPDATE users SET storage_used = storage_used + ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, delta, userID)
	if err != nil {
		return fmt.Errorf("更新 storage_used 失败: %w", err)
	}
	return nil
}

// SetActive 启用/禁用用户。
func (d *UserDAO) SetActive(ctx context.Context, userID int64, active bool) error {
	v := 0
	if active {
		v = 1
	}
	const q = `UPDATE users SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, v, userID)
	if err != nil {
		return fmt.Errorf("更新 is_active 失败: %w", err)
	}
	return nil
}

// ListAll 查询所有用户（按 ID 升序），管理员后台用。
func (d *UserDAO) ListAll(ctx context.Context) ([]*User, error) {
	q := fmt.Sprintf("SELECT %s FROM users ORDER BY id", userColumns)
	rows, err := d.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询所有用户失败: %w", err)
	}
	defer rows.Close()
	var users []*User
	for {
		if !rows.Next() {
			break
		}
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// Count 统计用户总数。
func (d *UserDAO) Count(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM users`
	var count int64
	err := d.db.QueryRowContext(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计用户数失败: %w", err)
	}
	return count, nil
}

// ActiveCount 返回活跃用户数。
func (d *UserDAO) ActiveCount(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM users WHERE is_active = 1`
	var count int64
	err := d.db.QueryRowContext(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计活跃用户数失败: %w", err)
	}
	return count, nil
}
