package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	testDir := "test_files"
	if err := os.MkdirAll(testDir, 0755); err != nil {
		fmt.Printf("Ошибка создания директории %s: %v\n", testDir, err)
		os.Exit(1)
	}

	sizes := []struct {
		name string
		size int64
	}{
		{"binary_1KB.bin", 1024},
		{"binary_10KB.bin", 10 * 1024},
		{"binary_100KB.bin", 100 * 1024},
		{"binary_1MB.bin", 1024 * 1024},
		{"binary_1GB.bin", 1024 * 1024 * 1024},
	}

	for _, fileInfo := range sizes {
		if err := generateBinaryFile(filepath.Join(testDir, fileInfo.name), fileInfo.size); err != nil {
			fmt.Printf("Ошибка создания файла %s: %v\n", fileInfo.name, err)
			continue
		}
		fmt.Printf("Создан бинарный файл: %s (%s)\n", fileInfo.name, formatBytesBin(fileInfo.size))
	}

	fmt.Println("\nВсе бинарные тестовые файлы созданы успешно!")
}

func generateBinaryFile(filePath string, size int64) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	buffer := make([]byte, 64*1024)
	remaining := size

	for remaining > 0 {
		blockSize := int64(len(buffer))
		if remaining < blockSize {
			blockSize = remaining
		}
		if _, err := rand.Read(buffer[:blockSize]); err != nil {
			return err
		}
		if _, err := file.Write(buffer[:blockSize]); err != nil {
			return err
		}
		remaining -= blockSize
	}
	return nil
}

func formatBytesBin(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
