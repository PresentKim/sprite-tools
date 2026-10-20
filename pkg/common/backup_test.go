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
		t.Fatalf("요약이 예상과 다릅니다: %+v", sum)
	}
	for _, r := range results {
		if r.Backup == "" {
			t.Fatalf("백업 경로가 없습니다: %+v", r)
		}
		want := "old-a"
		if strings.HasSuffix(r.Input, "b.png") {
			want = "old-b"
		}
		if got := readFile(t, r.Backup); got != want {
			t.Errorf("%s 백업 내용 = %q, want %q", r.Input, got, want)
		}
		if got := readFile(t, r.Input); got != "new" {
			t.Errorf("%s 덮어쓰기 결과 = %q", r.Input, got)
		}
		// 백업 위치: <파일 폴더>/.sprite-tools-backup/<시각>/<이름>
		stampDir := filepath.Dir(r.Backup)
		if filepath.Base(r.Backup) != filepath.Base(r.Input) ||
			filepath.Base(filepath.Dir(stampDir)) != BackupDirName ||
			filepath.Dir(filepath.Dir(stampDir)) != filepath.Dir(r.Input) {
			t.Errorf("백업 위치가 예상과 다릅니다: %s", r.Backup)
		}
	}
	// 한 번 실행에는 시각 폴더 이름이 같아야 합니다.
	if filepath.Base(filepath.Dir(results[0].Backup)) != filepath.Base(filepath.Dir(results[1].Backup)) {
		t.Errorf("한 번 실행의 백업 폴더 시각이 서로 다릅니다")
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
			return "", Skip("바뀌는 것이 없음")
		}
		return "", os.ErrInvalid
	}, func(r Result) {
		if r.Backup != "" {
			t.Errorf("건너뜀/실패한 파일에 백업 경로가 남았습니다: %+v", r)
		}
	})
	if sum.Skipped != 1 || sum.Failed != 1 || sum.Backups != 0 || len(sum.BackupDirs) != 0 {
		t.Fatalf("요약이 예상과 다릅니다: %+v", sum)
	}
	// 백업 폴더 자체도 남지 않아야 합니다.
	if _, err := os.Stat(filepath.Join(dir, BackupDirName)); !os.IsNotExist(err) {
		t.Errorf("빈 백업 폴더가 남았습니다 (err=%v)", err)
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
		t.Fatalf("Backup 이 꺼져 있는데 백업됨: %+v", sum)
	}
	if _, err := os.Stat(filepath.Join(dir, BackupDirName)); !os.IsNotExist(err) {
		t.Errorf("백업 폴더가 만들어졌습니다")
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
		t.Fatalf("백업 폴더가 스캔에 포함됨: %v", files)
	}
}

func TestValidateBackupNeedsInPlace(t *testing.T) {
	if err := (Options{Backup: true, Suffix: "_x"}).Validate(); err == nil {
		t.Error("InPlace 없이 Backup 을 켜면 오류여야 합니다")
	}
	if err := (Options{Backup: true, InPlace: true}).Validate(); err != nil {
		t.Errorf("InPlace+Backup 은 유효해야 합니다: %v", err)
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
		t.Fatal("덮어쓰기가 되지 않았습니다")
	}

	if err := RestoreBackup(backup, a); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, a); got != "old" {
		t.Errorf("복원 결과 = %q, want old", got)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("복원 후에도 백업 파일은 남아 있어야 합니다: %v", err)
	}
}

func TestRestoreBackupRejectsOutsideBackupDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x.png")
	dst := filepath.Join(dir, "y.png")
	writeFile(t, src, "x")
	writeFile(t, dst, "y")
	if err := RestoreBackup(src, dst); err == nil {
		t.Error("백업 폴더 밖의 파일은 복원할 수 없어야 합니다")
	}
	if readFile(t, dst) != "y" {
		t.Error("거부된 복원이 대상 파일을 바꿨습니다")
	}
}

// 백업 파일은 원래 있던 위치(<폴더>/<이름>)로만 복원할 수 있어야 합니다.
func TestRestoreBackupRejectsOtherDestination(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, BackupDirName, "20260101-000000", "a.png")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, backup, "old")
	other := t.TempDir()
	for _, dest := range []string{
		filepath.Join(dir, "b.png"),   // 이름이 다름
		filepath.Join(other, "a.png"), // 폴더가 다름
		filepath.Join(dir, "sub", "a.png"),
	} {
		if err := RestoreBackup(backup, dest); err == nil {
			t.Errorf("%s 로의 복원은 거부되어야 합니다", dest)
		}
		if _, err := os.Stat(dest); err == nil {
			t.Errorf("거부된 복원이 %s 를 만들었습니다", dest)
		}
	}
	if err := RestoreBackup(backup, filepath.Join(dir, "a.png")); err != nil {
		t.Errorf("원래 위치로의 복원은 허용되어야 합니다: %v", err)
	}
}
