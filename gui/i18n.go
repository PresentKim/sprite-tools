package main

import "sprite-tools/pkg/i18n"

// 표시 언어: Go 쪽 메시지(오류, 처리 결과, 네이티브 대화상자)는 pkg/i18n 카탈로그를 따릅니다.
// 기본 언어는 en 이며, 프런트엔드가 저장된 설정으로 시작할 때 SetLanguage 를 호출합니다.

// SetLanguage: 표시 언어를 바꿉니다 ("en", "ko", "ko-KR" 등). 지원하지 않는 코드는 오류를 반환하고 언어는 그대로입니다.
func (a *App) SetLanguage(code string) error {
	return i18n.SetLanguage(code)
}

// Language: 현재 표시 언어의 코드("en", "ko" ...)를 반환합니다.
func (a *App) Language() string {
	return i18n.Language()
}

// Languages: 사용할 수 있는 언어 목록입니다. Name 은 각 언어 자신의 표기입니다 (English, 한국어).
func (a *App) Languages() []i18n.LangInfo {
	return i18n.Languages()
}
