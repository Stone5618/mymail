// Package attachment
// attachment_test.go 测试附件文件存储与安全校验。
//
// 测试覆盖：
//   - ValidateFile 各种场景（大小/MIME/扩展名/魔数伪装）
//   - SaveFile 保存文件并校验路径
//   - DeleteFile/ReadFile/OpenFile
//   - sanitizeFilename 防路径穿越
//   - detectMimeByMagic 魔数检测
//   - isSameMimeFamily 同族判断
package attachment

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymail/mymail-go/internal/config"
)

// newTestStore 创建基于 t.TempDir() 的测试 Store。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	cfg := &config.Config{
		AttachmentPath:    filepath.Join(t.TempDir(), "attachments"),
		MaxAttachmentSize: 1024 * 1024, // 1MB
	}
	return New(cfg)
}

// ============ ValidateFile 测试 ============

func TestValidateFile_ValidPDF(t *testing.T) {
	s := newTestStore(t)
	// PDF 魔数 %PDF
	head := []byte{0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E, 0x35}
	err := s.ValidateFile("doc.pdf", "application/pdf", 100, head)
	if err != nil {
		t.Errorf("合法 PDF 应通过校验: %v", err)
	}
}

func TestValidateFile_ValidPNG(t *testing.T) {
	s := newTestStore(t)
	head := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	err := s.ValidateFile("img.png", "image/png", 100, head)
	if err != nil {
		t.Errorf("合法 PNG 应通过校验: %v", err)
	}
}

func TestValidateFile_ValidJPEG(t *testing.T) {
	s := newTestStore(t)
	head := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}
	err := s.ValidateFile("img.jpg", "image/jpeg", 100, head)
	if err != nil {
		t.Errorf("合法 JPEG 应通过校验: %v", err)
	}
}

func TestValidateFile_ValidTextNoMagic(t *testing.T) {
	s := newTestStore(t)
	// text/plain 没有魔数，应跳过魔数校验
	head := []byte("Hello, world!")
	err := s.ValidateFile("note.txt", "text/plain", 13, head)
	if err != nil {
		t.Errorf("合法 txt 应通过校验: %v", err)
	}
}

func TestValidateFile_TooLarge(t *testing.T) {
	s := newTestStore(t)
	err := s.ValidateFile("big.pdf", "application/pdf", 2*1024*1024, nil)
	if err == nil {
		t.Error("超过大小限制应报错")
	}
	if !strings.Contains(err.Error(), "超过限制") {
		t.Errorf("错误消息应包含'超过限制'，实际：%v", err)
	}
}

func TestValidateFile_EmptyFile(t *testing.T) {
	s := newTestStore(t)
	err := s.ValidateFile("empty.pdf", "application/pdf", 0, nil)
	if err == nil {
		t.Error("空文件应报错")
	}
	if !strings.Contains(err.Error(), "为空") {
		t.Errorf("错误消息应包含'为空'，实际：%v", err)
	}
}

func TestValidateFile_UnsupportedMime(t *testing.T) {
	s := newTestStore(t)
	err := s.ValidateFile("malware.exe", "application/x-msdownload", 100, nil)
	if err == nil {
		t.Error("不在白名单的 MIME 应报错")
	}
	if !strings.Contains(err.Error(), "不支持的文件类型") {
		t.Errorf("错误消息应包含'不支持的文件类型'，实际：%v", err)
	}
}

func TestValidateFile_UnsupportedExt(t *testing.T) {
	s := newTestStore(t)
	// MIME 在白名单但扩展名不在
	err := s.ValidateFile("malware.exe", "application/pdf", 100, nil)
	if err == nil {
		t.Error("不在白名单的扩展名应报错")
	}
	if !strings.Contains(err.Error(), "不支持的文件扩展名") {
		t.Errorf("错误消息应包含'不支持的文件扩展名'，实际：%v", err)
	}
}

// P0-7 关键测试：MIME 伪装检测
func TestValidateFile_MagicMismatch_P0_7(t *testing.T) {
	s := newTestStore(t)
	// 声明是 PDF，但文件头是 PNG
	pngHead := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	err := s.ValidateFile("fake.pdf", "application/pdf", 100, pngHead)
	if err == nil {
		t.Error("P0-7：MIME 伪装应被检测到")
	}
	if !strings.Contains(err.Error(), "伪装") {
		t.Errorf("错误消息应包含'伪装'，实际：%v", err)
	}
}

// 反向伪装：声明 PNG 实际是 PDF
func TestValidateFile_MagicMismatch_Reverse(t *testing.T) {
	s := newTestStore(t)
	pdfHead := []byte{0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E, 0x35}
	err := s.ValidateFile("fake.png", "image/png", 100, pdfHead)
	if err == nil {
		t.Error("反向 MIME 伪装应被检测到")
	}
}

// 同族类型应放行
func TestValidateFile_SameFamilyZip(t *testing.T) {
	s := newTestStore(t)
	zipHead := []byte{0x50, 0x4B, 0x03, 0x04}
	// 声明 x-zip-compressed，实际检测为 zip（同族）
	err := s.ValidateFile("data.zip", "application/x-zip-compressed", 100, zipHead)
	if err != nil {
		t.Errorf("zip 同族类型应通过校验: %v", err)
	}
}

func TestValidateFile_SameFamilyGzip(t *testing.T) {
	s := newTestStore(t)
	gzipHead := []byte{0x1F, 0x8B, 0x08}
	// 声明 x-gzip，实际检测为 gzip（同族）
	err := s.ValidateFile("data.gz", "application/x-gzip", 100, gzipHead)
	if err != nil {
		t.Errorf("gzip 同族类型应通过校验: %v", err)
	}
}

// 大小写不敏感扩展名
func TestValidateFile_ExtCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	err := s.ValidateFile("IMG.PNG", "image/png", 100, nil)
	if err != nil {
		t.Errorf("大写扩展名应通过校验: %v", err)
	}
}

// ============ SaveFile 测试 ============

func TestSaveFile_Success(t *testing.T) {
	s := newTestStore(t)
	content := []byte("%PDF-1.5 fake pdf content for test")
	in := SaveFileInput{
		UserID:    100,
		Filename:  "doc.pdf",
		MimeType:  "application/pdf",
		Size:      int64(len(content)),
		Reader:    bytes.NewReader(content),
		HeadBytes: content[:8],
	}

	path, err := s.SaveFile(in)
	if err != nil {
		t.Fatalf("SaveFile 失败: %v", err)
	}
	if path == "" {
		t.Fatal("path 不能为空")
	}

	// 校验文件存在
	if _, err := os.Stat(path); err != nil {
		t.Errorf("保存的文件不存在: %v", err)
	}

	// 校验路径包含用户 ID
	if !strings.Contains(path, filepath.Join(s.rootPath, "100")) {
		t.Errorf("路径应包含用户 ID 100，实际：%s", path)
	}
}

func TestSaveFile_ValidateFailed(t *testing.T) {
	s := newTestStore(t)
	// 用伪装的文件头，SaveFile 应在校验阶段就拒绝
	pngHead := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	content := append(pngHead, []byte("fake pdf content")...)
	in := SaveFileInput{
		UserID:    100,
		Filename:  "fake.pdf",
		MimeType:  "application/pdf",
		Size:      int64(len(content)),
		Reader:    bytes.NewReader(content[8:]),
		HeadBytes: pngHead,
	}

	_, err := s.SaveFile(in)
	if err == nil {
		t.Error("P0-7：伪装文件应被拒绝保存")
	}
}

func TestSaveFile_CleanupOnFailure(t *testing.T) {
	// 校验失败时不应留下半成品文件
	s := newTestStore(t)
	in := SaveFileInput{
		UserID:    100,
		Filename:  "bad.exe",
		MimeType:  "application/x-msdownload", // 不在白名单
		Size:      100,
		Reader:    bytes.NewReader([]byte("content")),
		HeadBytes: nil,
	}

	_, err := s.SaveFile(in)
	if err == nil {
		t.Fatal("应报错")
	}

	// 用户目录下不应有文件
	userDir := filepath.Join(s.rootPath, "100")
	entries, _ := os.ReadDir(userDir)
	if len(entries) > 0 {
		t.Errorf("校验失败后不应留下文件，但发现 %d 个", len(entries))
	}
}

// ============ DeleteFile/ReadFile/OpenFile 测试 ============

func TestDeleteFile_Success(t *testing.T) {
	s := newTestStore(t)
	// 先保存一个文件
	content := []byte("%PDF-1.5 hello")
	path, _ := s.SaveFile(SaveFileInput{
		UserID:    1,
		Filename:  "a.pdf",
		MimeType:  "application/pdf",
		Size:      int64(len(content)),
		Reader:    bytes.NewReader(content[8:]),
		HeadBytes: content[:8],
	})

	if err := s.DeleteFile(path); err != nil {
		t.Fatalf("DeleteFile 失败: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("删除后文件应不存在")
	}
}

func TestDeleteFile_NotExist(t *testing.T) {
	s := newTestStore(t)
	// 删除不存在的文件不应报错（幂等）
	if err := s.DeleteFile(filepath.Join(s.rootPath, "nonexistent")); err != nil {
		t.Errorf("删除不存在的文件不应报错: %v", err)
	}
}

func TestReadFile_Success(t *testing.T) {
	s := newTestStore(t)
	content := []byte("%PDF-1.5 readable content")
	path, _ := s.SaveFile(SaveFileInput{
		UserID:    1,
		Filename:  "r.pdf",
		MimeType:  "application/pdf",
		Size:      int64(len(content)),
		Reader:    bytes.NewReader(content[8:]),
		HeadBytes: content[:8],
	})

	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	// 头部 + 剩余内容
	expected := append(content[:8], content[8:]...)
	if !bytes.Equal(got, expected) {
		t.Errorf("读取内容不一致")
	}
}

func TestOpenFile_Success(t *testing.T) {
	s := newTestStore(t)
	content := []byte("%PDF-1.5 openable")
	path, _ := s.SaveFile(SaveFileInput{
		UserID:    1,
		Filename:  "o.pdf",
		MimeType:  "application/pdf",
		Size:      int64(len(content)),
		Reader:    bytes.NewReader(content[8:]),
		HeadBytes: content[:8],
	})

	f, err := s.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile 失败: %v", err)
	}
	defer f.Close()
}

// ============ SaveFromMultipartFile 测试 ============

func TestSaveFromMultipartFile_Success(t *testing.T) {
	s := newTestStore(t)
	// 构造一个真实的 multipart 表单（multipart.FileHeader 无法直接手工构造，
	// 因为其内部 file 字段需要 fs.file / os.File 等具体类型）
	content := []byte("%PDF-1.5 multipart upload content here")

	// 构造一个 multipart body（用 CreatePart 以便设置 Content-Type 为 application/pdf）
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="upload.pdf"`)
	h.Set("Content-Type", "application/pdf")
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatalf("CreatePart 失败: %v", err)
	}
	part.Write(content)
	w.Close()

	// 解析 multipart
	req, _ := http.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	_ = req.ParseMultipartForm(2 * 1024 * 1024)
	files := req.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("期望 1 个文件，实际 %d", len(files))
	}

	// 调用 SaveFromMultipartFile
	path, written, err := s.SaveFromMultipartFile(100, files[0])
	if err != nil {
		t.Fatalf("SaveFromMultipartFile 失败: %v", err)
	}
	if path == "" {
		t.Error("path 不能为空")
	}
	if written != int64(len(content)) {
		t.Errorf("written 期望 %d，实际 %d", len(content), written)
	}

	// 校验文件存在
	if _, err := os.Stat(path); err != nil {
		t.Errorf("保存的文件不存在: %v", err)
	}
}

func TestSaveFromMultipartFile_MagicMismatch(t *testing.T) {
	s := newTestStore(t)
	// 声明 PDF，实际是 PNG 内容
	pngHead := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	content := append(pngHead, []byte("fake pdf body")...)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	// 手动设置 Content-Type 为 application/pdf
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="fake.pdf"`)
	h.Set("Content-Type", "application/pdf")
	part, _ := w.CreatePart(h)
	part.Write(content)
	w.Close()

	req, _ := http.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	_ = req.ParseMultipartForm(2 * 1024 * 1024)
	files := req.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("期望 1 个文件，实际 %d", len(files))
	}

	_, _, err := s.SaveFromMultipartFile(100, files[0])
	if err == nil {
		t.Error("P0-7：伪装文件应被 SaveFromMultipartFile 拒绝")
	}
}

// ============ 内部辅助函数测试 ============

func TestDetectMimeByMagic(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"PDF", []byte{0x25, 0x50, 0x44, 0x46}, "application/pdf"},
		{"JPEG", []byte{0xFF, 0xD8, 0xFF}, "image/jpeg"},
		{"PNG", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"GIF", []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}, "image/gif"},
		{"ZIP", []byte{0x50, 0x4B, 0x03, 0x04}, "application/zip"},
		{"GZIP", []byte{0x1F, 0x8B, 0x08}, "application/gzip"},
		{"BMP", []byte{0x42, 0x4D, 0x00, 0x00}, "image/bmp"},
		{"WEBP/RIFF", []byte{0x52, 0x49, 0x46, 0x46}, "image/webp"},
		{"Unknown", []byte{0x00, 0x01, 0x02, 0x03}, ""},
		{"Empty", []byte{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectMimeByMagic(c.head)
			if got != c.want {
				t.Errorf("detectMimeByMagic(%v) = %q，期望 %q", c.head, got, c.want)
			}
		})
	}
}

func TestIsSameMimeFamily(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"application/pdf", "application/pdf", true},
		{"application/zip", "application/x-zip-compressed", true},
		{"application/x-zip-compressed", "application/zip", true},
		{"application/gzip", "application/x-gzip", true},
		{"application/x-gzip", "application/gzip", true},
		{"application/pdf", "image/png", false},
		{"application/zip", "application/gzip", false},
		{"image/jpeg", "image/png", false},
	}
	for _, c := range cases {
		got := isSameMimeFamily(c.a, c.b)
		if got != c.want {
			t.Errorf("isSameMimeFamily(%q, %q) = %v，期望 %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // 关键性质：不含 / \ .. 等危险字符
	}{
		{"普通文件名", "document.pdf", "document.pdf"},
		{"含路径", "../../../etc/passwd", "passwd"}, // filepath.Base 已剥离路径
		{"含空格", "my file.pdf", "my_file.pdf"},
		{"含括号", "report(1).pdf", "report_1_.pdf"},
		{"Windows 路径", "..\\..\\secret.txt", "secret.txt"}, // \ 被丢弃
		{"空字符串", "", "attachment"},
		{"仅点", ".", "attachment"},
		{"仅双点", "..", "attachment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeFilename(c.in)
			// 关键校验：不含路径分隔符
			if strings.Contains(got, "/") || strings.Contains(got, "\\") {
				t.Errorf("净化后文件名不应包含路径分隔符：%q", got)
			}
			if strings.Contains(got, "..") {
				t.Errorf("净化后文件名不应包含 ..：%q", got)
			}
			if c.want != "" && got != c.want {
				t.Errorf("sanitizeFilename(%q) = %q，期望 %q", c.in, got, c.want)
			}
		})
	}
}

func TestSanitizeFilename_LengthLimit(t *testing.T) {
	// 超长文件名应被截断
	longName := strings.Repeat("a", 200) + ".pdf"
	got := sanitizeFilename(longName)
	if len(got) > 100 {
		t.Errorf("净化后文件名长度应 <= 100，实际 %d", len(got))
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Errorf("截断后应保留扩展名 .pdf，实际 %q", got)
	}
}
