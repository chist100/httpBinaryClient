package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func BenchmarkUploadFile(b *testing.B) {
	// Создаем тестовый файл
	testFile := createTestFile(b, 1024*1024) // 1MB
	defer os.Remove(testFile)

	// Создаем простой HTTP сервер для тестирования
	server := createTestServer(b)
	defer server.Close()

	// Тестируем разные конфигурации
	configs := []struct {
		name   string
		config *ClientConfig
	}{
		{
			name: "Default",
			config: &ClientConfig{
				BufferSize:     64 * 1024,
				MaxConcurrency: 1,
				Timeout:        30 * time.Minute,
				RetryAttempts:  0,
			},
		},
		{
			name: "HighPerformance",
			config: &ClientConfig{
				BufferSize:     256 * 1024,
				MaxConcurrency: 1,
				Timeout:        30 * time.Minute,
				RetryAttempts:  0,
			},
		},
		{
			name: "LargeBuffer",
			config: &ClientConfig{
				BufferSize:     1024 * 1024,
				MaxConcurrency: 1,
				Timeout:        30 * time.Minute,
				RetryAttempts:  0,
			},
		},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			client := NewHTTPClientWithConfig(cfg.config)
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := client.UploadFile(ctx, testFile, server.URL+"/upload", nil)
				if err != nil {
					b.Fatalf("Upload failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkParallelUploads(b *testing.B) {
	// Тестируем разные уровни параллелизма
	concurrencyLevels := []int{1, 2, 4, 8}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			// Создаем несколько тестовых файлов для каждого теста
			testFiles := make([]string, 4)
			for i := range testFiles {
				testFiles[i] = createTestFile(b, 256*1024) // 256KB каждый
				defer os.Remove(testFiles[i])
			}

			// Создаем простой HTTP сервер для тестирования
			server := createTestServer(b)
			defer server.Close()

			config := &ClientConfig{
				BufferSize:     256 * 1024,
				MaxConcurrency: concurrency,
				Timeout:        30 * time.Minute,
				RetryAttempts:  0,
			}

			client := NewHTTPClientWithConfig(config)
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := client.UploadMultipleFiles(ctx, testFiles, server.URL+"/upload", nil)
				if err != nil {
					b.Fatalf("Parallel upload failed: %v", err)
				}
			}
		})
	}
}

// createTestFile создает временный тестовый файл заданного размера
func createTestFile(b *testing.B, size int) string {
	file, err := os.CreateTemp("", "benchmark_test_*.bin")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer file.Close()

	// Заполняем файл случайными данными
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	_, err = file.Write(data)
	if err != nil {
		b.Fatalf("Failed to write test data: %v", err)
	}

	return file.Name()
}

// createTestServer создает простой HTTP сервер для тестирования
func createTestServer(b *testing.B) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Просто читаем и отбрасываем данные
		_, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
}
