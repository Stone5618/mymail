// handler.go 实现 SMTP Backend + Session 接口。
//
// 与原 Node.js smtp-receiver.js 行为对应：
//   - onMailFrom：记录 from，校验空地址（MAIL FROM:<> 退信场景允许）
//   - onRcptTo：校验收件人域名 == 配置 domain；校验用户存在且活跃
//   - onData：解析 RFC 5322 邮件 → 保存附件 → 写 maildir → 调 DeliverLocal
//
// 反垃圾（SPF/DNSBL/灰名单）在阶段 4 加入；本阶段默认 spamScore=0。
package smtp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"strings"
	"time"

	mailmsg "github.com/emersion/go-message/mail"
	smtpsrv "github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/maildir"
)

// backend 实现 smtpsrv.Backend。
type backend struct {
	cfg      *config.Config
	userDAO  *dao.UserDAO
	mailSvc  *service.MailService
	maildir  *maildir.Maildir
	attStore *attachment.Store
	// 阶段 4 反垃圾组件（nil 表示禁用）
	greylist    *GreylistChecker    // 灰名单检查器
	rateLimiter *RateLimiter        // 连接限流器
	validator   *MessageValidator   // 反垃圾验证器
}

// NewSession 实现 Backend.NewSession。每个 SMTP 连接创建一个新 session。
// 阶段 4：接入连接级限流（RateLimiter），超限返回 421 临时错误。
func (b *backend) NewSession(c *smtpsrv.Conn) (smtpsrv.Session, error) {
	var remoteIP string
	if conn := c.Conn(); conn != nil {
		if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			remoteIP = tcpAddr.IP.String()
		} else {
			remoteIP = conn.RemoteAddr().String()
		}
	}

	// 连接级限流（阶段 4）
	if b.rateLimiter != nil && !b.rateLimiter.Check(remoteIP) {
		slog.Info("SMTP 连接被限流拒绝", "ip", remoteIP)
		return nil, errors.New("421 4.7.0 too many connections from this IP")
	}

	return &session{
		backend:  b,
		remoteIP: remoteIP,
		hostname: c.Hostname(),
		rcptTo:   nil,
	}, nil
}

// session 实现 smtpsrv.Session。
//
// 状态机：
//   - Mail(from)：记录 from，重置 rcptTo
//   - Rcpt(to)：校验域名 + 收件人存在；累计到 rcptTo
//   - Data(r)：解析邮件 → 投递给所有 rcptTo
//   - Reset()：清空 from/rcptTo
//   - Logout()：清理资源
type session struct {
	backend  *backend
	remoteIP string
	hostname string
	from     string
	rcptTo   []string
}

// Mail 实现 Session.Mail。
// 兼容原 Node.js：仅记录，不做 SPF 校验（阶段 4 加入）。
func (s *session) Mail(from string, opts *smtpsrv.MailOptions) error {
	s.from = from
	s.rcptTo = nil
	slog.Debug("SMTP MAIL FROM", "from", from, "ip", s.remoteIP)
	return nil
}

// Rcpt 实现 Session.Rcpt。
// 校验：
//  1. 收件人域名 == 配置 domain（拒绝外部域）
//  2. 用户存在且活跃
//  3. 灰名单检查（阶段 4）：首次见到拒绝，延迟窗口后放行
func (s *session) Rcpt(to string, opts *smtpsrv.RcptOptions) error {
	addr, err := mail.ParseAddress(to)
	if err != nil {
		slog.Info("SMTP RCPT TO 地址解析失败", "to", to, "error", err)
		return errors.New("invalid recipient address")
	}
	parts := strings.SplitN(addr.Address, "@", 2)
	if len(parts) != 2 {
		return errors.New("invalid recipient address")
	}
	username, domain := parts[0], parts[1]
	if domain != s.backend.cfg.Domain {
		slog.Info("SMTP RCPT TO 域名不匹配，拒绝", "to", to, "domain", domain, "expected", s.backend.cfg.Domain)
		return errors.New("not our domain")
	}
	user, err := s.backend.userDAO.FindByUsername(context.Background(), username)
	if err != nil {
		slog.Error("SMTP RCPT TO 查询用户失败", "username", username, "error", err)
		return errors.New("internal error")
	}
	if user == nil || !user.IsActive {
		slog.Info("SMTP RCPT TO 用户不存在或未激活", "username", username)
		return errors.New("user not found")
	}

	// 灰名单检查（阶段 4）：MAIL FROM 为空（退信）时跳过
	if s.backend.greylist != nil && s.from != "" {
		dec := s.backend.greylist.Check(context.Background(), s.remoteIP, s.from, addr.Address)
		if !dec.Allow {
			slog.Info("SMTP RCPT TO 被灰名单拒绝",
				"ip", s.remoteIP, "from", s.from, "to", addr.Address,
				"reason", dec.Reason, "retry_after", dec.RetryAfter)
			return fmt.Errorf("450 4.7.1 greylisted, try again in %d seconds", dec.RetryAfter)
		}
	}

	s.rcptTo = append(s.rcptTo, addr.Address)
	slog.Debug("SMTP RCPT TO 接受", "to", to, "username", username)
	return nil
}

// Data 实现 Session.Data。
// 流程：
//  1. 读取完整邮件数据
//  2. 解析 RFC 5322 邮件（headers + body + attachments）
//  3. 反垃圾验证（阶段 4）：SPF/DNSBL/评分，拒绝/标记/放行
//  4. 对每个收件人调用 MailService.DeliverLocal
func (s *session) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("读取邮件数据失败: %w", err)
	}
	slog.Info("SMTP DATA 收到", "from", s.from, "recipients", len(s.rcptTo), "bytes", len(data))

	// 解析邮件
	parsed, err := parseMail(data)
	if err != nil {
		slog.Error("SMTP 邮件解析失败", "from", s.from, "error", err)
		// 不向客户端暴露内部错误细节
		return errors.New("message parse error")
	}

	// 反垃圾验证（阶段 4）
	spamScore := 0
	spamReasons := ""
	if s.backend.validator != nil && len(s.rcptTo) > 0 {
		senderDomain := extractDomain(s.from)
		result := s.backend.validator.Validate(context.Background(), ValidateInput{
			IP:           s.remoteIP,
			Sender:       s.from,
			SenderDomain: senderDomain,
			HELODomain:   s.hostname,
			Recipient:    s.rcptTo[0],
		})
		spamScore = result.SpamScore
		spamReasons = result.SpamReasons

		// 拒绝投递（spam 评分超过阈值）
		if result.ShouldReject {
			slog.Info("SMTP 邮件被反垃圾拒绝",
				"from", s.from, "ip", s.remoteIP,
				"score", spamScore, "reasons", spamReasons)
			return fmt.Errorf("550 5.7.1 message rejected as spam (score=%d)", spamScore)
		}
	}

	// 对每个收件人投递
	for _, recipient := range s.rcptTo {
		username := recipient
		if idx := strings.Index(recipient, "@"); idx > 0 {
			username = recipient[:idx]
		}
		if err := s.deliverToOne(username, recipient, data, parsed, spamScore, spamReasons); err != nil {
			slog.Error("本地投递失败", "recipient", recipient, "error", err)
			// 继续处理下一个收件人（与原 Node.js 行为一致：continue）
			continue
		}
	}
	return nil
}

// deliverToOne 将邮件投递给单个收件人。
//  1. 写一份原始邮件到 maildir new/（兼容性）
//  2. 解析出的附件保存到附件存储（路径属于收件人用户 ID）
//  3. 调用 MailService.DeliverLocal 完成入库
//  spamScore/spamReasons 来自反垃圾验证（阶段 4）
func (s *session) deliverToOne(username, recipient string, raw []byte, parsed *parsedMail, spamScore int, spamReasons string) error {
	// 1. 写 maildir（原 Node.js 行为）
	if s.backend.maildir != nil {
		if _, err := s.backend.maildir.SaveNew(username, raw); err != nil {
			slog.Warn("写 maildir 失败（继续 DB 投递）", "username", username, "error", err)
		}
	}

	// 2. 查找收件人用户 ID（用于附件存储路径）
	user, err := s.backend.userDAO.FindByUsername(context.Background(), username)
	if err != nil {
		return fmt.Errorf("查询收件人失败: %w", err)
	}
	if user == nil {
		return nil // 已在 Rcpt 校验过，这里再次保护
	}

	// 3. 保存附件到磁盘
	var attachments []service.LocalAttachment
	for _, att := range parsed.attachments {
		storagePath, finalMime, err := s.saveAttachment(user.ID, att)
		if err != nil {
			slog.Warn("保存附件失败", "filename", att.filename, "error", err)
			continue
		}
		attachments = append(attachments, service.LocalAttachment{
			Filename:    att.filename,
			MimeType:    finalMime,
			Content:     att.content,
			StoragePath: storagePath,
			SizeBytes:   int64(len(att.content)),
		})
	}

	// 4. 调用 DeliverLocal
	subject := parsed.subject
	if subject == "" {
		subject = "(无主题)"
	}
	messageID := parsed.messageID
	if messageID == "" {
		messageID = fmt.Sprintf("<%s@%s>", uuid.New().String(), s.backend.cfg.Domain)
	}

	in := service.DeliverLocalInput{
		RecipientUsername: username,
		FromAddr:          parsed.fromAddr,
		FromName:          parsed.fromName,
		ToAddr:            recipient,
		CcAddr:            parsed.ccAddr,
		ReplyTo:           parsed.replyTo,
		Subject:           subject,
		BodyHTML:          parsed.bodyHTML,
		BodyText:          parsed.bodyText,
		MessageID:         messageID,
		InReplyTo:         parsed.inReplyTo,
		HeadersRaw:        parsed.headersRaw,
		SizeBytes:         int64(len(raw)),
		Attachments:       attachments,
		// 阶段 4：接入反垃圾后传入实际评分
		SpamScore:   spamScore,
		SpamReasons: spamReasons,
	}
	return s.backend.mailSvc.DeliverLocal(context.Background(), in)
}

// saveAttachment 将入站邮件附件保存到磁盘。
// 调用 attachment.Store.SaveInboundFile：放宽 MIME 白名单（魔数优先识别），
// 保留大小限制、文件名净化、危险扩展名拦截。返回存储路径与最终 MIME。
func (s *session) saveAttachment(userID int64, att parsedAttachment) (storagePath, finalMime string, err error) {
	return s.backend.attStore.SaveInboundFile(attachment.SaveFileInput{
		UserID:    userID,
		Filename:  att.filename,
		MimeType:  att.mimeType,
		Size:      int64(len(att.content)),
		Reader:    bytes.NewReader(att.content),
		HeadBytes: headBytes(att.content),
	})
}

// headBytes 取前 16 字节用于魔数校验。
func headBytes(b []byte) []byte {
	if len(b) >= 16 {
		return b[:16]
	}
	return b
}

// Reset 实现 Session.Reset。
func (s *session) Reset() {
	s.from = ""
	s.rcptTo = nil
}

// Logout 实现 Session.Logout。
func (s *session) Logout() error {
	s.Reset()
	return nil
}

// wordDecoder 解码 MIME encoded-word（RFC 2047），支持常见中文/日文/韩文字符集。
var wordDecoder = &mime.WordDecoder{
	CharsetReader: charsetReader,
}

// charsetReader 返回指定字符集的解码 reader。
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	cs := strings.ToLower(charset)
	switch cs {
	case "gbk", "gb2312", "gb18030", "cp936":
		return simplifiedchinese.GB18030.NewDecoder().Reader(input), nil
	case "big5":
		return traditionalchinese.Big5.NewDecoder().Reader(input), nil
	case "shift_jis", "shift-jis", "sjis":
		return japanese.ShiftJIS.NewDecoder().Reader(input), nil
	case "euc-jp":
		return japanese.EUCJP.NewDecoder().Reader(input), nil
	case "euc-kr":
		return korean.EUCKR.NewDecoder().Reader(input), nil
	default:
		return input, nil
	}
}

// decodeMimeHeader 解码 MIME encoded-word 头部字段。
// 解码失败时返回原始值，避免丢失信息。
func decodeMimeHeader(s string) string {
	if s == "" {
		return ""
	}
	decoded, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}

// decodeAddressNames 解码地址列表中的显示名。
func decodeAddressNames(addrs []*mail.Address) {
	for _, a := range addrs {
		if a != nil {
			a.Name = decodeMimeHeader(a.Name)
		}
	}
}

// parseAddressHeader 从头部字段解析单个邮件地址。
// 兼容整个地址被编码成 encoded-word 的非标准头部（如 Python smtplib 生成）。
func parseAddressHeader(h mailmsg.Header, key string) (addr, name string) {
	raw := h.Get(key)
	if raw == "" {
		return "", ""
	}
	decoded := decodeMimeHeader(raw)
	if decoded == "" {
		return "", ""
	}
	// 单个头部可能包含多个地址（不规范），取第一个
	parts := strings.Split(decoded, ",")
	first := strings.TrimSpace(parts[0])
	if a, err := mail.ParseAddress(first); err == nil && a != nil {
		return a.Address, decodeMimeHeader(a.Name)
	}
	// 解析失败时兜底：若包含 <addr> 则提取，否则整体作为地址
	if idx := strings.Index(first, "<"); idx >= 0 {
		end := strings.Index(first[idx:], ">")
		if end > 0 {
			return first[idx+1 : idx+end], strings.TrimSpace(strings.Trim(first[:idx], `"`))
		}
	}
	return first, ""
}

// parseAddressListHeader 从头部字段解析多个邮件地址，返回逗号分隔的地址列表。
func parseAddressListHeader(h mailmsg.Header, key string) string {
	raw := h.Get(key)
	if raw == "" {
		return ""
	}
	decoded := decodeMimeHeader(raw)
	if decoded == "" {
		return ""
	}
	if list, err := mail.ParseAddressList(decoded); err == nil && len(list) > 0 {
		addrs := make([]string, len(list))
		for i, a := range list {
			addrs[i] = a.Address
		}
		return strings.Join(addrs, ", ")
	}
	// 解析失败时返回原始解码值
	return decoded
}

// ============ 邮件解析 ============

// parsedMail 解析后的邮件数据。
type parsedMail struct {
	messageID   string
	subject     string
	fromAddr    string
	fromName    string
	toAddr      string
	ccAddr      string
	replyTo     string
	inReplyTo   string
	bodyText    string
	bodyHTML    string
	headersRaw  string
	attachments []parsedAttachment
}

// parsedAttachment 解析后的附件。
type parsedAttachment struct {
	filename string
	mimeType string
	content  []byte
}

// parseMail 解析 RFC 5322 邮件。
//
// 使用 github.com/emersion/go-message/mail 解析邮件结构，
// 提取头部字段、正文（text/plain + text/html）、附件。
func parseMail(data []byte) (*parsedMail, error) {
	r, err := mailmsg.CreateReader(bytes.NewReader(data))
	if err != nil && !isUnknownCharset(err) {
		return nil, fmt.Errorf("创建邮件 reader 失败: %w", err)
	}
	defer r.Close()

	out := &parsedMail{}

	// 提取头部字段（显示名需解码 MIME encoded-word）
	out.messageID, _ = r.Header.MessageID()
	out.subject, _ = r.Header.Subject()
	out.subject = decodeMimeHeader(out.subject)

	// 某些客户端（如 Python smtplib）会把整个 From 地址编码成单个 encoded-word，
	// go-message 的 AddressList 无法直接解析。先对原始头部完整解码，再解析地址。
	out.fromAddr, out.fromName = parseAddressHeader(r.Header, "From")
	out.toAddr = parseAddressListHeader(r.Header, "To")
	out.ccAddr = parseAddressListHeader(r.Header, "Cc")
	out.replyTo, _ = parseAddressHeader(r.Header, "Reply-To")

	if inReplyToList, err := r.Header.MsgIDList("In-Reply-To"); err == nil && len(inReplyToList) > 0 {
		out.inReplyTo = inReplyToList[0]
	}

	// 序列化 headers 用于持久化
	out.headersRaw = serializeHeaders(r.Header)

	// 遍历 parts
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil && !isUnknownCharset(err) {
			return nil, fmt.Errorf("读取邮件 part 失败: %w", err)
		}
		content, _ := io.ReadAll(p.Body)

		switch h := p.Header.(type) {
		case *mailmsg.InlineHeader:
			ct, _, _ := h.ContentType()
			body := string(content)
			switch {
			case strings.HasPrefix(ct, "text/html"):
				if out.bodyHTML == "" {
					out.bodyHTML = body
				}
			case strings.HasPrefix(ct, "text/plain"):
				if out.bodyText == "" {
					out.bodyText = body
				}
			}
		case *mailmsg.AttachmentHeader:
			filename, _ := h.Filename()
			if filename == "" {
				filename = fmt.Sprintf("attachment-%d", time.Now().UnixNano())
			}
			ct, _, _ := h.ContentType()
			if ct == "" {
				ct = "application/octet-stream"
			}
			out.attachments = append(out.attachments, parsedAttachment{
				filename: filename,
				mimeType: ct,
				content:  content,
			})
		}
	}

	// 兜底：若 HTML 为空，用 text；反之亦然（与原 Node.js 一致）
	if out.bodyHTML == "" && out.bodyText != "" {
		out.bodyHTML = out.bodyText
	}
	if out.bodyText == "" && out.bodyHTML != "" {
		// 简单兜底，不剥离 HTML 标签（前端会处理）
		out.bodyText = out.bodyHTML
	}

	return out, nil
}

// serializeHeaders 将 Header 序列化为 JSON 字符串（与原 Node.js 兼容）。
func serializeHeaders(h mailmsg.Header) string {
	m := h.Map()
	if len(m) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for k, vs := range m {
		if !first {
			sb.WriteByte(',')
		}
		first = false
		sb.WriteString(fmt.Sprintf("%q:%q", k, strings.Join(vs, ", ")))
	}
	sb.WriteByte('}')
	return sb.String()
}

// isUnknownCharset 判断是否为未知字符集错误（可忽略）。
func isUnknownCharset(err error) bool {
	// go-message 通过 message.IsUnknownCharset 暴露此判定，
	// 但为避免引入新依赖，用字符串匹配
	return err != nil && strings.Contains(err.Error(), "unknown charset")
}

// extractDomain 从邮箱地址中提取域名。
// 如 "user@example.com" → "example.com"；无 @ 时返回空字符串。
// 用于反垃圾验证的 senderDomain 参数。
func extractDomain(addr string) string {
	idx := strings.LastIndex(addr, "@")
	if idx < 0 || idx == len(addr)-1 {
		return ""
	}
	return addr[idx+1:]
}
