package processor

import (
	"os"
	"strings"
	"testing"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// 이 패키지의 기존 테스트는 한국어 메시지(골든 해시 포함)를 기대하므로 한국어로 실행합니다.
// 한국어 카탈로그가 예전 문구를 그대로 재현하는지 확인하는 역할도 합니다. 영어는 TestEnglishMessages 에서 확인합니다.
func TestMain(m *testing.M) {
	if err := i18n.SetLanguage("ko"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestEnglishMessages(t *testing.T) {
	if err := i18n.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	defer i18n.SetLanguage("ko")

	_, msg, err := ReduceImage(goldenNRGBA(64, 48), 0.02)
	if err != nil || msg != "Colors 29 → 9" {
		t.Errorf("reduce: %q %v", msg, err)
	}
	_, _, err = ReduceImage(goldenInputs()[4].img, 0.02)
	if !common.IsSkip(err) || err.Error() != "Nothing to merge (colors: 1)" {
		t.Errorf("skip: %v", err)
	}
	_, _, err = DownscaleImage(goldenPaletted(40, 30), 4)
	if err == nil || err.Error() != "Image size 40x30 is not divisible by scale 4" {
		t.Errorf("error: %v", err)
	}
	if _, _, err := UpscaleImage(goldenNRGBA(4, 4), 1); err == nil || strings.ContainsAny(err.Error(), "가나다라마바사아자차카타파하") {
		t.Errorf("upscale error should be English: %v", err)
	}
}
