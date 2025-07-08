package client

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ProgressCallback функция для отслеживания прогресса передачи
type ProgressCallback func(bytesTransferred, totalBytes int64, percentage float64)

// ClientConfig конфигурация для оптимизации клиента
type ClientConfig struct {
	BufferSize     int           // Размер буфера для чтения файла (по умолчанию 64KB)
	MaxConcurrency int           // Максимальное количество параллельных загрузок
	Timeout        time.Duration // Таймаут для HTTP-клиента
	RetryAttempts  int           // Количество попыток при ошибке
	RetryDelay     time.Duration // Задержка между попытками
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		BufferSize:     64 * 1024, // 64KB
		MaxConcurrency: runtime.NumCPU(),
		Timeout:        30 * time.Minute,
		RetryAttempts:  3,
		RetryDelay:     time.Second,
	}
}

// HTTPClient HTTP-клиент для потоковой передачи файлов
type HTTPClient struct {
	client *http.Client
	config *ClientConfig
	sem    chan struct{} // Семафор для ограничения параллельных загрузок
}

// NewHTTPClient создает новый HTTP-клиент
func NewHTTPClient(timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
		config: DefaultConfig(),
		sem:    make(chan struct{}, runtime.NumCPU()),
	}
}

// NewHTTPClientWithConfig создает новый HTTP-клиент с кастомной конфигурацией
func NewHTTPClientWithConfig(config *ClientConfig) *HTTPClient {
	if config == nil {
		config = DefaultConfig()
	}

	// Оптимизируем HTTP-клиент для высоких нагрузок
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true, // Отключаем сжатие для бинарных данных
	}

	return &HTTPClient{
		client: &http.Client{
			Timeout:   config.Timeout,
			Transport: transport,
		},
		config: config,
		sem:    make(chan struct{}, config.MaxConcurrency),
	}
}

// UploadFile выполняет потоковую загрузку файла на сервер
func (c *HTTPClient) UploadFile(ctx context.Context, filePath, serverURL string, progressCallback ProgressCallback) error {
	// Получаем семафор для ограничения параллельных загрузок
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	var lastErr error
	for attempt := 0; attempt <= c.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.config.RetryDelay):
			}
		}

		err := c.uploadFileOnce(ctx, filePath, serverURL, progressCallback)
		if err == nil {
			return nil
		}

		lastErr = err
		// Не повторяем попытки для определенных ошибок
		if isPermanentError(err) {
			break
		}
	}

	return fmt.Errorf("загрузка не удалась после %d попыток, последняя ошибка: %w", c.config.RetryAttempts+1, lastErr)
}

// uploadFileOnce выполняет одну попытку загрузки файла
func (c *HTTPClient) uploadFileOnce(ctx context.Context, filePath, serverURL string, progressCallback ProgressCallback) error {
	// Открываем файл для чтения
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("ошибка открытия файла: %w", err)
	}
	defer file.Close()

	// Получаем информацию о файле
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("ошибка получения информации о файле: %w", err)
	}

	fileSize := fileInfo.Size()
	if fileSize == 0 {
		return fmt.Errorf("файл пустой")
	}

	// Создаем pipe для потоковой передачи
	pr, pw := io.Pipe()
	defer pr.Close()

	// Создаем multipart writer
	multipartWriter := multipart.NewWriter(pw)

	// Канал для синхронизации завершения горутины
	done := make(chan error, 1)

	// Запускаем горутину для записи данных в pipe
	go func() {
		defer pw.Close()
		defer multipartWriter.Close()

		// Создаем поле для файла
		part, err := multipartWriter.CreateFormFile("file", filepath.Base(filePath))
		if err != nil {
			done <- fmt.Errorf("ошибка создания поля формы: %w", err)
			return
		}

		// Используем конфигурируемый размер буфера
		buffer := make([]byte, c.config.BufferSize)
		var bytesTransferred int64

		for {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			default:
				n, err := file.Read(buffer)
				if n > 0 {
					_, writeErr := part.Write(buffer[:n])
					if writeErr != nil {
						done <- fmt.Errorf("ошибка записи в pipe: %w", writeErr)
						return
					}

					bytesTransferred += int64(n)

					// Вызываем callback для отображения прогресса
					if progressCallback != nil {
						percentage := float64(bytesTransferred) / float64(fileSize) * 100
						progressCallback(bytesTransferred, fileSize, percentage)
					}
				}

				if err == io.EOF {
					done <- nil // Успешное завершение
					return
				}
				if err != nil {
					done <- fmt.Errorf("ошибка чтения файла: %w", err)
					return
				}
			}
		}
	}()

	// Создаем HTTP запрос
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL, pr)
	if err != nil {
		return fmt.Errorf("ошибка создания HTTP запроса: %w", err)
	}

	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	// Выполняем запрос
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка выполнения HTTP запроса: %w", err)
	}
	defer resp.Body.Close()

	// Ждем завершения горутины записи
	writeErr := <-done
	if writeErr != nil {
		return writeErr
	}

	// Проверяем статус ответа
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("сервер вернул ошибку: %s, статус: %d, тело: %s", resp.Status, resp.StatusCode, string(body))
	}

	return nil
}

// isPermanentError определяет, является ли ошибка постоянной (не требует retry)
func isPermanentError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Ошибки, которые не стоит повторять
	permanentErrors := []string{
		"ошибка открытия файла",
		"файл пустой",
		"ошибка создания поля формы",
		"ошибка чтения файла",
		"ошибка записи в pipe",
	}

	for _, permanentErr := range permanentErrors {
		if strings.Contains(errStr, permanentErr) {
			return true
		}
	}

	return false
}

// UploadFileWithProgress выполняет загрузку файла с автоматическим отображением прогресса
func (c *HTTPClient) UploadFileWithProgress(ctx context.Context, filePath, serverURL string) error {
	var mu sync.Mutex
	var lastUpdate time.Time

	progressCallback := func(bytesTransferred, totalBytes int64, percentage float64) {
		mu.Lock()
		defer mu.Unlock()

		// Обновляем прогресс не чаще чем раз в секунду
		if time.Since(lastUpdate) >= time.Second {
			fmt.Printf("\rПрогресс: %.2f%% (%s / %s)",
				percentage,
				formatBytes(bytesTransferred),
				formatBytes(totalBytes))
			lastUpdate = time.Now()
		}
	}

	err := c.UploadFile(ctx, filePath, serverURL, progressCallback)
	if err != nil {
		fmt.Printf("\nОшибка: %v\n", err)
		return err
	}

	fmt.Printf("\nЗагрузка завершена успешно!\n")
	return nil
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

// UploadMultipleFiles загружает несколько файлов параллельно
func (c *HTTPClient) UploadMultipleFiles(ctx context.Context, files []string, serverURL string, progressCallback ProgressCallback) error {
	if len(files) == 0 {
		return fmt.Errorf("список файлов пуст")
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(files))

	// Создаем контекст с отменой для всех горутин
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Запускаем загрузку каждого файла в отдельной горутине
	for _, filePath := range files {
		wg.Add(1)
		go func(file string) {
			defer wg.Done()

			// Создаем отдельный callback для каждого файла
			fileProgressCallback := func(bytesTransferred, totalBytes int64, percentage float64) {
				if progressCallback != nil {
					progressCallback(bytesTransferred, totalBytes, percentage)
				}
			}

			err := c.UploadFile(ctx, file, serverURL, fileProgressCallback)
			if err != nil {
				select {
				case errors <- fmt.Errorf("ошибка загрузки файла %s: %w", file, err):
				case <-ctx.Done():
				}
			}
		}(filePath)
	}

	// Ждем завершения всех загрузок
	wg.Wait()
	close(errors)

	// Собираем все ошибки
	var allErrors []string
	for err := range errors {
		allErrors = append(allErrors, err.Error())
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("ошибки при загрузке файлов: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// UploadDirectory загружает все файлы из директории
func (c *HTTPClient) UploadDirectory(ctx context.Context, dirPath, serverURL string, progressCallback ProgressCallback) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения директории: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			filePath := filepath.Join(dirPath, entry.Name())
			files = append(files, filePath)
		}
	}

	return c.UploadMultipleFiles(ctx, files, serverURL, progressCallback)
}
