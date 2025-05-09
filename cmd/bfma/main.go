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

	"github.com/skalibog/bfma/internal/analysis/aggregator"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/exchange"
	"github.com/skalibog/bfma/internal/storage"
	"github.com/skalibog/bfma/internal/ui"
)

func main() {
	// Обработка флагов командной строки
	configPath := flag.String("config", "config.yaml", "путь к файлу конфигурации")
	flag.Parse()

	// Загружаем конфигурацию
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	// Создаем контекст с возможностью отмены
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Инициализируем хранилище
	store, err := storage.NewInfluxDBStorage(cfg.Storage)
	if err != nil {
		log.Fatalf("Ошибка подключения к хранилищу данных: %v", err)
	}
	defer store.Close()

	// Инициализируем клиент биржи
	client, err := exchange.NewBinanceClient(cfg.Binance)
	if err != nil {
		log.Fatalf("Ошибка создания клиента Binance: %v", err)
	}

	// Создаем агрегатор аналитики
	analyzer := aggregator.NewAnalyzer(cfg.Analysis, store, client)

	// Инициализируем UI
	userInterface, err := ui.NewTermUI(cfg.UI, analyzer)
	if err != nil {
		log.Fatalf("Ошибка создания интерфейса: %v", err)
	}

	// Запускаем сборщики данных
	dataCollectors := []exchange.DataCollector{
		exchange.NewCandleCollector(client, store, cfg.Trading.Symbols, cfg.Trading.Interval),
		exchange.NewOrderBookCollector(client, store, cfg.Trading.Symbols, cfg.Analysis.OrderBook.Depth),
		exchange.NewFundingRateCollector(client, store, cfg.Trading.Symbols),
		exchange.NewOpenInterestCollector(client, store, cfg.Trading.Symbols),
	}

	for _, collector := range dataCollectors {
		if err := collector.Start(ctx); err != nil {
			log.Fatalf("Ошибка запуска сборщика данных: %v", err)
		}
		defer collector.Stop()
	}

	// Запускаем аналитический процесс
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.Analysis.IntervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				signals, err := analyzer.GenerateSignals(ctx)
				if err != nil {
					log.Printf("Ошибка при генерации сигналов: %v", err)
					continue
				}
				userInterface.UpdateSignals(signals)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Запускаем UI
	go userInterface.Start()

	// Ожидаем сигнала для завершения
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nЗавершение работы...")
	cancel()
	time.Sleep(time.Second) // Даем горутинам время на завершение
}