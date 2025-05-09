// internal/storage/influxdb.go
package storage

import (
	"context"
	"fmt"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/pkg/models"
)

// InfluxDBStorage реализует интерфейс Storage с использованием InfluxDB
type InfluxDBStorage struct {
	client   influxdb2.Client
	queryAPI api.QueryAPI
	writeAPI api.WriteAPI
	org      string
	bucket   string
}

// NewInfluxDBStorage создает новое хранилище InfluxDB
func NewInfluxDBStorage(cfg config.StorageConfig) (*InfluxDBStorage, error) {
	client := influxdb2.NewClient(cfg.URL, cfg.Token)

	// Проверка соединения
	health, err := client.Health(context.Background())
	if err != nil {
		return nil, fmt.Errorf("ошибка соединения с InfluxDB: %w", err)
	}
	if health == nil || health.Status != "pass" {
		return nil, fmt.Errorf("InfluxDB не в состоянии 'pass': %+v", health)
	}

	queryAPI := client.QueryAPI(cfg.Organization)
	writeAPI := client.WriteAPI(cfg.Organization, cfg.Bucket)

	return &InfluxDBStorage{
		client:   client,
		queryAPI: queryAPI,
		writeAPI: writeAPI,
		org:      cfg.Organization,
		bucket:   cfg.Bucket,
	}, nil
}

// Close закрывает соединение с базой данных
func (s *InfluxDBStorage) Close() {
	s.client.Close()
}

// SaveCandle сохраняет свечу в базу данных
func (s *InfluxDBStorage) SaveCandle(ctx context.Context, candle *models.Candle) error {
	// Создаем точку для записи в InfluxDB
	point := influxdb2.NewPoint(
		"candles",
		map[string]string{
			"symbol":   candle.Symbol,
			"interval": candle.Interval,
		},
		map[string]interface{}{
			"open":   candle.Open,
			"high":   candle.High,
			"low":    candle.Low,
			"close":  candle.Close,
			"volume": candle.Volume,
		},
		candle.OpenTime,
	)

	// Записываем точку
	s.writeAPI.WritePoint(point)
	s.writeAPI.Flush()

	return nil
}

// SaveCandles сохраняет множество свечей
func (s *InfluxDBStorage) SaveCandles(ctx context.Context, candles []*models.Candle) error {
	for _, candle := range candles {
		point := influxdb2.NewPoint(
			"candles",
			map[string]string{
				"symbol":   candle.Symbol,
				"interval": candle.Interval,
			},
			map[string]interface{}{
				"open":   candle.Open,
				"high":   candle.High,
				"low":    candle.Low,
				"close":  candle.Close,
				"volume": candle.Volume,
			},
			candle.OpenTime,
		)
		s.writeAPI.WritePoint(point)
	}

	s.writeAPI.Flush()
	return nil
}

// GetCandles получает исторические свечи
func (s *InfluxDBStorage) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]*models.Candle, error) {
	// Формируем Flux-запрос
	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -30d)
			|> filter(fn: (r) => r._measurement == "candles")
			|> filter(fn: (r) => r.symbol == "%s")
			|> filter(fn: (r) => r.interval == "%s")
			|> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
			|> sort(columns: ["_time"], desc: true)
			|> limit(n: %d)
	`, s.bucket, symbol, interval, limit)

	// Выполняем запрос
	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса свечей: %w", err)
	}

	// Обрабатываем результаты
	var candles []*models.Candle
	for result.Next() {
		record := result.Record()

		// Извлекаем поля
		timestamp := record.Time()
		open, _ := record.ValueByKey("open").(float64)
		high, _ := record.ValueByKey("high").(float64)
		low, _ := record.ValueByKey("low").(float64)
		close, _ := record.ValueByKey("close").(float64)
		volume, _ := record.ValueByKey("volume").(float64)

		// Создаем объект свечи
		candle := &models.Candle{
			Symbol:    symbol,
			Interval:  interval,
			OpenTime:  timestamp,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: timestamp.Add(getIntervalDuration(interval)),
		}

		candles = append(candles, candle)
	}

	// Проверяем на ошибки при обработке результатов
	if result.Err() != nil {
		return nil, fmt.Errorf("ошибка при обработке результатов: %w", result.Err())
	}

	return candles, nil
}

// GetLatestCandles получает последние свечи
func (s *InfluxDBStorage) GetLatestCandles(ctx context.Context, symbol, interval string, limit int) ([]*models.Candle, error) {
	return s.GetCandles(ctx, symbol, interval, limit)
}

// SaveOrderBook сохраняет стакан заявок
func (s *InfluxDBStorage) SaveOrderBook(ctx context.Context, orderBook *models.OrderBook) error {
	// Создаем одну точку для стакана
	point := influxdb2.NewPoint(
		"orderbooks",
		map[string]string{
			"symbol": orderBook.Symbol,
		},
		map[string]interface{}{
			"asks": convertOrderBookLevels(orderBook.Asks),
			"bids": convertOrderBookLevels(orderBook.Bids),
		},
		orderBook.Timestamp,
	)

	s.writeAPI.WritePoint(point)
	s.writeAPI.Flush()

	return nil
}

// GetLatestOrderBook получает последний стакан заявок
func (s *InfluxDBStorage) GetLatestOrderBook(ctx context.Context, symbol string) (*models.OrderBook, error) {
	// Формируем Flux-запрос
	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -1h)
			|> filter(fn: (r) => r._measurement == "orderbooks")
			|> filter(fn: (r) => r.symbol == "%s")
			|> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
			|> sort(columns: ["_time"], desc: true)
			|> limit(n: 1)
	`, s.bucket, symbol)

	// Выполняем запрос
	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса стакана: %w", err)
	}

	// Обрабатываем результат
	if result.Next() {
		record := result.Record()

		// Извлекаем поля
		timestamp := record.Time()
		asksStr, _ := record.ValueByKey("asks").(string)
		bidsStr, _ := record.ValueByKey("bids").(string)

		// Преобразуем строки в структуры
		asks := parseOrderBookLevels(asksStr)
		bids := parseOrderBookLevels(bidsStr)

		// Создаем объект стакана
		orderBook := &models.OrderBook{
			Symbol:    symbol,
			Timestamp: timestamp,
			Asks:      asks,
			Bids:      bids,
		}

		return orderBook, nil
	}

	// Проверяем на ошибки
	if result.Err() != nil {
		return nil, fmt.Errorf("ошибка при обработке результатов: %w", result.Err())
	}

	return nil, fmt.Errorf("стакан заявок для %s не найден", symbol)
}

// SaveFundingRate сохраняет ставку финансирования
func (s *InfluxDBStorage) SaveFundingRate(ctx context.Context, rate *models.FundingRate) error {
	// Создаем точку для записи
	point := influxdb2.NewPoint(
		"funding_rates",
		map[string]string{
			"symbol": rate.Symbol,
		},
		map[string]interface{}{
			"rate":            rate.Rate,
			"next_funding":    rate.NextFundingTime,
		},
		rate.Timestamp,
	)

	s.writeAPI.WritePoint(point)
	s.writeAPI.Flush()

	return nil
}

// GetFundingRates получает историю ставок финансирования
func (s *InfluxDBStorage) GetFundingRates(ctx context.Context, symbol string, limit int) ([]*models.FundingRate, error) {
	// Формируем Flux-запрос
	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -14d)
			|> filter(fn: (r) => r._measurement == "funding_rates")
			|> filter(fn: (r) => r.symbol == "%s")
			|> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
			|> sort(columns: ["_time"], desc: true)
			|> limit(n: %d)
	`, s.bucket, symbol, limit)

	// Выполняем запрос
	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса ставок финансирования: %w", err)
	}

	// Обрабатываем результаты
	var rates []*models.FundingRate
	for result.Next() {
		record := result.Record()

		// Извлекаем поля
		timestamp := record.Time()
		rate, _ := record.ValueByKey("rate").(string)
		nextFunding, _ := record.ValueByKey("next_funding").(time.Time)

		// Создаем объект ставки финансирования
		fundingRate := &models.FundingRate{
			Symbol:          symbol,
			Rate:            rate,
			Timestamp:       timestamp,
			NextFundingTime: nextFunding,
		}

		rates = append(rates, fundingRate)
	}

	// Проверяем на ошибки
	if result.Err() != nil {
		return nil, fmt.Errorf("ошибка при обработке результатов: %w", result.Err())
	}

	return rates, nil
}

// SaveOpenInterest сохраняет открытый интерес
func (s *InfluxDBStorage) SaveOpenInterest(ctx context.Context, oi *models.OpenInterest) error {
	// Создаем точку для записи
	point := influxdb2.NewPoint(
		"open_interest",
		map[string]string{
			"symbol": oi.Symbol,
		},
		map[string]interface{}{
			"value": oi.Value,
		},
		oi.Timestamp,
	)

	s.writeAPI.WritePoint(point)
	s.writeAPI.Flush()

	return nil
}

// GetOpenInterest получает историю открытого интереса
func (s *InfluxDBStorage) GetOpenInterest(ctx context.Context, symbol string, limit int) ([]*models.OpenInterest, error) {
	// Формируем Flux-запрос
	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -14d)
			|> filter(fn: (r) => r._measurement == "open_interest")
			|> filter(fn: (r) => r.symbol == "%s")
			|> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
			|> sort(columns: ["_time"], desc: true)
			|> limit(n: %d)
	`, s.bucket, symbol, limit)

	// Выполняем запрос
	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса открытого интереса: %w", err)
	}

	// Обрабатываем результаты
	var openInterest []*models.OpenInterest
	for result.Next() {
		record := result.Record()

		// Извлекаем поля
		timestamp := record.Time()
		value, _ := record.ValueByKey("value").(string)

		// Создаем объект открытого интереса
		oi := &models.OpenInterest{
			Symbol:    symbol,
			Value:     value,
			Timestamp: timestamp,
		}

		openInterest = append(openInterest, oi)
	}

	// Проверяем на ошибки
	if result.Err() != nil {
		return nil, fmt.Errorf("ошибка при обработке результатов: %w", result.Err())
	}

	return openInterest, nil
}

// SaveSignal сохраняет сигнал
func (s *InfluxDBStorage) SaveSignal(ctx context.Context, signal *models.SignalResult) error {
	// Создаем точку для записи
	point := influxdb2.NewPoint(
		"signals",
		map[string]string{
			"symbol": signal.Symbol,
		},
		map[string]interface{}{
			"recommendation": signal.Recommendation,
			"strength":       signal.SignalStrength,
			"position_size":  signal.PositionSize,
			"price":          signal.CurrentPrice,
			"components":     fmt.Sprintf("%v", signal.Components),
		},
		signal.Timestamp,
	)

	s.writeAPI.WritePoint(point)
	s.writeAPI.Flush()

	return nil
}

// GetSignalHistory получает историю сигналов
func (s *InfluxDBStorage) GetSignalHistory(ctx context.Context, symbol string, limit int) ([]*models.SignalResult, error) {
	// Формируем Flux-запрос
	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -30d)
			|> filter(fn: (r) => r._measurement == "signals")
			|> filter(fn: (r) => r.symbol == "%s")
			|> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
			|> sort(columns: ["_time"], desc: true)
			|> limit(n: %d)
	`, s.bucket, symbol, limit)

	// Выполняем запрос
	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса истории сигналов: %w", err)
	}

	// Обрабатываем результаты
	var signals []*models.SignalResult
	for result.Next() {
		record := result.Record()

		// Извлекаем поля
		timestamp := record.Time()
		recommendation, _ := record.ValueByKey("recommendation").(string)
		strength, _ := record.ValueByKey("strength").(float64)
		positionSize, _ := record.ValueByKey("position_size").(float64)
		price, _ := record.ValueByKey("price").(float64)

		// Создаем объект сигнала
		signal := &models.SignalResult{
			Symbol:         symbol,
			Timestamp:      timestamp,
			Recommendation: recommendation,
			SignalStrength: strength,
			PositionSize:   positionSize,
			CurrentPrice:   price,
			Components:     make(map[string]float64),
		}

		signals = append(signals, signal)
	}

	// Проверяем на ошибки
	if result.Err() != nil {
		return nil, fmt.Errorf("ошибка при обработке результатов: %w", result.Err())
	}

	return signals, nil
}

// GetSymbols возвращает список отслеживаемых символов
func (s *InfluxDBStorage) GetSymbols(ctx context.Context) ([]string, error) {
	// Формируем Flux-запрос для получения уникальных символов
	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -1d)
			|> filter(fn: (r) => r._measurement == "candles")
			|> keep(columns: ["symbol"])
			|> group(columns: ["symbol"])
			|> distinct(column: "symbol")
	`, s.bucket)

	// Выполняем запрос
	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса символов: %w", err)
	}

	// Обрабатываем результаты
	var symbols []string
	for result.Next() {
		record := result.Record()
		symbol, _ := record.ValueByKey("symbol").(string)
		symbols = append(symbols, symbol)
	}

	// Проверяем на ошибки
	if result.Err() != nil {
		return nil, fmt.Errorf("ошибка при обработке результатов: %w", result.Err())
	}

	return symbols, nil
}

// convertOrderBookLevels конвертирует уровни стакана в строку для хранения
func convertOrderBookLevels(levels []models.OrderBookLevel) string {
	result := "["
	for i, level := range levels {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("{\"price\":\"%s\",\"amount\":\"%s\"}", level.Price, level.Amount)
	}
	result += "]"
	return result
}

// parseOrderBookLevels парсит строку в уровни стакана
func parseOrderBookLevels(data string) []models.OrderBookLevel {
	// Это упрощенная реализация, в реальном коде нужен более надежный парсинг JSON
	var levels []models.OrderBookLevel
	// Парсинг пропущен для упрощения
	return levels
}

// getIntervalDuration конвертирует строковый интервал в duration
func getIntervalDuration(interval string) time.Duration {
	switch interval {
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 72 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

// Storage интерфейс для работы с хранилищем данных
type Storage interface {
	// Методы для свечей
	SaveCandle(ctx context.Context, candle *models.Candle) error
	SaveCandles(ctx context.Context, candles []*models.Candle) error
	GetCandles(ctx context.Context, symbol, interval string, limit int) ([]*models.Candle, error)
	GetLatestCandles(ctx context.Context, symbol, interval string, limit int) ([]*models.Candle, error)

	// Методы для стакана заявок
	SaveOrderBook(ctx context.Context, orderBook *models.OrderBook) error
	GetLatestOrderBook(ctx context.Context, symbol string) (*models.OrderBook, error)

	// Методы для ставок финансирования
	SaveFundingRate(ctx context.Context, rate *models.FundingRate) error
	GetFundingRates(ctx context.Context, symbol string, limit int) ([]*models.FundingRate, error)

	// Методы для открытого интереса
	SaveOpenInterest(ctx context.Context, oi *models.OpenInterest) error
	GetOpenInterest(ctx context.Context, symbol string, limit int) ([]*models.OpenInterest, error)

	// Методы для сигналов
	SaveSignal(ctx context.Context, signal *models.SignalResult) error
	GetSignalHistory(ctx context.Context, symbol string, limit int) ([]*models.SignalResult, error)

	// Вспомогательные методы
	GetSymbols(ctx context.Context) ([]string, error)
	Close()
}