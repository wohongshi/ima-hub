package ima

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func makeZip(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	p := filepath.Join(dir, "test.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	f.Close()
	return p
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	p := makeZip(t, dir, map[string]string{
		"数学/第一章.pdf":  "pdfdata",
		"数学/第二章.pdf":  "pdfdata2",
		"英语/词汇.txt":   "word",
		"根目录笔记.md":    "# hi",
		".DS_Store":   "junk",
		"__MACOSX/x":  "junk",
		"../evil.txt": "evil",
	})
	out, err := ExtractArchive(p, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("expect 4 files (skip junk + zip-slip), got %d: %+v", len(out), out)
	}
	names := map[string]bool{}
	for _, f := range out {
		names[f.RelName] = true
		if _, err := os.Stat(f.Path); err != nil {
			t.Fatalf("missing extracted file: %s", f.Path)
		}
	}
	for _, want := range []string{"数学_第一章.pdf", "数学_第二章.pdf", "英语_词汇.txt", "根目录笔记.md"} {
		if !names[want] {
			t.Fatalf("missing %s in %+v", want, names)
		}
	}
}

func TestExtractZipSlip(t *testing.T) {
	dir := t.TempDir()
	// 手工构造含 ../ 的 zip 条目（zip.Writer.Create 允许任意名字）
	p := filepath.Join(dir, "evil.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../../tmp/pwned.txt")
	w.Write([]byte("x"))
	zw.Close()
	f.Close()
	out, err := ExtractArchive(p, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range out {
		if bytes.Contains([]byte(e.Path), []byte("..")) {
			t.Fatalf("zip-slip escaped: %s", e.Path)
		}
		if !strings.HasPrefix(e.Path, filepath.Join(dir, "out")) {
			t.Fatalf("path outside dest: %s", e.Path)
		}
	}
}

func TestIsArchive(t *testing.T) {
	for _, n := range []string{"a.zip", "b.7z", "c.RAR", "d.Zip"} {
		if !IsArchive(n) {
			t.Fatalf("%s should be archive", n)
		}
	}
	for _, n := range []string{"a.pdf", "b.txt", "c.tar.gz"} {
		if IsArchive(n) {
			t.Fatalf("%s should NOT be archive", n)
		}
	}
}

func TestExtract7z(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z CLI 不可用")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "a.pdf"), []byte("AAA"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("BBB"), 0o644)
	p := filepath.Join(dir, "t.7z")
	cmd := exec.Command("7z", "a", "-t7z", p, ".")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z create: %v %s", err, out)
	}
	out, err := ExtractArchive(p, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expect 2, got %+v", out)
	}
}

func TestExtractRar(t *testing.T) {
	for _, fixture := range []string{"testdata/testfile.rar3.rar", "testdata/testfile.rar5.rar"} {
		if _, err := os.Stat(fixture); err != nil {
			t.Skip("缺少夹具 " + fixture)
		}
		dir := t.TempDir()
		out, err := ExtractArchive(fixture, filepath.Join(dir, "out"))
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		if len(out) == 0 {
			t.Fatalf("%s: expect >=1 files, got none", fixture)
		}
		for _, e := range out {
			if _, err := os.Stat(e.Path); err != nil {
				t.Fatalf("%s: missing extracted %s", fixture, e.Path)
			}
		}
		t.Logf("%s -> %d files: %+v", fixture, len(out), out)
	}
}
