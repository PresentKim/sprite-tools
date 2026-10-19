package common

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type ProcessTask func(inputPath, outputPath string) error

func RunParallel(pattern, suffix string, overwrite bool, threads int, task ProcessTask) error {
	if threads <= 0 {
		threads = runtime.NumCPU()
	}

	var files []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		filename := filepath.Base(path)
		matched, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(filename))
		if matched {
			// overwrite가 false이고 suffix가 지정되어 있을 때만 접미사 중복 파일 건너뜀
			if !overwrite && suffix != "" && strings.HasSuffix(strings.TrimSuffix(filename, filepath.Ext(filename)), suffix) {
				return nil
			}
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(files) == 0 {
		fmt.Printf("No matching files found for pattern: '%s'\n", pattern)
		return nil
	}

	fmt.Printf("Processing %d files using %d threads...\n", len(files), threads)

	jobs := make(chan string, len(files))
	var wg sync.WaitGroup

	for w := 0; w < threads; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				outPath := path
				if !overwrite && suffix != "" {
					ext := filepath.Ext(path)
					outPath = strings.TrimSuffix(path, ext) + suffix + ext
				}

				// 동일 경로 덮어쓰기 시 안전하게 처리하기 위한 분기
				if path == outPath {
					tmpPath := path + ".tmp"
					if err := task(path, tmpPath); err == nil {
						os.Remove(path)
						os.Rename(tmpPath, path)
					} else {
						os.Remove(tmpPath)
						fmt.Printf("[ERROR] Failed to process %s: %v\n", path, err)
					}
				} else {
					if err := task(path, outPath); err != nil {
						fmt.Printf("[ERROR] Failed to process %s: %v\n", path, err)
					}
				}
			}
		}()
	}

	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()

	fmt.Println("All tasks completed.")
	return nil
}