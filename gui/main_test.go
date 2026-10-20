package main

import (
	"os"
	"testing"

	"sprite-tools/pkg/i18n"
)

// The existing tests check Korean wording, so they run in Korean. English and switching are covered in TestLanguageMethods.
func TestMain(m *testing.M) {
	if err := i18n.SetLanguage("ko"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestLanguageMethods(t *testing.T) {
	defer i18n.SetLanguage("ko")
	a := NewApp()

	langs := a.Languages()
	if len(langs) < 2 || langs[0].Code != "en" || langs[0].Name != "English" {
		t.Fatalf("Languages() = %+v", langs)
	}
	if err := a.SetLanguage("en"); err != nil || a.Language() != "en" {
		t.Fatalf("SetLanguage(en): %v, %q", err, a.Language())
	}
	if got := dirtyMessage(3); got != "The editor has 3 unsaved documents. Quit without saving?" {
		t.Errorf("en dirtyMessage: %q", got)
	}
	if _, _, err := parseFrameSize("x"); err == nil || err.Error() != "Invalid frame size format (e.g. 16 or 16x24)" {
		t.Errorf("en parseFrameSize: %v", err)
	}
	if err := a.SetLanguage("ko-KR"); err != nil || a.Language() != "ko" {
		t.Fatalf("SetLanguage(ko-KR): %v, %q", err, a.Language())
	}
	if err := a.SetLanguage("xx"); err == nil || a.Language() != "ko" {
		t.Errorf("unknown language: %v, now %q", err, a.Language())
	}
}
