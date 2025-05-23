package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/storage"
	"github.com/skalibog/bfma/pkg/logger"
	"github.com/skalibog/bfma/pkg/models"
)

// BinanceClient клиент для взаимодействия с Binance
type BinanceClient struct {
	futures *futures.Client
	spot    *binance.Client
}

// NewBinanceClient создает новый клиент Binance
func NewBinanceClient(cfg config.BinanceConfig) (*BinanceClient, error) {
	// Устанавливаем режим testnet перед созданием клиентов
	if cfg.Testnet {
		binance.UseTestnet = true
		futures.UseTestnet = true
	}

	// После установки режима создаем клиенты
	futuresClient := futures.NewClient(cfg.APIKey, cfg.APISecret)
	spotClient := binance.NewClient(cfg.APIKey, cfg.APISecret)

	// Отладочный вывод
	logger.Info("Создание клиента Binance успешно")

	return &BinanceClient{
		futures: futuresClient,
		spot:    spotClient,
	}, nil
}

// GetKlines получает исторические свечи
func (c *BinanceClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]*models.Candle, error) {
	klines, err := c.futures.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения свечей: %w", err)
	}

	logger.Info("Klines", zap.String("symbol", symbol), zap.String("interval", interval), zap.Int("limit", limit), zap.Int("count", len(klines)))
	candles := make([]*models.Candle, len(klines))
	for i, k := range klines {
		// Преобразуем строковые значения в float64
		open, _ := strconv.ParseFloat(k.Open, 64)
		high, _ := strconv.ParseFloat(k.High, 64)
		low, _ := strconv.ParseFloat(k.Low, 64)
		close, _ := strconv.ParseFloat(k.Close, 64)
		volume, _ := strconv.ParseFloat(k.Volume, 64)

		candle := &models.Candle{
			Symbol:    symbol,
			Interval:  interval,
			OpenTime:  time.Unix(k.OpenTime/1000, 0),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: time.Unix(k.CloseTime/1000, 0),
		}
		candles[i] = candle
	}

	return candles, nil
}

// GetOrderBook получает стакан заявок
func (c *BinanceClient) GetOrderBook(ctx context.Context, symbol string, limit int) (*models.OrderBook, error) {
	ob, err := c.futures.NewDepthService().
		Symbol(symbol).
		Limit(limit).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения стакана: %w", err)
	}

	orderBook := &models.OrderBook{
		Symbol:    symbol,
		Timestamp: time.Now(),
		Bids:      make([]models.OrderBookLevel, len(ob.Bids)),
		Asks:      make([]models.OrderBookLevel, len(ob.Asks)),
	}

	for i, bid := range ob.Bids {
		orderBook.Bids[i] = models.OrderBookLevel{
			Price:  bid.Price,
			Amount: bid.Quantity,
		}
	}

	for i, ask := range ob.Asks {
		orderBook.Asks[i] = models.OrderBookLevel{
			Price:  ask.Price,
			Amount: ask.Quantity,
		}
	}

	return orderBook, nil
}

// GetFundingRate получает текущую ставку финансирования
func (c *BinanceClient) GetFundingRate(ctx context.Context, symbol string) (*models.FundingRate, error) {
	rates, err := c.futures.NewPremiumIndexService().
		Symbol(symbol).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения ставки финансирования: %w", err)
	}

	if len(rates) == 0 {
		return nil, fmt.Errorf("не найдены данные о ставке финансирования для %s", symbol)
	}

	// NextFundingTime - это timestamp в миллисекундах, преобразуем в time.Time
	nextFundingTime := time.Unix(rates[0].NextFundingTime/1000, 0)

	rate := &models.FundingRate{
		Symbol:          symbol,
		Rate:            rates[0].LastFundingRate,
		Timestamp:       time.Now(),
		NextFundingTime: nextFundingTime,
	}

	return rate, nil
}

// OpenInterestResp структура для парсинга ответа API
type OpenInterestResp struct {
	Symbol       string `json:"symbol"`
	OpenInterest string `json:"openInterest"`
	Time         int64  `json:"time"`
}

// GetOpenInterest получает текущий открытый интерес напрямую через REST API
func (c *BinanceClient) GetOpenInterest(ctx context.Context, symbol string) (*models.OpenInterest, error) {
	baseURL := "https://fapi.binance.com"
	if futures.UseTestnet {
		baseURL = "https://testnet.binancefuture.com"
	}

	url := fmt.Sprintf("%s/fapi/v1/openInterest?symbol=%s", baseURL, symbol)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	var oiResp OpenInterestResp
	if err := json.Unmarshal(body, &oiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %w", err)
	}

	return &models.OpenInterest{
		Symbol:    symbol,
		Value:     oiResp.OpenInterest,
		Timestamp: time.Unix(oiResp.Time/1000, 0),
	}, nil
}

// DataCollector интерфейс для сборщиков данных
type DataCollector interface {
	Start(ctx context.Context) error
	Stop()
}

// CandleCollector сборщик данных о свечах
type CandleCollector struct {
	client   *BinanceClient
	storage  storage.Storage
	symbols  []string
	interval string
	doneC    chan struct{}
	stopC    chan struct{}
}

// NewCandleCollector создает новый сборщик свечей
func NewCandleCollector(client *BinanceClient, storage storage.Storage, symbols []string, interval string) *CandleCollector {
	return &CandleCollector{
		client:   client,
		storage:  storage,
		symbols:  symbols,
		interval: interval,
	}
}

// Start запускает сборщик данных
func (c *CandleCollector) Start(ctx context.Context) error {
	logger.Info("Запуск сборщика свечей",
		zap.Strings("symbols", c.symbols),
		zap.String("interval", c.interval))

	// Загружаем исторические данные
	for _, symbol := range c.symbols {
		logger.Info("Загрузка исторических свечей",
			zap.String("symbol", symbol),
			zap.String("interval", c.interval),
			zap.Int("limit", 1000)) // Увеличил лимит до 1000

		candles, err := c.client.GetKlines(ctx, symbol, c.interval, 500) // Увеличил до 1000
		if err != nil {
			logger.Error("Ошибка загрузки исторических свечей",
				zap.String("symbol", symbol),
				zap.Error(err))
			return fmt.Errorf("ошибка загрузки исторических свечей для %s: %w", symbol, err)
		}

		logger.Info("Получены исторические свечи",
			zap.String("symbol", symbol),
			zap.Int("count", len(candles)))

		if err := c.storage.SaveCandles(ctx, candles); err != nil {
			logger.Error("Ошибка сохранения исторических свечей",
				zap.String("symbol", symbol),
				zap.Error(err))
			return fmt.Errorf("ошибка сохранения исторических свечей для %s: %w", symbol, err)
		}

		logger.Info("Исторические свечи сохранены",
			zap.String("symbol", symbol),
			zap.Int("count", len(candles)))
	}

	// Подписываемся на обновления свечей через WebSocket
	for _, symbol := range c.symbols {
		wsKlineHandler := func(event *futures.WsKlineEvent) {
			logger.Debug("Получено WS событие свечи",
				zap.String("symbol", symbol),
				zap.Time("time", time.Now()),
				zap.String("interval", c.interval),
				zap.Bool("is_final", event.Kline.IsFinal))
			k := event.Kline

			// Преобразуем строковые значения в float64
			open, _ := strconv.ParseFloat(k.Open, 64)
			high, _ := strconv.ParseFloat(k.High, 64)
			low, _ := strconv.ParseFloat(k.Low, 64)
			closes, _ := strconv.ParseFloat(k.Close, 64)
			volume, _ := strconv.ParseFloat(k.Volume, 64)

			candle := &models.Candle{
				Symbol:    symbol,
				Interval:  c.interval,
				OpenTime:  time.Unix(k.StartTime/1000, 0),
				Open:      open,
				High:      high,
				Low:       low,
				Close:     closes,
				Volume:    volume,
				CloseTime: time.Unix(k.EndTime/1000, 0),
			}

			c.storage.SaveCandle(ctx, candle)
		}

		errHandler := func(err error) {
			logger.Error("Ошибка WebSocket для свечей", zap.String("symbol", symbol), zap.Error(err))
		}

		var err error
		c.doneC, c.stopC, err = futures.WsKlineServe(symbol, c.interval, wsKlineHandler, errHandler)
		if err != nil {
			logger.Error("Ошибка подписки на WebSocket для свечей", zap.String("symbol", symbol), zap.Error(err))
			return fmt.Errorf("ошибка подписки на WebSocket для свечей %s: %w", symbol, err)
		}
	}

	return nil
}

// Stop останавливает сборщик данных
func (c *CandleCollector) Stop() {
	if c.stopC != nil {
		close(c.stopC)
	}
}

// OrderBookCollector сборщик данных о стакане заявок
type OrderBookCollector struct {
	client       *BinanceClient
	storage      storage.Storage
	symbols      []string
	depth        int
	doneChannels []chan struct{} // Было: doneC chan struct{}
	stopChannels []chan struct{} // Было: stopC chan struct{}
}

// NewOrderBookCollector создает новый сборщик стакана заявок
func NewOrderBookCollector(client *BinanceClient, storage storage.Storage, symbols []string, depth int) *OrderBookCollector {
	return &OrderBookCollector{
		client:  client,
		storage: storage,
		symbols: symbols,
		depth:   depth,
	}
}

// Start запускает сборщик данных
func (c *OrderBookCollector) Start(ctx context.Context) error {
	// Загружаем начальный стакан через REST API
	for _, symbol := range c.symbols {
		orderBook, err := c.client.GetOrderBook(ctx, symbol, c.depth)
		if err != nil {
			logger.Error("Ошибка загрузки стакана", zap.Error(err))
			continue // Продолжаем с другими символами вместо полной остановки
		}
		c.storage.SaveOrderBook(ctx, orderBook)
	}

	// Используем один обработчик для всех символов
	handler := func(event *futures.WsDepthEvent) {
		symbol := event.Symbol // Получаем символ из события

		logger.Debug("Получено WS событие стакана",
			zap.String("symbol", symbol),
			zap.Time("time", time.Now()),
			zap.Int("depth", c.depth))

		// Создаем объект стакана и сохраняем
		orderBook := &models.OrderBook{
			Symbol:    symbol,
			Timestamp: time.Now(),
			Bids:      make([]models.OrderBookLevel, len(event.Bids)),
			Asks:      make([]models.OrderBookLevel, len(event.Asks)),
		}

		// Заполняем данными
		for i, bid := range event.Bids {
			orderBook.Bids[i] = models.OrderBookLevel{
				Price:  bid.Price,
				Amount: bid.Quantity,
			}
		}
		for i, ask := range event.Asks {
			orderBook.Asks[i] = models.OrderBookLevel{
				Price:  ask.Price,
				Amount: ask.Quantity,
			}
		}

		// Сохраняем в базу
		if err := c.storage.SaveOrderBook(ctx, orderBook); err != nil {
			logger.Error("Ошибка сохранения стакана",
				zap.String("symbol", symbol), zap.Error(err))
		}
	}

	errHandler := func(err error) {
		logger.Error("Ошибка WebSocket", zap.Error(err))
		// Просто логируем ошибку и продолжаем работу
	}
	symbolsMap := make(map[string]string)
	for _, sym := range c.symbols {
		// Для Binance API нужен формат "symbol@depth"
		symbolsMap[sym] = sym + "@depth"
	}

	logger.Info("Подписка на WebSocket для стакана", zap.Any("symbols", symbolsMap))
	_, _, err := futures.WsCombinedDepthServe(symbolsMap, handler, errHandler)

	return err
}

// Stop останавливает сборщик данных
func (c *OrderBookCollector) Stop() {
	for _, stopC := range c.stopChannels {
		if stopC != nil {
			close(stopC)
		}
	}
}

// FundingRateCollector сборщик данных о ставках финансирования
type FundingRateCollector struct {
	client  *BinanceClient
	storage storage.Storage
	symbols []string
	ticker  *time.Ticker
	done    chan struct{}
}

// NewFundingRateCollector создает новый сборщик ставок финансирования
func NewFundingRateCollector(client *BinanceClient, storage storage.Storage, symbols []string) *FundingRateCollector {
	return &FundingRateCollector{
		client:  client,
		storage: storage,
		symbols: symbols,
		done:    make(chan struct{}),
	}
}

// Start запускает сборщик данных
func (c *FundingRateCollector) Start(ctx context.Context) error {
	// Загружаем текущие ставки финансирования
	for _, symbol := range c.symbols {
		rate, err := c.client.GetFundingRate(ctx, symbol)
		if err != nil {
			return fmt.Errorf("ошибка загрузки ставки финансирования для %s: %w", symbol, err)
		}

		if err := c.storage.SaveFundingRate(ctx, rate); err != nil {
			return fmt.Errorf("ошибка сохранения ставки финансирования для %s: %w", symbol, err)
		}
	}

	// Запускаем периодическое обновление ставок финансирования
	c.ticker = time.NewTicker(10 * time.Minute) // Обновляем каждый час

	go func() {
		for {
			select {
			case <-c.ticker.C:
				for _, symbol := range c.symbols {
					rate, err := c.client.GetFundingRate(ctx, symbol)
					if err != nil {
						logger.Error("Ошибка получения ставки финансирования",
							zap.String("symbol", symbol),
							zap.Error(err))
						continue
					}

					if err := c.storage.SaveFundingRate(ctx, rate); err != nil {
						logger.Error("Ошибка сохранения ставки финансирования",
							zap.String("symbol", symbol),
							zap.Error(err))
					}
				}
			case <-c.done:
				return
			}
		}
	}()

	return nil
}

// Stop останавливает сборщик данных
func (c *FundingRateCollector) Stop() {
	if c.ticker != nil {
		c.ticker.Stop()
		close(c.done)
	}
}

// OpenInterestCollector сборщик данных о открытом интересе
type OpenInterestCollector struct {
	client  *BinanceClient
	storage storage.Storage
	symbols []string
	ticker  *time.Ticker
	done    chan struct{}
}

// NewOpenInterestCollector создает новый сборщик открытого интереса
func NewOpenInterestCollector(client *BinanceClient, storage storage.Storage, symbols []string) *OpenInterestCollector {
	return &OpenInterestCollector{
		client:  client,
		storage: storage,
		symbols: symbols,
		done:    make(chan struct{}),
	}
}

// Start запускает сборщик данных
func (c *OpenInterestCollector) Start(ctx context.Context) error {
	// Загружаем текущий открытый интерес
	for _, symbol := range c.symbols {
		oi, err := c.client.GetOpenInterest(ctx, symbol)
		if err != nil {
			return fmt.Errorf("ошибка загрузки открытого интереса для %s: %w", symbol, err)
		}

		if err := c.storage.SaveOpenInterest(ctx, oi); err != nil {
			return fmt.Errorf("ошибка сохранения открытого интереса для %s: %w", symbol, err)
		}
	}

	// Запускаем периодическое обновление открытого интереса
	c.ticker = time.NewTicker(15 * time.Minute) // Обновляем каждые 15 минут

	go func() {
		for {
			select {
			case <-c.ticker.C:
				for _, symbol := range c.symbols {
					oi, err := c.client.GetOpenInterest(context.Background(), symbol)
					if err != nil {
						fmt.Printf("Ошибка получения открытого интереса для %s: %v\n", symbol, err)
						continue
					}

					if err := c.storage.SaveOpenInterest(context.Background(), oi); err != nil {
						fmt.Printf("Ошибка сохранения открытого интереса для %s: %v\n", symbol, err)
					}
				}
			case <-c.done:
				return
			}
		}
	}()

	return nil
}

// Stop останавливает сборщик данных
func (c *OpenInterestCollector) Stop() {
	if c.ticker != nil {
		c.ticker.Stop()
		close(c.done)
	}
}
