package aggregator

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"sync"
	"time"

	"github.com/skalibog/bfma/internal/analysis/funding"
	"github.com/skalibog/bfma/internal/analysis/oianalysis"
	"github.com/skalibog/bfma/internal/analysis/orderbook"
	"github.com/skalibog/bfma/internal/analysis/technical"
	"github.com/skalibog/bfma/internal/analysis/volumedelta"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/exchange"
	"github.com/skalibog/bfma/internal/storage"
	"github.com/skalibog/bfma/pkg/logger"
	"github.com/skalibog/bfma/pkg/models"
)

// Analyzer объединяет все аналитические компоненты
type Analyzer struct {
	config          config.AnalysisConfig
	storage         storage.Storage
	client          *exchange.BinanceClient
	technicalAnal   *technical.Analyzer
	orderbookAnal   *orderbook.Analyzer
	fundingAnal     *funding.Analyzer
	oiAnal          *oianalysis.Analyzer
	volumeDeltaAnal *volumedelta.Analyzer
	symbols         []string
}

// NewAnalyzer создает новый анализатор
func NewAnalyzer(cfg config.AnalysisConfig, storage storage.Storage, client *exchange.BinanceClient, symbols []string) *Analyzer {
	return &Analyzer{
		config:          cfg,
		storage:         storage,
		client:          client,
		technicalAnal:   technical.NewAnalyzer(cfg.Technical),
		orderbookAnal:   orderbook.NewAnalyzer(cfg.OrderBook),
		fundingAnal:     funding.NewAnalyzer(cfg.Funding),
		oiAnal:          oianalysis.NewAnalyzer(cfg.OpenInterest),
		volumeDeltaAnal: volumedelta.NewAnalyzer(cfg.VolumeDelta),
		symbols:         symbols, // Инициализируем из параметра
	}
}

// GenerateSignals генерирует сигналы для всех отслеживаемых символов
func (a *Analyzer) GenerateSignals(ctx context.Context) (map[string]*models.SignalResult, error) {
	// Используем наш внутренний список символов
	symbols := a.symbols

	results := make(map[string]*models.SignalResult)
	var wg sync.WaitGroup
	var mutex sync.Mutex

	for _, symbol := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()

			signal, err := a.generateSignalForSymbol(ctx, sym)
			if err != nil {
				// Логируем ошибку, но продолжаем для других символов
				fmt.Printf("Ошибка генерации сигнала для %s: %v\n", sym, err)
				return
			}

			mutex.Lock()
			results[sym] = signal
			mutex.Unlock()
		}(symbol)
	}

	wg.Wait()
	return results, nil
}

// generateSignalForSymbol генерирует сигнал для одного символа
func (a *Analyzer) generateSignalForSymbol(ctx context.Context, symbol string) (*models.SignalResult, error) {
	// Получаем данные для анализа
	interval := "1m" // Получаем из конфигурации или устанавливаем по умолчанию

	// Запускаем все анализаторы параллельно
	var wg sync.WaitGroup
	var technicalSignal, orderbookSignal, fundingSignal, oiSignal, volumeDeltaSignal float64
	var technicalErr, orderbookErr, fundingErr, oiErr, volumeDeltaErr error

	wg.Add(5)

	// Технический анализ
	go func() {
		defer wg.Done()
		technicalSignal, technicalErr = a.technicalAnal.Analyze(ctx, a.storage, symbol, interval)
		logger.Debug("AGGREGATOR: Технический анализ завершен", zap.String("symbol", symbol), zap.Float64("signal", technicalSignal))

	}()

	// Анализ стакана
	go func() {
		defer wg.Done()
		orderbookSignal, orderbookErr = a.orderbookAnal.Analyze(ctx, a.storage, symbol)
		logger.Debug("AGGREGATOR: Анализ стакана завершен", zap.String("symbol", symbol), zap.Float64("signal", orderbookSignal))
	}()

	// Анализ ставок финансирования
	go func() {
		defer wg.Done()
		fundingSignal, fundingErr = a.fundingAnal.Analyze(ctx, a.storage, symbol)
		logger.Debug("AGGREGATOR: Анализ ставок финансирования завершен", zap.String("symbol", symbol), zap.Float64("signal", fundingSignal))
	}()

	// Анализ открытого интереса
	go func() {
		defer wg.Done()
		oiSignal, oiErr = a.oiAnal.Analyze(ctx, a.storage, symbol)
		logger.Debug("AGGREGATOR: Анализ открытого интереса завершен", zap.String("symbol", symbol), zap.Float64("signal", oiSignal))
	}()

	// Анализ дельты объемов
	go func() {
		defer wg.Done()
		volumeDeltaSignal, volumeDeltaErr = a.volumeDeltaAnal.Analyze(ctx, a.storage, symbol)
		logger.Debug("AGGREGATOR: Анализ дельты объемов завершен", zap.String("symbol", symbol), zap.Float64("signal", volumeDeltaSignal))
	}()

	wg.Wait()

	if technicalErr != nil {
		logger.Warn("Предупреждение: технический анализ недоступен",
			zap.String("symbol", symbol),
			zap.Error(technicalErr),
			zap.Int("требуется_свечей", a.config.Technical.MACDSlow+a.config.Technical.MACDSignal))
		technicalSignal = 0
	}
	if orderbookErr != nil {
		logger.Warn("Предупреждение: анализ стакана недоступен", zap.String("symbol", symbol), zap.Error(orderbookErr))
		orderbookSignal = 0
	}
	if fundingErr != nil {
		logger.Warn("Предупреждение: анализ финансирования недоступен", zap.String("symbol", symbol), zap.Error(fundingErr))
		fundingSignal = 0
	}
	if oiErr != nil {
		logger.Warn("Предупреждение: анализ открытого интереса недоступен", zap.String("symbol", symbol), zap.Error(oiErr))
		oiSignal = 0
	}
	if volumeDeltaErr != nil {
		logger.Warn("Предупреждение: анализ дельты объемов недоступен", zap.String("symbol", symbol), zap.Error(volumeDeltaErr))
		volumeDeltaSignal = 0
	}

	// Взвешиваем сигналы
	weightedSignal := (technicalSignal * a.config.Technical.Weight) +
		(orderbookSignal * a.config.OrderBook.Weight) +
		(fundingSignal * a.config.Funding.Weight) +
		(oiSignal * a.config.OpenInterest.Weight) +
		(volumeDeltaSignal * a.config.VolumeDelta.Weight)

	// Определяем рекомендацию
	var recommendation string
	var positionSize float64

	if weightedSignal >= a.config.SignalThresholds.StrongBuy {
		recommendation = "СИЛЬНАЯ ПОКУПКА"
		positionSize = 1.0
	} else if weightedSignal >= a.config.SignalThresholds.Buy {
		recommendation = "ПОКУПКА"
		positionSize = 0.7
	} else if weightedSignal <= a.config.SignalThresholds.StrongSell {
		recommendation = "СИЛЬНАЯ ПРОДАЖА"
		positionSize = 1.0
	} else if weightedSignal <= a.config.SignalThresholds.Sell {
		recommendation = "ПРОДАЖА"
		positionSize = 0.7
	} else {
		recommendation = "НЕЙТРАЛЬНО"
		positionSize = 0.0
	}

	// Получаем текущие рыночные данные
	currentPrice := 0.0
	candles, err := a.storage.GetLatestCandles(ctx, symbol, interval, 1)
	if err == nil && len(candles) > 0 {
		currentPrice = candles[0].Close
	}

	// Формируем результат
	result := &models.SignalResult{
		Symbol:         symbol,
		Timestamp:      time.Now(),
		Recommendation: recommendation,
		SignalStrength: weightedSignal,
		PositionSize:   positionSize,
		CurrentPrice:   currentPrice,
		Components: map[string]float64{
			"technical":    technicalSignal,
			"orderbook":    orderbookSignal,
			"funding":      fundingSignal,
			"openInterest": oiSignal,
			"volumeDelta":  volumeDeltaSignal,
		},
	}

	// Сохраняем сигнал в хранилище
	if err := a.storage.SaveSignal(ctx, result); err != nil {
		fmt.Printf("Предупреждение: не удалось сохранить сигнал: %v\n", err)
	}

	return result, nil
}

// GetSignalHistory возвращает историю сигналов для символа
func (a *Analyzer) GetSignalHistory(ctx context.Context, symbol string, limit int) ([]*models.SignalResult, error) {
	return a.storage.GetSignalHistory(ctx, symbol, limit)
}
