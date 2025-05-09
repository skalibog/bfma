package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/skalibog/bfma/pkg/logger"
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
	"go.uber.org/zap"
)

func main() {
	logger.Init()
	defer logger.GetLogger().Sync()

	// Обработка флагов командной строки
	configPath := flag.String("config", "config.yaml", "путь к файлу конфигурации")
	flag.Parse()

	// Проверяем наличие файла конфигурации
	logger.Info("Проверка наличия файла конфигурации", zap.String("path", *configPath))
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		logger.Fatal("Файл конфигурации не найден", zap.String("path", *configPath))
	}

	// Загружаем конфигурацию
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("Ошибка загрузки конфигурации", zap.Error(err))
	}

	// Создаем контекст с возможностью отмены через горутину
	ctx, cancel := context.WithCancel(context.Background())

	// Настраиваем обработку сигналов завершения
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nЗавершение работы...")
		cancel()
		time.Sleep(5 * time.Second) // Даем горутинам время на завершение
		os.Exit(0)
	}()

	// Инициализируем хранилище
	store, err := storage.NewInfluxDBStorage(cfg.Storage)
	if err != nil {
		logger.Fatal("Ошибка инициализации хранилища", zap.Error(err))
	}
	defer store.Close()

	// Инициализируем клиент биржи
	client, err := exchange.NewBinanceClient(cfg.Binance)
	if err != nil {
		logger.Fatal("Ошибка инициализации клиента биржи", zap.Error(err))
	}

	// Создаем агрегатор аналитики
	analyzer := aggregator.NewAnalyzer(cfg.Analysis, store, client, cfg.Trading.Symbols)

	// Инициализируем UI
	userInterface, err := ui.NewTermUI(cfg.UI, analyzer, ctx)
	if err != nil {
		logger.Fatal("Ошибка инициализации пользовательского интерфейса", zap.Error(err))
	}

	// Запускаем сборщики данных в отдельных горутинах
	dataCollectors := []exchange.DataCollector{
		exchange.NewCandleCollector(client, store, cfg.Trading.Symbols, cfg.Trading.Interval),
		exchange.NewOrderBookCollector(client, store, cfg.Trading.Symbols, cfg.Analysis.OrderBook.Depth),
		exchange.NewFundingRateCollector(client, store, cfg.Trading.Symbols),
		exchange.NewOpenInterestCollector(client, store, cfg.Trading.Symbols),
	}

	for _, collector := range dataCollectors {
		collector := collector // Локальная копия для горутины
		go func() {
			defer collector.Stop()
			if err := collector.Start(ctx); err != nil {
				log.Printf("Предупреждение: ошибка запуска сборщика данных: %v", err)
			}
		}()
	}

	// Запускаем аналитический процесс в горутине
	go func() {
		// Отложенный старт для накопления данных
		time.Sleep(5 * time.Second)

		ticker := time.NewTicker(time.Duration(cfg.Analysis.IntervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				signals, err := analyzer.GenerateSignals(ctx)
				if err != nil {
					log.Printf("Предупреждение: ошибка при генерации сигналов: %v", err)
					continue
				}
				if len(signals) > 0 {
					userInterface.UpdateSignals(signals)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Запускаем UI в основном потоке (блокирующий вызов)
	// Это последняя инструкция в основном потоке
	userInterface.Start()
}
