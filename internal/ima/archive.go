package ima

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

// 压缩包自动解压上传支持：zip / 7z / rar。

const (
	MaxArchiveFiles = 5000        // 解压文件数上限
	MaxArchiveTotal = 2 << 30     // 解压总大小上限 2GB
	maxArchiveSelf  = 500 << 20   // 压缩包本身大小上限 500MB
)

// IsArchive 是否为支持自动解压的压缩包。
func IsArchive(name string) bool {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "zip", "7z", "rar":
		return true
	}
	return false
}

// ExtractedFile 解压出的一个文件。
type ExtractedFile struct {
	RelName string // 压缩包内相对路径（分隔符规整为 _，作为上传标题）
	Path    string // 落盘绝对路径
	Size    int64
}

// ExtractArchive 解压 archivePath 到 destDir，返回文件清单。
// 安全约束：zip-slip 路径穿越拦截、文件数与总大小上限、跳过目录与系统垃圾文件。
func ExtractArchive(archivePath, destDir string) ([]ExtractedFile, error) {
	st, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxArchiveSelf {
		return nil, fmt.Errorf("压缩包超过大小上限（%dMB）", maxArchiveSelf>>20)
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(archivePath), "."))
	// 临时文件常无扩展名：按 magic 嗅探兜底
	if ext != "zip" && ext != "7z" && ext != "rar" {
		if f, err := os.Open(archivePath); err == nil {
			head := make([]byte, 8)
			n, _ := f.Read(head)
			f.Close()
			switch {
			case n >= 4 && string(head[:4]) == "PK\x03\x04":
				ext = "zip"
			case n >= 6 && string(head[:6]) == "7z\xbc\xaf'\x1c":
				ext = "7z"
			case n >= 4 && string(head[:4]) == "Rar!":
				ext = "rar"
			}
		}
	}
	switch ext {
	case "zip":
		return extractZip(archivePath, destDir)
	case "7z":
		return extract7z(archivePath, destDir)
	case "rar":
		return extractRar(archivePath, destDir)
	}
	return nil, errors.New("不支持的压缩包格式：" + ext)
}

// safeRel 清洗压缩包内路径，防 zip-slip；返回清洗后的相对路径。
// 传空或清洗后为空表示跳过。
func safeRel(name string) (string, bool) {
	n := strings.ReplaceAll(name, "\\", "/")
	n = strings.TrimPrefix(n, "./")
	// 逐段安检：含 .. 的路径直接拒收；跳过系统垃圾
	for _, seg := range strings.Split(n, "/") {
		if seg == ".." {
			return "", false
		}
		if seg == "__MACOSX" || strings.HasPrefix(seg, ".DS_Store") || seg == "Thumbs.db" {
			return "", false
		}
	}
	clean := path.Clean("/" + n) // 强制绝对语义清洗，"../" 会被折掉
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." || clean == ".." {
		return "", false
	}
	return clean, true
}

// relTitle 作为上传标题：保留目录层级信息，分隔符换 "_"。
func relTitle(rel string) string {
	t := strings.ReplaceAll(rel, "/", "_")
	t = strings.ReplaceAll(t, "\\", "_")
	return t
}

type extractCounter struct {
	files int
	bytes int64
	out   []ExtractedFile
}

func (c *extractCounter) add(rel, abs string, size int64) error {
	c.files++
	c.bytes += size
	if c.files > MaxArchiveFiles {
		return fmt.Errorf("解压文件数超过上限（%d）", MaxArchiveFiles)
	}
	if c.bytes > MaxArchiveTotal {
		return fmt.Errorf("解压总大小超过上限（%dGB）", MaxArchiveTotal>>30)
	}
	c.out = append(c.out, ExtractedFile{RelName: relTitle(rel), Path: abs, Size: size})
	return nil
}

func writeEntry(destDir, rel string, r io.Reader) (int64, error) {
	abs := filepath.Join(destDir, filepath.FromSlash(rel))
	if !strings.HasPrefix(abs, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return 0, errors.New("非法路径：" + rel)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(abs)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	// 单文件也受解压总量约束（限制读取防止炸弹）
	remain := int64(MaxArchiveTotal)
	n, err := io.Copy(f, io.LimitReader(r, remain))
	if err != nil {
		return n, err
	}
	return n, nil
}

func extractZip(archivePath, destDir string) ([]ExtractedFile, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("zip 打开失败: %w", err)
	}
	defer zr.Close()
	var c extractCounter
	for _, f := range zr.File {
		rel, ok := safeRel(f.Name)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", f.Name, err)
		}
		n, err := writeEntry(destDir, rel, rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if err := c.add(rel, filepath.Join(destDir, filepath.FromSlash(rel)), n); err != nil {
			return nil, err
		}
	}
	return c.out, nil
}

func extract7z(archivePath, destDir string) ([]ExtractedFile, error) {
	zr, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("7z 打开失败: %w", err)
	}
	defer zr.Close()
	var c extractCounter
	for _, f := range zr.File {
		rel, ok := safeRel(f.Name)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", f.Name, err)
		}
		n, err := writeEntry(destDir, rel, rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if err := c.add(rel, filepath.Join(destDir, filepath.FromSlash(rel)), n); err != nil {
			return nil, err
		}
	}
	return c.out, nil
}

func extractRar(archivePath, destDir string) ([]ExtractedFile, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rr, err := rardecode.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("rar 打开失败: %w", err)
	}
	var c extractCounter
	for {
		hdr, err := rr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("rar 读取失败: %w", err)
		}
		rel, ok := safeRel(hdr.Name)
		if !ok || hdr.IsDir {
			continue
		}
		n, err := writeEntry(destDir, rel, rr)
		if err != nil {
			return nil, err
		}
		if err := c.add(rel, filepath.Join(destDir, filepath.FromSlash(rel)), n); err != nil {
			return nil, err
		}
	}
	return c.out, nil
}
