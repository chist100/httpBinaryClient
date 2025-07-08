package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"httpBinaryClient/client"
	"httpBinaryClient/server"
)

func main() {
	var (
		mode      = flag.String("mode", "client", "Режим работы: client или server")
		port      = flag.String("port", "8080", "Порт для сервера")
		filePath  = flag.String("file", "", "Путь к файлу для загрузки (для клиента)")
		serverURL = flag.String("url", "http://localhost:8080/upload", "URL сервера для загрузки (для клиента)")
		timeout   = flag.Duration("timeout", 30*time.Minute, "Таймаут для HTTP-клиента")
	)
	flag.Parse()

	switch *mode {
	case "server":
		runServer(*port)
	case "client":
		if *filePath == "" {
			log.Fatal("Для клиента необходимо указать путь к файлу через -file")
		}
		runClient(*filePath, *serverURL, *timeout)
	default:
		log.Fatal("Неизвестный режим. Используйте 'client' или 'server'")
	}
}

func runServer(port string) {
	// Создаем и запускаем сервер
	srv := server.NewHTTPServer(port)

	// Обработка сигналов для graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nПолучен сигнал завершения, останавливаем сервер...")
		if err := srv.Stop(); err != nil {
			log.Printf("Ошибка остановки сервера: %v", err)
		}
	}()

	if err := srv.Start(); err != nil {
		log.Fatal("Ошибка запуска сервера:", err)
	}
}

func runClient(filePath, serverURL string, timeout time.Duration) {
	// Проверяем существование файла
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Fatalf("Файл не найден: %s", filePath)
	}

	// Создаем HTTP-клиент
	httpClient := client.NewHTTPClient(timeout)

	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fmt.Printf("Начинаем загрузку файла: %s\n", filePath)
	fmt.Printf("Сервер: %s\n", serverURL)
	fmt.Printf("Таймаут: %v\n\n", timeout)

	// Выполняем загрузку файла
	if err := httpClient.UploadFileWithProgress(ctx, filePath, serverURL); err != nil {
		log.Fatalf("Ошибка загрузки файла: %v", err)
	}
}
