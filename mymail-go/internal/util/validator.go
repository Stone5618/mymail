// Package util 提供跨层通用工具函数。
//
// validator.go 实现邮箱、用户名、文件夹等输入校验。
// 兼容性：邮箱正则与原 Node.js mail.js:111 一致（宽松：^[^\s@]+@[^\s@]+$）。
package util

import (
	"fmt"
	"regexp"
	"strings"
)

// usernameRegex 用户名格式校验（与原 Node.js + auth_service.go 一致）。
// 规则：字母、数字、点、下划线，3-20 字符。
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9._]{3,20}$`)

// emailRegex 邮箱格式校验（与原 Node.js mail.js:111 一致，宽松匹配）。
// 注意：不要求顶级域名有点，比 RFC 5322 宽松，但够用。
var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+$`)

// 合法文件夹名（与原 Node.js 一致）。
var validFolders = map[string]bool{
	"INBOX":  true,
	"SENT":   true,
	"DRAFTS": true,
	"TRASH":  true,
	"JUNK":   true,
}

// ValidateUsername 校验用户名格式。
// 返回 nil 表示合法。
func ValidateUsername(username string) error {
	if username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if !usernameRegex.MatchString(username) {
		return fmt.Errorf("用户名仅允许字母、数字、点、下划线，3-20字符")
	}
	return nil
}

// ValidateEmail 校验单个邮箱格式。
// 返回 nil 表示合法。
func ValidateEmail(email string) error {
	if !emailRegex.MatchString(email) {
		return fmt.Errorf("无效的邮箱地址: %s", email)
	}
	return nil
}

// ValidateEmails 校验多个邮箱（逗号分隔或切片）。
// 任一不合法返回错误（错误消息含第一个无效地址，与原 Node.js 一致）。
func ValidateEmails(addrs []string) error {
	for _, a := range addrs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !emailRegex.MatchString(a) {
			return fmt.Errorf("无效的邮箱地址: %s", a)
		}
	}
	return nil
}

// SplitRecipients 拆分收件人字符串（逗号分隔），去空格、去空项。
// 与原 Node.js mail.js:105-108 的 to.split(',') 行为一致，但额外做 trim。
func SplitRecipients(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ValidateFolder 校验文件夹名是否合法。
func ValidateFolder(folder string) error {
	if !validFolders[folder] {
		return fmt.Errorf("无效的文件夹: %s", folder)
	}
	return nil
}

// IsValidFolder 判断文件夹名是否合法（布尔版本，便于 if 判断）。
func IsValidFolder(folder string) bool {
	return validFolders[folder]
}

// NormalizeEmail 标准化邮箱（小写 + 去空格）。
// 用于登录与投递时兼容大小写差异。
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ExtractDomain 从邮箱地址提取域名（@ 之后部分，小写）。
// 若无 @ 返回空字符串。
func ExtractDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// ExtractUsername 从邮箱地址提取用户名（@ 之前部分，原样保留大小写）。
// 若无 @ 返回原字符串。
func ExtractUsername(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email
	}
	return email[:at]
}
