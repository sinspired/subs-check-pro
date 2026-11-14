// sub-store\update_substore_assets_test.go
package substore

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, body := range files {
		fw, _ := w.Create(name)
		_, _ = fw.Write([]byte(body))
	}
	_ = w.Close()
	_ = f.Close()
}

func oldFrontend(t *testing.T) string {
	t.Helper()
	front := filepath.Join(t.TempDir(), "frontend")
	_ = os.MkdirAll(filepath.Join(front, "scp"), 0o755)
	_ = os.WriteFile(filepath.Join(front, "index.html"), []byte("OLD"), 0o644)
	_ = os.WriteFile(filepath.Join(front, "scp", "logo.svg"), []byte("<svg/>"), 0o644)
	return front
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallFrontendZipKeepsScp(t *testing.T) {
	front := oldFrontend(t)
	zp := filepath.Join(filepath.Dir(front), "f.zip")
	writeZip(t, zp, map[string]string{"dist/index.html": "NEW", "dist/assets/a.js": "js"})

	if err := installFrontendZip(zp, front); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(front, "index.html")) != "NEW" || readFile(t, filepath.Join(front, "scp", "logo.svg")) != "<svg/>" {
		t.Fatal("新前端未生效或 scp/ 丢失")
	}
	for _, d := range []string{front + ".new", front + ".old"} {
		if _, err := os.Stat(d); err == nil {
			t.Fatalf("残留目录: %s", d)
		}
	}
}

func TestInstallFrontendZipFailureKeepsOld(t *testing.T) {
	front := oldFrontend(t)
	for name, files := range map[string]map[string]string{
		"bad.zip":    nil,
		"nodist.zip": {"other/index.html": "x"},
		"slip.zip":   {"dist/../../evil.txt": "x", "dist/index.html": "NEW"},
	} {
		zp := filepath.Join(filepath.Dir(front), name)
		if files == nil {
			_ = os.WriteFile(zp, []byte("not a zip"), 0o644)
		} else {
			writeZip(t, zp, files)
		}
		if err := installFrontendZip(zp, front); err == nil {
			t.Fatalf("%s 应报错", name)
		}
		if readFile(t, filepath.Join(front, "index.html")) != "OLD" {
			t.Fatalf("%s 失败后旧前端被破坏", name)
		}
	}
}
