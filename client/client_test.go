package client

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadFile_FileNotFound(t *testing.T) {
	httpClient := NewHTTPClient(30 * time.Second)
	ctx := context.Background()

	// Пытаемся загрузить несуществующий файл
	err := httpClient.UploadFile(ctx, "/nonexistent/file.txt", "http://localhost:8080/upload", nil)

	if err == nil {
		t.Fatal("Ожидалась ошибка для несуществующего файла")
	}

	if !strings.Contains(err.Error(), "ошибка открытия файла") {
		t.Errorf("Ожидалась ошибка открытия файла, получена: %v", err)
	}
}

func TestUploadFile_EmptyFile(t *testing.T) {
	// Создаем пустой файл
	tempDir := t.TempDir()
	emptyFile := filepath.Join(tempDir, "empty.txt")

	err := os.WriteFile(emptyFile, []byte{}, 0644)
	if err != nil {
		t.Fatalf("Ошибка создания пустого файла: %v", err)
	}

	httpClient := NewHTTPClient(30 * time.Second)
	ctx := context.Background()

	// Пытаемся загрузить пустой файл
	err = httpClient.UploadFile(ctx, emptyFile, "http://localhost:8080/upload", nil)

	if err == nil {
		t.Fatal("Ожидалась ошибка для пустого файла")
	}

	if !strings.Contains(err.Error(), "файл пустой") {
		t.Errorf("Ожидалась ошибка пустого файла, получена: %v", err)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
		{1500, "1.5 KB"},
		{1536, "1.5 KB"},
	}

	for _, test := range tests {
		result := formatBytes(test.bytes)
		if result != test.expected {
			t.Errorf("Для %d байт ожидалось %s, получено %s", test.bytes, test.expected, result)
		}
	}
}

// TestUploadFile_Integration - интеграционный тест с реальным сервером
func TestUploadFile_Integration(t *testing.T) {
	// Проверяем наличие тестового файла
	testFile := "../test_files/binary_1KB.bin"
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Skipf("Тестовый файл %s не найден, пропускаем интеграционный тест", testFile)
	}

	// Этот тест требует запущенного сервера
	// Для запуска: go run main.go -mode=server -port=8080
	serverURL := "http://localhost:8080/upload"

	httpClient := NewHTTPClient(10 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Тестируем загрузку файла
	var progressCalled bool
	progressCallback := func(bytesTransferred, totalBytes int64, percentage float64) {
		progressCalled = true
		t.Logf("Прогресс: %.2f%% (%d / %d байт)", percentage, bytesTransferred, totalBytes)
	}

	err := httpClient.UploadFile(ctx, testFile, serverURL, progressCallback)
	if err != nil {
		// Если сервер не запущен, это нормально
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "context deadline exceeded") {
			t.Skipf("Сервер не запущен, пропускаем тест: %v", err)
		}
		t.Fatalf("Ошибка загрузки файла: %v", err)
	}

	if !progressCalled {
		t.Error("Progress callback не был вызван")
	}
}
