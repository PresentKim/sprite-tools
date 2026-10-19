# sprite-tools

픽셀 아트 PNG를 일괄 처리하는 명령줄 도구입니다.

```
go build -o sprite-tools.exe .
sprite-tools help
```

## 명령

| 명령 | 별칭 | 설명 | 기본 출력 |
| --- | --- | --- | --- |
| `reduce` | `rd` | 비슷한 색상을 합쳐 색상 수를 줄임 (`-t` 로 강도 조절) | `*-reduced.png` |
| `slice` | `sl` | 스프라이트 시트를 프레임으로 자름 (`-size 16` / `-size 16x24`) | `*-0001.png` … |
| `downscale` | `ds` | 확대된 픽셀 아트를 1:1로 축소 (자동 감지, `-scale` 로 지정) | `*-downscaled.png` |
| `index` | `ix` | 인덱스(팔레트) PNG로 변환, 0번 색을 투명으로 고정 | `*-indexed.png` |
| `recolor` | `rc` | Lospec 팔레트 또는 팔레트 PNG의 색으로 변환 (`-p`) | `*-recolored.png` |

명령은 별칭 외에도 겹치지 않는 앞부분으로 줄여 쓸 수 있습니다 (`red` → reduce, `d` → downscale).
이전 이름(`simplify`, `split`, `palette`, `palette-convert`)도 계속 동작합니다.

## 공통 옵션

- `[경로...]` 파일·폴더를 여러 개 지정 가능. 생략하면 현재 폴더의 PNG.
- `-i` 폴더 탐색 시 파일 이름 패턴 (기본 `*.png`)
- `-r` 하위 폴더까지 탐색
- `-s` 출력 파일 접미사 / `-w` 원본 덮어쓰기
- `-n` 실제 처리 없이 대상 파일만 미리보기
- `-j` 동시 처리 스레드 수

이전 실행의 출력 파일(같은 접미사)은 자동으로 대상에서 제외되며, 저장은 임시 파일을 거쳐 원자적으로 이뤄집니다.
처리 중 실패한 파일이 있으면 종료 코드 1, 잘못된 사용법이면 2를 반환합니다.

## 예시

```
sprite-tools rd hero.png -t 0.08
sprite-tools ds assets -r -w
sprite-tools sl sheet.png -size 32 -o frames
sprite-tools rc -p otterisk-96 -n
```

다운로드한 Lospec 팔레트는 실행 파일 옆 `palettes/` 폴더에 캐시됩니다 (`recolor -list`).
