// internal/analysis/funding/analyzer.go
package funding

import (
	"context"
	"fmt"
	"math"
	// "time"

	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/storage"
	"github.com/skalibog/bfma/pkg/models"
)

// Analyzer реализует анализатор ставок финансирования
type Analyzer struct {
	config config.FundingConfig
}

// NewAnalyzer создает новый анализатор ставок финансирования
func NewAnalyzer(cfg config.FundingConfig) *Analyzer {
	return &Analyzer{
		config: cfg,
	}
}

// Analyze анализирует ставки финансирования и возвращает сигнал от -100 до 100
func (a *Analyzer) Analyze(ctx context.Context, storage storage.Storage, symbol string) (float64, error) {
	// Получаем историю ставок финансирования
	fundingRates, err := storage.GetFundingRates(ctx, symbol, a.config.Periods)
	if err != nil {
		return 0, fmt.Errorf("ошибка получения ставок финансирования: %w", err)
	}

	if len(fundingRates) == 0 {
		return 0, fmt.Errorf("нет данных о ставках финансирования для %s", symbol)
	}

	// Анализируем различные аспекты ставок финансирования
	extremeSignal := a.analyzeExtremes(fundingRates)
	trendSignal := a.analyzeTrend(fundingRates)
	changeSignal := a.analyzeChange(fundingRates)

	// Комбинируем сигналы с весами
	weightedSignal := (extremeSignal * 0.4) +
		(trendSignal * 0.4) +
		(changeSignal * 0.2)

	return weightedSignal, nil
}

// analyzeExtremes анализирует экстремальные значения ставок финансирования
func (a *Analyzer) analyzeExtremes(rates []*models.FundingRate) float64 {
	if len(rates) == 0 {
		return 0
	}

	// Получаем текущую ставку финансирования
	currentRate, err := parseRate(rates[0].Rate)
	if err != nil {
		return 0
	}

	// Определяем экстремальное значение на основе исторических данных
	// Обычно ставка финансирования находится в пределах от -0.75% до +0.75%
	extremeThreshold := a.config.ExtremeThreshold

	// Сигнал на основе экстремальных значений
	// Высокая положительная ставка: держатели длинных позиций платят держателям коротких
	// Высокая отрицательная ставка: держатели коротких позиций платят держателям длинных
	var signal float64
	if currentRate > extremeThreshold {
		// Экстремально положительная ставка (медвежий сигнал)
		signal = -100 * math.Min(currentRate/0.01, 1.0)
	} else if currentRate < -extremeThreshold {
		// Экстремально отрицательная ставка (бычий сигнал)
		signal = 100 * math.Min(math.Abs(currentRate)/0.01, 1.0)
	} else {
		// В пределах нормы, слабый сигнал
		signal = -currentRate * 10000
	}

	return signal
}

// analyzeTrend анализирует тренд ставок финансирования
func (a *Analyzer) analyzeTrend(rates []*models.FundingRate) float64 {
	// Нужно минимум 3 значения для анализа тренда
	if len(rates) < 3 {
		return 0
	}

	// Парсим ставки
	var fundingValues []float64
	for _, rate := range rates {
		value, err := parseRate(rate.Rate)
		if err != nil {
			continue
		}
		fundingValues = append(fundingValues, value)
	}

	if len(fundingValues) < 3 {
		return 0
	}

	// Анализируем направление тренда
	// Для простоты используем линейную регрессию
	slope := calculateSlope(fundingValues)

	// Интерпретация тренда:
	// - Возрастающий тренд ставок (положительный наклон) = медвежий сигнал
	// - Убывающий тренд ставок (отрицательный наклон) = бычий сигнал
	var signal float64
	if slope > 0 {
		// Ставки растут - медвежий сигнал
		signal = -100 * math.Min(slope*1000, 1.0)
	} else {
		// Ставки падают - бычий сигнал
		signal = 100 * math.Min(math.Abs(slope)*1000, 1.0)
	}

	return signal
}

// analyzeChange анализирует изменение ставок финансирования
func (a *Analyzer) analyzeChange(rates []*models.FundingRate) float64 {
	if len(rates) < 2 {
		return 0
	}

	// Получаем текущую и предыдущую ставки
	currentRate, err1 := parseRate(rates[0].Rate)
	prevRate, err2 := parseRate(rates[1].Rate)

	if err1 != nil || err2 != nil {
		return 0
	}

	// Рассчитываем изменение
	change := currentRate - prevRate

	// Интерпретация изменения:
	// - Резкое увеличение ставки = медвежий сигнал (шортить)
	// - Резкое уменьшение ставки = бычий сигнал (лонгить)
	var signal float64
	if change > 0 {
		// Ставка увеличилась - медвежий сигнал
		signal = -100 * math.Min(change/0.001, 1.0)
	} else {
		// Ставка уменьшилась - бычий сигнал
		signal = 100 * math.Min(math.Abs(change)/0.001, 1.0)
	}

	return signal
}

// calculateSlope вычисляет наклон линейной регрессии
func calculateSlope(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	n := float64(len(values))
	sum_x := 0.0
	sum_y := 0.0
	sum_xy := 0.0
	sum_xx := 0.0

	for i, y := range values {
		x := float64(i)
		sum_x += x
		sum_y += y
		sum_xy += x * y
		sum_xx += x * x
	}

	// Формула наклона линейной регрессии
	slope := (n*sum_xy - sum_x*sum_y) / (n*sum_xx - sum_x*sum_x)
	if math.IsNaN(slope) {
		return 0
	}

	return slope
}

// parseRate парсит строковое представление ставки в число
func parseRate(rateStr string) (float64, error) {
	var rate float64
	_, err := fmt.Sscanf(rateStr, "%f", &rate)
	if err != nil {
		return 0, err
	}
	return rate, nil
}
