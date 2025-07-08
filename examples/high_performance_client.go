package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"httpBinaryClient/client"
)

func main() {
	// Создаем оптимизированную конфигурацию для высоких нагрузок
	config := &client.ClientConfig{
		BufferSize:     256 * 1024, // 256KB буфер для лучшей производительности
		MaxConcurrency: 8,          // 8 параллельных загрузок
		Timeout:        60 * time.Minute,
		RetryAttempts:  5, // Больше попыток для надежности
		RetryDelay:     2 * time.Second,
	}

	// Создаем оптимизированный клиент
	httpClient := client.NewHTTPClientWithConfig(config)

	// Список файлов для загрузки
	files := []string{
		"test_files/binary_1KB.bin",
		"test_files/binary_10KB.bin",
		"test_files/binary_100KB.bin",
		"test_files/binary_1MB.bin",
	}

	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	fmt.Println("Начинаем параллельную загрузку файлов...")
	fmt.Printf("Конфигурация: буфер=%dKB, параллелизм=%d, retry=%d\n",
		config.BufferSize/1024, config.MaxConcurrency, config.RetryAttempts)

	// Общий прогресс для всех файлов
	var totalTransferred int64
	var mu sync.Mutex

	progressCallback := func(bytesTransferred, totalBytes int64, percentage float64) {
		mu.Lock()
		defer mu.Unlock()

		totalTransferred += bytesTransferred
		fmt.Printf("\rОбщий прогресс: %.2f%% (%s)",
			percentage,
			formatBytes(totalTransferred))
	}

	// Загружаем файлы параллельно
	err := httpClient.UploadMultipleFiles(ctx, files, "http://localhost:8080/upload", progressCallback)
	if err != nil {
		log.Fatalf("Ошибка загрузки файлов: %v", err)
	}

	fmt.Printf("\nВсе файлы загружены успешно!\n")
}

// formatBytes форматирует байты в читаемый вид
func formatBytes(bytes int64) string {
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

// Пример загрузки всей директории
func uploadDirectoryExample() {
	config := &client.ClientConfig{
		BufferSize:     512 * 1024, // 512KB для больших файлов
		MaxConcurrency: 4,          // Меньше параллелизма для больших файлов
		Timeout:        120 * time.Minute,
		RetryAttempts:  3,
		RetryDelay:     5 * time.Second,
	}

	httpClient := client.NewHTTPClientWithConfig(config)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	fmt.Println("Загружаем все файлы из директории test_files/...")

	err := httpClient.UploadDirectory(ctx, "test_files", "http://localhost:8080/upload", nil)
	if err != nil {
		log.Fatalf("Ошибка загрузки директории: %v", err)
	}

	fmt.Println("Директория загружена успешно!")
}
