package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ProgressCallback функция для отслеживания прогресса приема
type ProgressCallback func(bytesReceived, totalBytes int64, percentage float64)

// HTTPServer HTTP-сервер для приема файлов
type HTTPServer struct {
	server *http.Server
	port   string
}

// NewHTTPServer создает новый HTTP-сервер
func NewHTTPServer(port string) *HTTPServer {
	return &HTTPServer{
		port: port,
	}
}

// Start запускает HTTP-сервер
func (s *HTTPServer) Start() error {
	mux := http.NewServeMux()

	// Обработчик для загрузки файлов
	mux.HandleFunc("/upload", s.handleUpload)

	// Простой обработчик для проверки работы сервера
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HTTP File Upload Server is running"))
	})

	s.server = &http.Server{
		Addr:    ":" + s.port,
		Handler: mux,
	}

	fmt.Printf("Сервер запущен на порту %s\n", s.port)
	fmt.Printf("Для загрузки файлов используйте: http://localhost:%s/upload\n", s.port)

	return s.server.ListenAndServe()
}

// Stop останавливает HTTP-сервер
func (s *HTTPServer) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

// handleUpload обрабатывает загрузку файлов
func (s *HTTPServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Парсим multipart форму
	err := r.ParseMultipartForm(32 << 20) // 32MB max memory
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка парсинга формы: %v", err), http.StatusBadRequest)
		return
	}

	// Получаем файл из формы
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка получения файла: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Создаем директорию для сохранения файлов
	uploadDir := "uploads"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Ошибка создания директории: %v", err), http.StatusInternalServerError)
		return
	}

	// Создаем файл для сохранения
	filePath := filepath.Join(uploadDir, header.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка создания файла: %v", err), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Получаем размер файла (если доступен)
	contentLength := r.ContentLength
	if contentLength <= 0 {
		// Если размер не известен, попробуем получить из заголовка
		if header.Size > 0 {
			contentLength = header.Size
		}
	}

	// Время начала загрузки
	startTime := time.Now()

	fmt.Printf("\n=== НАЧАЛО ЗАГРУЗКИ ===\n")
	fmt.Printf("Файл: %s\n", header.Filename)
	fmt.Printf("Размер: %s\n", formatBytes(contentLength))
	fmt.Printf("Время начала: %s\n", startTime.Format("15:04:05"))
	fmt.Printf("IP клиента: %s\n", r.RemoteAddr)
	fmt.Printf("User-Agent: %s\n", r.UserAgent())
	fmt.Printf("========================\n\n")

	// Создаем прогресс-бар с дополнительной информацией
	var mu sync.Mutex
	var lastUpdate time.Time
	var bytesReceived int64
	var lastBytesReceived int64
	var lastUpdateTime time.Time

	progressCallback := func(bytesReceived, totalBytes int64, percentage float64) {
		mu.Lock()
		defer mu.Unlock()

		now := time.Now()

		// Обновляем прогресс не чаще чем раз в секунду
		if now.Sub(lastUpdate) >= time.Second {
			// Вычисляем скорость передачи
			timeDiff := now.Sub(lastUpdateTime).Seconds()
			if timeDiff > 0 {
				bytesDiff := bytesReceived - lastBytesReceived
				speed := float64(bytesDiff) / timeDiff

				// Вычисляем оставшееся время
				var eta string
				if speed > 0 && totalBytes > bytesReceived {
					remainingBytes := totalBytes - bytesReceived
					etaSeconds := float64(remainingBytes) / speed
					eta = formatDuration(time.Duration(etaSeconds) * time.Second)
				} else {
					eta = "вычисляется..."
				}

				// Вычисляем прошедшее время
				elapsed := now.Sub(startTime)

				fmt.Printf("\r[%s] Прием: %.2f%% (%s / %s) | Скорость: %s/s | Прошло: %s | Осталось: %s",
					now.Format("15:04:05"),
					percentage,
					formatBytes(bytesReceived),
					formatBytes(totalBytes),
					formatBytes(int64(speed)),
					formatDuration(elapsed),
					eta)

				lastUpdate = now
				lastBytesReceived = bytesReceived
				lastUpdateTime = now
			}
		}
	}

	// Буфер для чтения данных
	buffer := make([]byte, 64*1024) // 64KB буфер

	// Читаем и записываем файл по частям
	for {
		n, err := file.Read(buffer)
		if n > 0 {
			_, writeErr := dst.Write(buffer[:n])
			if writeErr != nil {
				http.Error(w, fmt.Sprintf("Ошибка записи файла: %v", writeErr), http.StatusInternalServerError)
				return
			}

			bytesReceived += int64(n)

			// Вызываем callback для отображения прогресса
			if contentLength > 0 {
				percentage := float64(bytesReceived) / float64(contentLength) * 100
				progressCallback(bytesReceived, contentLength, percentage)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Ошибка чтения файла: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Время окончания загрузки
	endTime := time.Now()
	totalDuration := endTime.Sub(startTime)

	// Вычисляем среднюю скорость
	var avgSpeed float64
	if totalDuration.Seconds() > 0 {
		avgSpeed = float64(bytesReceived) / totalDuration.Seconds()
	}

	fmt.Printf("\n\n=== ЗАГРУЗКА ЗАВЕРШЕНА ===\n")
	fmt.Printf("Файл: %s\n", header.Filename)
	fmt.Printf("Путь сохранения: %s\n", filePath)
	fmt.Printf("Размер принятых данных: %s\n", formatBytes(bytesReceived))
	fmt.Printf("Время начала: %s\n", startTime.Format("15:04:05"))
	fmt.Printf("Время окончания: %s\n", endTime.Format("15:04:05"))
	fmt.Printf("Общее время: %s\n", formatDuration(totalDuration))
	fmt.Printf("Средняя скорость: %s/s\n", formatBytes(int64(avgSpeed)))
	fmt.Printf("==========================\n\n")

	// Отправляем ответ клиенту
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Файл %s успешно загружен", header.Filename)))
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

// formatDuration форматирует время в читаемый вид
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Second).String()
}
