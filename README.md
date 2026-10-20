# sprite-tools

픽셀 아트 PNG를 일괄 처리하는 도구입니다. 명령줄(CLI)과 GUI 두 가지로 쓸 수 있으며, 둘은 같은 처리 코드를 공유합니다.

```
go build -o sprite-tools.exe .
sprite-tools help
```

## GUI

`gui/` 폴더에 [Wails](https://wails.io) 기반 데스크톱 앱이 있습니다.

```
cd gui
wails build        # → gui/build/bin/sprite-tools-gui.exe
wails dev          # 개발 모드 (화면 수정 시 자동 갱신)
```

- PNG 파일이나 폴더를 창에 끌어다 놓아 추가 (하위 폴더 포함 여부 선택)
- 위쪽 탭: 색상 줄이기 · 팔레트 적용 · 인덱스 PNG · 팔레트 추출 | 1:1 축소 · 확대 · 여백 자르기 | 프레임 자르기 · 시트 합치기
- 원본/결과 미리보기: 옵션을 바꾸면 바로 갱신되며, 픽셀이 뭉개지지 않게 정수 배율로 확대
- 결과 색상 견본, 팔레트 견본, Lospec 팔레트 불러오기
- 저장 방식: 원본 옆에 새 파일 / 다른 폴더에 저장(폴더에서 찾은 파일은 하위 폴더 구조 유지, 접미사는 비워도 됨) / 원본 덮어쓰기
- 팔레트 추출: 결과 칸에 추출된 색 견본이 보이며, 이름을 주면 '팔레트 적용'의 팔레트 목록에도 바로 저장
- 시트 합치기: 목록의 모든 파일을 이름의 숫자 순서(frame-2, frame-10)로 합친 시트를 미리보고, 실행하면 저장 위치를 물어봄
- 원본 덮어쓰기 전 백업(기본 켜짐): 각 파일 옆 `.sprite-tools-backup/<실행 시각>/` 에 원본을 복사해 두고, 실행이 끝난 알림의 `되돌리기` 로 한 번에 복원 (`백업 폴더 열기` 도 제공). 건너뛰거나 실패한 파일의 백업은 남기지 않음
- 미리보기 검사: 원본/결과 위에 마우스를 올리면 캡션에 픽셀 좌표 `x, y` 와 색상 `#rrggbb`·알파 표시. 결과 창을 누르고 있거나 `Space` 를 누르는 동안 원본을 보여주는 비교, 확대 시 두 창의 스크롤 동기화, `Ctrl+휠` 로 확대 단계(맞춤 · 1x · 2x · 4x · 8x · 16x) 변경
- 파일 목록 필터: 이름으로 걸러 보기(`Ctrl+F`, `Esc` 로 지움). 필터 중에도 실행은 기본적으로 목록 전체이며(버튼에 `전체 n개` 표시), `보이는 n개만 실행` 을 체크하면 보이는 파일만 처리
- 실행 후 알림의 `결과를 목록에 추가` 로 만들어진 파일(프레임 자르기·시트 합치기 결과 포함)을 바로 목록에 추가
- 진행률 표시, 취소(Esc), 결과 폴더 열기
- 단축키: `Ctrl+Enter` 실행, `Esc` 취소/닫기, `Alt+1~9` 탭 순서대로 명령 전환, `↑/↓` 파일 이동, `Delete` 목록에서 제거, `Ctrl+F` 파일 필터, `Space` 누르는 동안 원본 비교, `Ctrl+휠` 확대, `F1`/`?` 단축키 도움말 (상단 `?` 버튼도 같음)
- 설정은 다음 실행 때도 유지됩니다

빌드에는 Go, Node.js, Wails CLI(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)가 필요하며, 실행에는 WebView2(Windows 10/11 기본 포함)가 필요합니다.

## 명령

| 명령 | 별칭 | 설명 | 기본 출력 |
| --- | --- | --- | --- |
| `reduce` | `rd` | 비슷한 색상을 합쳐 색상 수를 줄임 (`-t` 로 강도 조절, `-k 16` 으로 최대 색상 수 지정) | `*-reduced.png` |
| `recolor` | `rc` | Lospec 팔레트 또는 팔레트 PNG의 색으로 변환 (`-p`) | `*-recolored.png` |
| `index` | `ix` | 인덱스(팔레트) PNG로 변환, 0번 색을 투명으로 고정 | `*-indexed.png` |
| `extract` | `ex` | 이미지의 색을 팔레트 PNG(색 1개 = 1px, 한 줄)로 추출. 많이 쓴 색 순, 256색 초과 시 실패. `-save <이름>` 으로 팔레트 폴더에도 저장 | `*-palette.png` |
| `downscale` | `ds` | 확대된 픽셀 아트를 1:1로 축소 (자동 감지, `-scale` 로 지정) | `*-downscaled.png` |
| `upscale` | `us` | 정수 배율 최근접 확대 (`-scale 2~32`, 기본 2) | `*-upscaled.png` |
| `trim` | `tr` | 투명한 바깥 여백을 잘라냄 (`-pad N` 으로 사방에 투명 여백 추가) | `*-trimmed.png` |
| `slice` | `sl` | 스프라이트 시트를 프레임으로 자름 (`-size 16` / `-size 16x24`) | `*-0001.png` … |
| `pack` | `pk` | 여러 이미지를 한 장의 시트로 합침 (`slice` 의 반대). `-cols`, `-gap`, `-align center`, 출력 `-o <파일.png>` 필수 | `-o` 로 지정한 파일 |

명령은 별칭 외에도 겹치지 않는 앞부분으로 줄여 쓸 수 있습니다 (`red` → reduce, `d` → downscale).
이전 이름(`simplify`, `split`, `palette`, `palette-convert`)도 계속 동작합니다.

## 공통 옵션

`pack` 을 뺀 파일별 명령(reduce, recolor, index, extract, downscale, upscale, trim)에 해당합니다.

- `[경로...]` 파일·폴더를 여러 개 지정 가능. 생략하면 현재 폴더의 PNG.
- `-i` 폴더 탐색 시 파일 이름 패턴 (기본 `*.png`)
- `-r` 하위 폴더까지 탐색
- `-s` 출력 파일 접미사 / `-w` 원본 덮어쓰기 (`extract` 는 `-w` 없음)
- `-backup` `-w` 로 덮어쓰기 전에 원본을 `<파일 폴더>/.sprite-tools-backup/<실행 시각 YYYYMMDD-HHMMSS>/<이름>` 에 복사 (`-w` 와 함께 쓰기를 권장, 기본은 꺼짐). 건너뛰거나 실패한 파일의 백업은 지우며, 백업이 남은 폴더는 종료 시 요약에 출력. 숨김 폴더(`.` 시작)라 이후 탐색에서 제외됨. 되돌리려면 백업 파일을 원래 자리로 복사
- `-o <폴더>` 결과를 다른 폴더에 저장. 폴더로 지정한 경로 아래에서 찾은 파일은 그 폴더 기준의 하위 경로를 유지하고(같은 이름이 달라도 겹치지 않음), 파일로 직접 지정한 경우에는 출력 폴더 바로 아래에 저장. 폴더는 자동으로 만들며 `-w` 와는 함께 쓸 수 없음.
  접미사는 비워도 되지만(`-s ""`), 출력이 원본과 같은 경로가 되는 파일은 실패로 처리해 원본을 지키며, 출력 폴더 안쪽 파일은 탐색에서 제외
- `-n` 실제 처리 없이 대상 파일과 실제 출력 경로만 미리보기
- `-j` 동시 처리 스레드 수

(`slice` 의 `-o` 는 프레임을 저장할 폴더이고, `pack` 의 `-o` 는 저장할 시트 파일입니다.)

이전 실행의 출력 파일(같은 접미사)은 자동으로 대상에서 제외되며, 저장은 임시 파일을 거쳐 원자적으로 이뤄집니다.
처리 중 실패한 파일이 있으면 종료 코드 1, 잘못된 사용법이면 2를 반환합니다.

## pack

```
sprite-tools pack frames -o sheet.png                   # 폴더의 모든 PNG
sprite-tools pk frames -cols 4 -gap 1 -align center -o sheet.png
```

- 입력은 자연 정렬 순서(`frame-2` < `frame-10`)로 합치며, 칸 크기는 가장 큰 너비 × 가장 큰 높이입니다. 작은 이미지는 칸의 왼쪽 위(`-align center` 면 가운데)에 놓입니다.
- `-cols 0` 이면 개수의 제곱근을 올림한 열 수, `-gap` 은 칸 사이 투명 간격입니다.
- 입력 파일을 덮어쓰는 출력은 항상 거부하고, 이미 있는 다른 파일은 `-force` 가 있어야 덮어씁니다. 폴더를 훑다가 같은 출력 파일이 잡히면 입력에서 뺍니다.
- 완료되면 `12프레임 → 4x3 시트, 64x48` 처럼 요약을 출력합니다.

## 예시

```
sprite-tools rd hero.png -t 0.08
sprite-tools ds assets -r -w
sprite-tools us sprites -r -scale 4 -o big
sprite-tools tr sprites -r -pad 1 -o trimmed -n
sprite-tools sl sheet.png -size 32 -o frames
sprite-tools pk frames -o sheet.png
sprite-tools ex hero.png -save hero-colors
sprite-tools rc -p hero-colors enemy.png
sprite-tools rc -p otterisk-96 -n
```

다운로드하거나 `extract -save` 로 저장한 팔레트는 사용자 폴더(`%APPDATA%\sprite-tools\palettes`)에 보관되어 CLI와 GUI가 함께 씁니다. 실행 파일 옆에 `palettes/` 폴더가 있으면 그쪽도 함께 찾습니다 (`recolor -list`).
