package common

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBackupCreatedBeforeOverwrite(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "sub", "b.png")
	writeFile(t, a, "old-a")
	writeFile(t, b, "old-b")

	var results []Result
	o := Options{InPlace: true, Backup: true, Threads: 2}
	sum := RunFiles(context.Background(), []string{a, b}, o, func(in, out string) (string, error) {
		return "ok", os.WriteFile(out, []byte("new"), 0o644)
	}, func(r Result) { results = append(results, r) })

	if sum.Done != 2 || sum.Backups != 2 || len(sum.BackupDirs) != 2 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
	for _, r := range results {
		if r.Backup == "" {
			t.Fatalf("no backup path: %+v", r)
		}
		want := "old-a"
		if strings.HasSuffix(r.Input, "b.png") {
			want = "old-b"
		}
		if got := readFile(t, r.Backup); got != want {
			t.Errorf("%s backup content = %q, want %q", r.Input, got, want)
		}
		if got := readFile(t, r.Input); got != "new" {
			t.Errorf("%s overwrite result = %q", r.Input, got)
		}
		// Backup location: <file folder>/.sprite-tools-backup/<time>/<name>
		stampDir := filepath.Dir(r.Backup)
		if filepath.Base(r.Backup) != filepath.Base(r.Input) ||
			filepath.Base(filepath.Dir(stampDir)) != BackupDirName ||
			filepath.Dir(filepath.Dir(stampDir)) != filepath.Dir(r.Input) {
			t.Errorf("unexpected backup location: %s", r.Backup)
		}
	}
	// All backups of one run must share the same timestamp folder name.
	if filepath.Base(filepath.Dir(results[0].Backup)) != filepath.Base(filepath.Dir(results[1].Backup)) {
		t.Errorf("backup folders of one run have different timestamps")
	}
}

func TestBackupRemovedOnSkipAndFail(t *testing.T) {
	dir := t.TempDir()
	skip := filepath.Join(dir, "skip.png")
	fail := filepath.Join(dir, "fail.png")
	writeFile(t, skip, "s")
	writeFile(t, fail, "f")

	o := Options{InPlace: true, Backup: true, Threads: 1}
	sum := RunFiles(context.Background(), []string{skip, fail}, o, func(in, out string) (string, error) {
		if in == skip {
			return "", Skip("nothing changes")
		}
		return "", os.ErrInvalid
	}, func(r Result) {
		if r.Backup != "" {
			t.Errorf("a skipped/failed file kept a backup path: %+v", r)
		}
	})
	if sum.Skipped != 1 || sum.Failed != 1 || sum.Backups != 0 || len(sum.BackupDirs) != 0 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
	// The backup folder itself must not be left behind either.
	if _, err := os.Stat(filepath.Join(dir, BackupDirName)); !os.IsNotExist(err) {
		t.Errorf("empty backup folder left behind (err=%v)", err)
	}
}

func TestBackupOffByDefault(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	writeFile(t, a, "old")
	sum := RunFiles(context.Background(), []string{a}, Options{InPlace: true}, func(in, out string) (string, error) {
		return "", os.WriteFile(out, []byte("new"), 0o644)
	}, nil)
	if sum.Backups != 0 {
		t.Fatalf("backed up although Backup is off: %+v", sum)
	}
	if _, err := os.Stat(filepath.Join(dir, BackupDirName)); !os.IsNotExist(err) {
		t.Errorf("a backup folder was created")
	}
}

func TestBackupNotScanned(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.png"), "x")
	writeFile(t, filepath.Join(dir, BackupDirName, "20240101-000000", "a.png"), "old")
	files, err := CollectFiles(Options{Roots: []string{dir}, Recursive: true, InPlace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("backup folder included in the scan: %v", files)
	}
}

func TestValidateBackupNeedsInPlace(t *testing.T) {
	if err := (Options{Backup: true, Suffix: "_x"}).Validate(); err == nil {
		t.Error("enabling Backup without InPlace should be an error")
	}
	if err := (Options{Backup: true, InPlace: true}).Validate(); err != nil {
		t.Errorf("InPlace+Backup should be valid: %v", err)
	}
}

func TestRestoreBackup(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	writeFile(t, a, "old")

	var backup string
	RunFiles(context.Background(), []string{a}, Options{InPlace: true, Backup: true}, func(in, out string) (string, error) {
		return "", os.WriteFile(out, []byte("new"), 0o644)
	}, func(r Result) { backup = r.Backup })
	if readFile(t, a) != "new" {
		t.Fatal("the file was not overwritten")
	}

	if err := RestoreBackup(backup, a); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, a); got != "old" {
		t.Errorf("restore result = %q, want old", got)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("the backup file should remain after restore: %v", err)
	}
}

func TestRestoreBackupRejectsOutsideBackupDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x.png")
	dst := filepath.Join(dir, "y.png")
	writeFile(t, src, "x")
	writeFile(t, dst, "y")
	if err := RestoreBackup(src, dst); err == nil {
		t.Error("a file outside the backup folder must not be restorable")
	}
	if readFile(t, dst) != "y" {
		t.Error("a rejected restore changed the target file")
	}
}

// A backup file can only be restored to its original location (<folder>/<name>).
func TestRestoreBackupRejectsOtherDestination(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, BackupDirName, "20260101-000000", "a.png")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, backup, "old")
	other := t.TempDir()
	for _, dest := range []string{
		filepath.Join(dir, "b.png"),   // different name
		filepath.Join(other, "a.png"), // different folder
		filepath.Join(dir, "sub", "a.png"),
	} {
		if err := RestoreBackup(backup, dest); err == nil {
			t.Errorf("restoring to %s should be rejected", dest)
		}
		if _, err := os.Stat(dest); err == nil {
			t.Errorf("a rejected restore created %s", dest)
		}
	}
	if err := RestoreBackup(backup, filepath.Join(dir, "a.png")); err != nil {
		t.Errorf("restoring to the original location should be allowed: %v", err)
	}
}

func TestBackupFileNow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writeFile(t, p, "old")
	b, err := BackupFileNow(p)
	if err != nil || b == "" {
		t.Fatalf("backup failed: %q %v", b, err)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(b))) != BackupDirName || filepath.Base(b) != "a.png" {
		t.Errorf("unexpected backup location: %s", b)
	}
	if got := readFile(t, b); got != "old" {
		t.Errorf("backup content = %q", got)
	}
	if b, err := BackupFileNow(filepath.Join(dir, "none.png")); err != nil || b != "" {
		t.Errorf("a missing file should give an empty path: %q %v", b, err)
	}
}
