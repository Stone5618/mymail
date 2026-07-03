// Package sanitize 实现 HTML 净化，防御 XSS。
//
// P0-4 修复（后端部分）：原 Node.js 后端对邮件 HTML 零处理，body_html 原样入库原样返回，
// 前端 MailDetailView.vue 直接 v-html，存在存储型 XSS 风险。
//
// 本包用 bluemonday UGCPolicy（用户生成内容策略）净化：
//   - 保留常见格式标签（p/div/span/a/img/ul/ol/li/table 等）
//   - 移除 script/style/iframe/object/embed/form 等危险标签
//   - 移除所有 on* 事件属性
//   - a 标签 href 仅允许 http/https/mailto 协议（防 javascript:）
//   - img 标签 src 仅允许 http/https/data 协议
//
// 净化策略：邮件入库时调用 SanitizeHTML，净化后存 body_html，原始存 body_html_raw。
package sanitize

import (
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// 预编译的策略（线程安全，可并发使用）。
// bluemonday 的 policy 本身是并发安全的（净化时只读）。
var (
	ugcPolicy     *bluemonday.Policy
	ugcPolicyOnce sync.Once
)

// policy 返回单例 UGC 策略。
func policy() *bluemonday.Policy {
	ugcPolicyOnce.Do(func() {
		p := bluemonday.UGCPolicy()
		// 额外允许 target 属性（链接新窗口打开）
		p.AllowAttrs("target").OnElements("a")
		ugcPolicy = p
	})
	return ugcPolicy
}

// SanitizeHTML 净化 HTML 内容，返回安全的 HTML 字符串。
// 输入为空时返回空字符串。
func SanitizeHTML(html string) string {
	if html == "" {
		return ""
	}
	return policy().Sanitize(html)
}

// SanitizeHTMLBytes 字节切片版本。
func SanitizeHTMLBytes(html []byte) []byte {
	if len(html) == 0 {
		return nil
	}
	return []byte(policy().Sanitize(string(html)))
}

// IsHTMLOnlyWhitespace 判断 HTML 是否仅含空白（含被净化后变空的场景）。
// 用于决定是否回退显示 body_text。
func IsHTMLOnlyWhitespace(html string) bool {
	for _, r := range html {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
