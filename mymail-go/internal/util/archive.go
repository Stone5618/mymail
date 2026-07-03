// Package util
// archive.go 实现附件 zip 打包，用于批量下载。
//
// 用标准库 archive/zip，流式写入避免 OOM。
// 与原 Node.js archiver 行为兼容：Content-Type=application/zip，
// Content-Disposition=attachment; filename="attachments.zip"。
package util

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ZipEntry 描述一个待打包的附件条目。
type ZipEntry struct {
	// Filename 是 zip 内的文件名（显示给用户）。
	Filename string
	// SourcePath 是磁盘上的源文件绝对路径。
	SourcePath string
}

// ZipAttachmentsInput zip 打包输入。
type ZipAttachmentsInput struct {
	// Entries 待打包条目列表。
	Entries []ZipEntry
	// Writer 目标写入器（通常是 http.ResponseWriter）。
	Writer io.Writer
}

// ZipAttachments 将多个附件流式打包为 zip 写入 w。
// 返回写入的字节数与错误。
//
// 文件名重名处理：若多个条目 Filename 相同，自动追加序号后缀（如 report.pdf、report(1).pdf）。
func ZipAttachments(in ZipAttachmentsInput) (int64, error) {
	if len(in.Entries) == 0 {
		return 0, fmt.Errorf("没有附件")
	}

	zipWriter := zip.NewWriter(in.Writer)
	defer zipWriter.Close()

	// 文件名去重映射
	seenNames := make(map[string]int, len(in.Entries))
	var total int64

	for _, entry := range in.Entries {
		name := sanitizeZipName(entry.Filename)
		// 重名处理
		if cnt, exists := seenNames[name]; exists {
			seenNames[name] = cnt + 1
			ext := filepath.Ext(name)
			base := name[:len(name)-len(ext)]
			name = fmt.Sprintf("%s(%d)%s", base, cnt, ext)
		} else {
			seenNames[name] = 1
		}

		n, err := appendFileToZip(zipWriter, name, entry.SourcePath)
		if err != nil {
			return total, fmt.Errorf("打包文件 %s 失败: %w", entry.Filename, err)
		}
		total += n
	}

	if err := zipWriter.Close(); err != nil {
		return total, fmt.Errorf("关闭 zip 失败: %w", err)
	}
	return total, nil
}

// appendFileToZip 将单个文件追加到 zip writer。
// 返回写入的未压缩字节数。
func appendFileToZip(zw *zip.Writer, name, srcPath string) (int64, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return 0, fmt.Errorf("打开源文件失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("读取文件信息失败: %w", err)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return 0, fmt.Errorf("创建 zip header 失败: %w", err)
	}
	header.Name = name
	header.Method = zip.Deflate // 默认压缩

	writer, err := zw.CreateHeader(header)
	if err != nil {
		return 0, fmt.Errorf("创建 zip 条目失败: %w", err)
	}

	copied, err := io.Copy(writer, f)
	if err != nil {
		return 0, fmt.Errorf("写入 zip 内容失败: %w", err)
	}
	return copied, nil
}

// sanitizeZipName 清理 zip 内文件名，防止路径穿越。
// 仅保留 basename，去除目录部分。
func sanitizeZipName(name string) string {
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}
