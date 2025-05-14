// internal/analysis/volumedelta/analyzer.go
package volumedelta

import (
	"context"
	"fmt"
	"github.com/skalibog/bfma/pkg/logger"
	"go.uber.org/zap"
	"math"

	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/storage"
	"github.com/skalibog/bfma/pkg/models"
)

// Analyzer реализует анализатор дельты объемов
type Analyzer struct {
	config config.VolumeDeltaConfig
}

// NewAnalyzer создает новый анализатор дельты объемов
func NewAnalyzer(cfg config.VolumeDeltaConfig) *Analyzer {
	return &Analyzer{
		config: cfg,
	}
}

// Analyze анализирует дельту объемов и возвращает сигнал от -100 до 100
func (a *Analyzer) Analyze(ctx context.Context, storage storage.Storage, symbol string) (float64, error) {
	// Получаем исторические свечи для анализа
	candles, err := storage.GetCandles(ctx, symbol, "1m", a.config.Lookback*60) // Минутные свечи
	if err != nil {
		return 0, fmt.Errorf("ошибка получения свечей: %w", err)
	}

	logger.Debug("Анализ дельты объемов ",
		zap.String("symbol", symbol),
		zap.Int("candles_available", len(candles)),
		zap.Int("candles_required", a.config.Lookback*10)) // Снижено требование

	if len(candles) < a.config.Lookback*10 {
		return 0, fmt.Errorf("недостаточно данных для анализа дельты объемов: %d свечей (требуется %d)",
			len(candles), a.config.Lookback*10)
	}

	// Анализируем различные аспекты дельты объемов
	cumulativeDeltaSignal := a.analyzeCumulativeDelta(candles)
	impulseSignal := a.analyzeVolumeImpulses(candles)
	volumePriceSignal := a.analyzeVolumePriceRelation(candles)

	// Комбинируем сигналы с весами
	weightedSignal := (cumulativeDeltaSignal * 0.5) +
		(impulseSignal * 0.3) +
		(volumePriceSignal * 0.2)

	return weightedSignal, nil
}

// analyzeCumulativeDelta анализирует кумулятивную дельту объемов
func (a *Analyzer) analyzeCumulativeDelta(candles []*models.Candle) float64 {
	if len(candles) < 10 {
		return 0
	}

	// Рассчитываем накопленную дельту объемов для последних N свечей
	var cumulativeDelta float64
	var totalVolume float64

	for i := 0; i < a.config.Lookback && i < len(candles); i++ {
		candle := candles[i]

		// Оцениваем дельту объема на основе направления свечи
		// Если свеча бычья (close > open), считаем объем положительным
		// Если свеча медвежья (close < open), считаем объем отрицательным
		delta := candle.Volume
		if candle.Close < candle.Open {
			delta = -delta
		}

		// Взвешиваем более недавние свечи сильнее
		weight := 1.0 - (float64(i) / float64(a.config.Lookback))

		cumulativeDelta += delta * weight
		totalVolume += math.Abs(delta) * weight
	}

	// Нормализуем дельту относительно общего объема
	if totalVolume == 0 {
		return 0
	}

	normalizedDelta := cumulativeDelta / totalVolume

	// Преобразуем в сигнал от -100 до 100
	return normalizedDelta * 100
}

// analyzeVolumeImpulses анализирует импульсы объема
func (a *Analyzer) analyzeVolumeImpulses(candles []*models.Candle) float64 {
	if len(candles) < 30 {
		return 0
	}

	// Рассчитываем средний объем
	var totalVolume float64
	for i := 0; i < 30 && i < len(candles); i++ {
		totalVolume += candles[i].Volume
	}
	avgVolume := totalVolume / 30

	// Ищем объемные импульсы за последние N свечей
	var impulseSignal float64

	for i := 0; i < 10 && i < len(candles); i++ {
		candle := candles[i]

		// Проверяем на значительное превышение среднего объема
		volumeRatio := candle.Volume / avgVolume

		if volumeRatio >= a.config.SignificanceThreshold {
			// Обнаружен объемный импульс

			// Определяем направление импульса
			impulseStrength := (volumeRatio - 1.0) * 10 // Сила пропорциональна превышению среднего
			if impulseStrength > a.config.SignificanceThreshold*10 {
				impulseStrength = a.config.SignificanceThreshold * 10 // Ограничиваем максимальную силу
			}

			// Направление определяется направлением свечи
			if candle.Close > candle.Open {
				// Бычий импульс
				impulseSignal += impulseStrength
			} else {
				// Медвежий импульс
				impulseSignal -= impulseStrength
			}
		}
	}

	// Нормализуем сигнал
	return math.Max(math.Min(impulseSignal, 100), -100)
}

// analyzeVolumePriceRelation анализирует соотношение объема и цены
func (a *Analyzer) analyzeVolumePriceRelation(candles []*models.Candle) float64 {
	if len(candles) < 10 {
		return 0
	}

	// Ищем случаи, когда объем увеличивается, а цена падает (или наоборот)
	var signal float64

	for i := 1; i < a.config.Lookback && i < len(candles); i++ {
		current := candles[i-1]
		previous := candles[i]

		// Изменение объема
		volumeChange := (current.Volume - previous.Volume) / previous.Volume

		// Изменение цены
		priceChange := (current.Close - previous.Close) / previous.Close

		// Анализируем расхождения между изменениями объема и цены
		if math.Abs(volumeChange) > 0.1 { // Значительное изменение объема
			// Цена растет, объем падает = слабый рост
			if priceChange > 0 && volumeChange < -0.1 {
				signal -= 5
			} else if priceChange < 0 && volumeChange < -0.1 {
				// Цена падает, объем падает = близкий разворот вверх
				signal += 10
			} else if priceChange > 0 && volumeChange > 0.1 {
				// Цена растет, объем растет = сильный рост
				signal += 10
			} else if priceChange < 0 && volumeChange > 0.1 {
				// Цена падает, объем растет = сильное падение
				signal -= 20
			}
		}
	}

	// Нормализуем сигнал
	return math.Max(math.Min(signal, 100), -100)
}
