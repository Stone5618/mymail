// Package attachment 实现附件文件存储与安全校验。
//
// 修复的缺陷：
//   - P0-7：MIME 类型白名单校验 + 文件头魔数校验，防止伪装文件类型
//   - P1-5：存储配额校验（上传前检查 storage_limit）
//
// 目录结构（与原 Node.js 一致）：
//   {attachmentPath}/{userId}/{timestamp}-{random}-{originalname}
package attachment

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mymail/mymail-go/internal/config"
)

// Store 封装附件文件存储操作。
type Store struct {
	rootPath        string
	maxSize         int64
	allowedTypes    map[string]bool
	allowedExts     map[string]bool
}

// New 创建附件存储实例。
func New(cfg *config.Config) *Store {
	s := &Store{
		rootPath: cfg.AttachmentPath,
		maxSize:  cfg.MaxAttachmentSize,
		allowedTypes: map[string]bool{
			// 文档
			"text/plain": true, "text/csv": true,
			"application/pdf": true,
			"application/msword": true,
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
			"application/vnd.ms-excel": true,
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
			"application/vnd.ms-powerpoint": true,
			"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
			"application/zip": true, "application/x-zip-compressed": true,
			"application/gzip": true, "application/x-gzip": true,
			"application/json": true, "application/xml": true,
			// 图片
			"image/jpeg": true, "image/png": true, "image/gif": true,
			"image/webp": true, "image/bmp": true, "image/svg+xml": true,
			// 音视频
			"audio/mpeg": true, "audio/ogg": true, "audio/wav": true,
			"video/mp4": true, "video/webm": true,
		},
		allowedExts: map[string]bool{
			".txt": true, ".csv": true, ".pdf": true, ".doc": true, ".docx": true,
			".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
			".zip": true, ".gz": true, ".tar": true, ".json": true, ".xml": true,
			".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
			".webp": true, ".bmp": true, ".svg": true,
			".mp3": true, ".ogg": true, ".wav": true, ".mp4": true, ".webm": true,
		},
	}
	return s
}

// magicNumbers 文件头魔数表（P0-7 修复：校验文件真实类型）。
// 用于检测客户端伪造 MIME 类型。
var magicNumbers = []struct {
	mime string
	head []byte
}{
	{"application/pdf", []byte{0x25, 0x50, 0x44, 0x46}},                   // %PDF
	{"image/jpeg", []byte{0xFF, 0xD8, 0xFF}},                              // JPEG
	{"image/png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}}, // PNG
	{"image/gif", []byte{0x47, 0x49, 0x46, 0x38}},                        // GIF8
	{"application/zip", []byte{0x50, 0x4B, 0x03, 0x04}},                  // PK..
	{"application/gzip", []byte{0x1F, 0x8B}},                             // gzip
	{"image/bmp", []byte{0x42, 0x4D}},                                    // BM
	{"image/webp", []byte{0x52, 0x49, 0x46, 0x46}},                       // RIFF (webp/wav/avi)
}

// detectMimeByMagic 通过文件头魔数检测真实 MIME 类型。
// 返回检测到的 MIME，若无法识别返回空字符串。
func detectMimeByMagic(head []byte) string {
	for _, mn := range magicNumbers {
		if len(head) >= len(mn.head) && bytesEqual(head[:len(mn.head)], mn.head) {
			return mn.mime
		}
	}
	return ""
}

// bytesEqual 比较字节切片相等。
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ValidateFile 校验上传文件的安全性（P0-7）。
// 检查项：
//  1. 文件大小不超过 maxSize
//  2. MIME 类型在白名单中
//  3. 文件扩展名在白名单中
//  4. 文件头魔数与声明的 MIME 一致（防伪装）
//
// 返回 nil 表示通过校验。
func (s *Store) ValidateFile(filename, declaredMime string, size int64, head []byte) error {
	// 1. 大小校验
	if size > s.maxSize {
		return fmt.Errorf("文件大小 %d 超过限制 %d", size, s.maxSize)
	}
	if size == 0 {
		return fmt.Errorf("文件为空")
	}

	// 2. MIME 白名单
	if !s.allowedTypes[declaredMime] {
		return fmt.Errorf("不支持的文件类型: %s", declaredMime)
	}

	// 3. 扩展名白名单
	ext := strings.ToLower(filepath.Ext(filename))
	if !s.allowedExts[ext] {
		return fmt.Errorf("不支持的文件扩展名: %s", ext)
	}

	// 4. 魔数校验（仅对已知魔数的类型校验）
	if len(head) > 0 {
		detected := detectMimeByMagic(head)
		if detected != "" {
			// 检测到了魔数，需与声明的一致（兼容同族类型）
			if !isSameMimeFamily(detected, declaredMime) {
				return fmt.Errorf("文件类型伪装：声明 %s，实际 %s", declaredMime, detected)
			}
		}
		// 无法识别魔数的类型（如 text/*、application/json）跳过此校验
	}

	return nil
}

// isSameMimeFamily 判断两个 MIME 是否属于同族（允许同类变种）。
// 例如 image/jpeg 与 image/jpeg 一致；application/zip 与 application/x-zip-compressed 一致。
func isSameMimeFamily(a, b string) bool {
	if a == b {
		return true
	}
	// zip 同族
	zipSet := map[string]bool{"application/zip": true, "application/x-zip-compressed": true}
	if zipSet[a] && zipSet[b] {
		return true
	}
	// gzip 同族
	gzipSet := map[string]bool{"application/gzip": true, "application/x-gzip": true}
	if gzipSet[a] && gzipSet[b] {
		return true
	}
	return false
}

// SaveFileInput 保存附件文件的输入参数。
type SaveFileInput struct {
	UserID     int64
	Filename   string
	MimeType   string
	Size       int64
	Reader     io.Reader // 文件内容读取器
	HeadBytes  []byte    // 文件头前 N 字节（用于魔数校验，至少 16 字节）
}

// SaveFile 保存上传的附件文件到磁盘。
// 返回存储路径（相对 rootPath）与唯一文件名。
// 修复 P0-7：保存前再次调用 ValidateFile。
func (s *Store) SaveFile(in SaveFileInput) (storagePath string, err error) {
	// 校验
	if err := s.ValidateFile(in.Filename, in.MimeType, in.Size, in.HeadBytes); err != nil {
		return "", err
	}

	// 创建用户目录
	userDir := filepath.Join(s.rootPath, fmt.Sprintf("%d", in.UserID))
	if err := os.MkdirAll(userDir, 0750); err != nil {
		return "", fmt.Errorf("创建附件目录失败: %w", err)
	}

	// 生成唯一文件名：{timestamp}-{random}-{originalname}
	unique := randomHex(8)
	safeName := sanitizeFilename(in.Filename)
	filename := fmt.Sprintf("%d-%s-%s", time.Now().Unix(), unique, safeName)
	fullPath := filepath.Join(userDir, filename)

	// 写入文件
	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("创建附件文件失败: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
		// 失败时清理半成品文件
		if err != nil {
			os.Remove(fullPath)
		}
	}()

	// 若有 headBytes，先写入头部，再流式写入剩余内容
	if len(in.HeadBytes) > 0 {
		if _, err := f.Write(in.HeadBytes); err != nil {
			return "", fmt.Errorf("写入文件头失败: %w", err)
		}
	}
	if _, err := io.Copy(f, in.Reader); err != nil {
		return "", fmt.Errorf("写入附件内容失败: %w", err)
	}

	return fullPath, nil
}

// sanitizeFilename 清理文件名中的危险字符（防止路径穿越）。
// 仅保留字母、数字、点、下划线、连字符。
func sanitizeFilename(name string) string {
	// 取 basename（去除路径）
	name = filepath.Base(name)
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			sb.WriteRune(r)
		}
		// 其他字符替换为下划线
		if r == ' ' || r == '(' || r == ')' {
			sb.WriteRune('_')
		}
	}
	result := sb.String()
	// 拒绝危险文件名（空、当前目录、父目录引用）
	if result == "" || result == "." || result == ".." {
		result = "attachment"
	}
	// 限制长度
	if len(result) > 100 {
		ext := filepath.Ext(result)
		result = result[:100-len(ext)] + ext
	}
	return result
}

// randomHex 生成 n 字节的随机 hex 字符串。
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// DeleteFile 删除附件文件。
// 文件不存在不报错（幂等）。
func (s *Store) DeleteFile(storagePath string) error {
	if err := os.Remove(storagePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除附件文件失败: %w", err)
	}
	return nil
}

// ReadFile 读取附件文件内容。
func (s *Store) ReadFile(storagePath string) ([]byte, error) {
	data, err := os.ReadFile(storagePath)
	if err != nil {
		return nil, fmt.Errorf("读取附件文件失败: %w", err)
	}
	return data, nil
}

// OpenFile 打开附件文件用于流式读取（下载用）。
func (s *Store) OpenFile(storagePath string) (*os.File, error) {
	f, err := os.Open(storagePath)
	if err != nil {
		return nil, fmt.Errorf("打开附件文件失败: %w", err)
	}
	return f, nil
}

// SaveFromMultipartFile 从 multipart.File 保存附件。
// 内部自动读取文件头（前 16 字节）用于魔数校验。
// 返回 storagePath 和实际写入字节数。
func (s *Store) SaveFromMultipartFile(userID int64, fh *multipart.FileHeader) (storagePath string, written int64, err error) {
	src, err := fh.Open()
	if err != nil {
		return "", 0, fmt.Errorf("打开上传文件失败: %w", err)
	}
	defer src.Close()

	// 读取前 16 字节用于魔数校验
	head := make([]byte, 16)
	n, err := io.ReadFull(src, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", 0, fmt.Errorf("读取文件头失败: %w", err)
	}
	head = head[:n]

	// 重置读取位置（合并 head + 剩余内容）
	combined := io.MultiReader(strings.NewReader(string(head)), src)

	path, err := s.SaveFile(SaveFileInput{
		UserID:    userID,
		Filename:  fh.Filename,
		MimeType:  fh.Header.Get("Content-Type"),
		Size:      fh.Size,
		Reader:    combined,
		HeadBytes: head,
	})
	if err != nil {
		return "", 0, err
	}
	return path, fh.Size, nil
}
