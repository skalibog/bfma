package exchange

import (
	"context"
	"fmt"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/pkg/models"
)

// BinanceClient клиент для взаимодействия с Binance
type BinanceClient struct {
	futures *futures.Client
	spot    *binance.Client
}

// NewBinanceClient создает новый клиент Binance
func NewBinanceClient(cfg config.BinanceConfig) (*BinanceClient, error) {
	futuresClient := futures.NewClient(cfg.APIKey, cfg.APISecret)
	spotClient := binance.NewClient(cfg.APIKey, cfg.APISecret)

	if cfg.Testnet {
		futuresClient.UseTestnet = true
		// Для спот-клиента нужно изменить базовый URL
		err := spotClient.SetBaseURL("https://testnet.binance.vision")
		if err != nil {
			return nil, fmt.Errorf("ошибка установки testnet URL: %w", err)
		}
	}

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

	candles := make([]*models.Candle, len(klines))
	for i, k := range klines {
		candle := &models.Candle{
			Symbol:    symbol,
			Interval:  interval,
			OpenTime:  time.Unix(k.OpenTime/1000, 0),
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
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

	timestamp, err := time.Parse("2006-01-02 15:04:05", rates[0].NextFundingTime)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга времени: %w", err)
	}

	rate := &models.FundingRate{
		Symbol:          symbol,
		Rate:            rates[0].LastFundingRate,
		Timestamp:       time.Now(),
		NextFundingTime: timestamp,
	}

	return rate, nil
}

// GetOpenInterest получает текущий открытый интерес
func (c *BinanceClient) GetOpenInterest(ctx context.Context, symbol string) (*models.OpenInterest, error) {
	oi, err := c.futures.NewOpenInterestService().
		Symbol(symbol).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения открытого интереса: %w", err)
	}

	openInterest := &models.OpenInterest{
		Symbol:    symbol,
		Value:     oi.OpenInterest,
		Timestamp: time.Now(),
	}

	return openInterest, nil
}
