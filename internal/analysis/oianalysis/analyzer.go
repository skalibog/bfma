// internal/analysis/oianalysis/analyzer.go
package oianalysis

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/storage"
	"github.com/skalibog/bfma/pkg/models"
)

// Analyzer реализует анализатор открытого интереса
type Analyzer struct {
	config config.OpenInterestConfig
}

// NewAnalyzer создает новый анализатор открытого интереса
func NewAnalyzer(cfg config.OpenInterestConfig) *Analyzer {
	return &Analyzer{
		config: cfg,
	}
}

// Analyze анализирует открытый интерес и возвращает сигнал от -100 до 100
func (a *Analyzer) Analyze(ctx context.Context, storage storage.Storage, symbol string) (float64, error) {
	// Получаем историю открытого интереса
	openInterest, err := storage.GetOpenInterest(ctx, symbol, a.config.Lookback)
	if err != nil {
		return 0, fmt.Errorf("ошибка получения данных открытого интереса: %w", err)
	}

	if len(openInterest) == 0 {
		return 0, fmt.Errorf("нет данных об открытом интересе для %s", symbol)
	}

	// Получаем исторические свечи для анализа дивергенции
	candles, err := storage.GetCandles(ctx, symbol, "1h", a.config.Lookback)
	if err != nil {
		return 0, fmt.Errorf("ошибка получения исторических свечей: %w", err)
	}

	if len(candles) < 2 {
		return 0, fmt.Errorf("недостаточно данных для анализа")
	}

	// Анализируем различные аспекты открытого интереса
	changeSignal := a.analyzeOIChange(openInterest)
	divergenceSignal := a.analyzeOIvsPriceDivergence(openInterest, candles)
	trendSignal := a.analyzeOITrend(openInterest)

	// Комбинируем сигналы с весами
	weightedSignal := (changeSignal * 0.4) +
		(divergenceSignal * 0.4) +
		(trendSignal * 0.2)

	return weightedSignal, nil
}

// analyzeOIChange анализирует изменение открытого интереса
func (a *Analyzer) analyzeOIChange(data []*models.OpenInterest) float64 {
	if len(data) < 2 {
		return 0
	}

	// Получаем текущий и предыдущий открытый интерес
	currentOI, err1 := parseOI(data[0].Value)
	prevOI, err2 := parseOI(data[1].Value)

	if err1 != nil || err2 != nil {
		return 0
	}

	// Рассчитываем процентное изменение
	if prevOI == 0 {
		return 0
	}
	percentChange := (currentOI - prevOI) / prevOI * 100

	// Интерпретация изменений:
	// - Резкое увеличение OI вместе с ростом цены = бычий сигнал
	// - Резкое увеличение OI вместе с падением цены = медвежий сигнал
	// - Резкое уменьшение OI = конец текущего тренда
	var signal float64
	changeThreshold := a.config.ChangeThreshold

	if math.Abs(percentChange) < changeThreshold {
		// Незначительное изменение - нейтральный сигнал
		signal = 0
	} else if percentChange > 0 {
		// Положительное изменение OI
		// Сила сигнала пропорциональна проценту изменения
		// Направление сигнала нейтральное, так как нужно смотреть на цену
		signal = math.Min(percentChange/changeThreshold, 1.0) * 50
	} else {
		// Отрицательное изменение OI обычно говорит о завершении тренда
		// Более сильное снижение - более сильный сигнал разворота
		signal = math.Min(math.Abs(percentChange)/changeThreshold, 1.0) * -20
	}

	return signal
}

// analyzeOIvsPriceDivergence анализирует дивергенцию между OI и ценой
func (a *Analyzer) analyzeOIvsPriceDivergence(openInterest []*models.OpenInterest, candles []*models.Candle) float64 {
	if len(openInterest) < 5 || len(candles) < 5 {
		return 0
	}

	// Подготавливаем данные для анализа
	oiValues := make([]float64, 0, len(openInterest))
	priceValues := make([]float64, 0, len(candles))

	// Обратите внимание, что данные OI и свечи могут иметь разные временные метки
	// Здесь мы упрощаем и просто берем последние значения
	for i := 0; i < len(openInterest) && i < len(candles) && i < 5; i++ {
		oi, err := parseOI(openInterest[i].Value)
		if err != nil {
			continue
		}
		oiValues = append(oiValues, oi)
		priceValues = append(priceValues, candles[i].Close)
	}

	if len(oiValues) < 3 || len(priceValues) < 3 {
		return 0
	}

	// Рассчитываем наклоны трендов цены и OI
	priceSlope := calculateSlope(priceValues)
	oiSlope := calculateSlope(oiValues)

	// Анализируем дивергенцию
	var signal float64

	// Проверяем на дивергенцию
	// Если направления трендов разные, это дивергенция
	if priceSlope * oiSlope < 0 {
		// Дивергенция обнаружена

		// Цена растет, OI падает = потенциальное ослабление роста
		if priceSlope > 0 && oiSlope < 0 {
			// Сила сигнала основана на степени дивергенции
			signal = -70 * math.Min(math.Abs(priceSlope * oiSlope * 1000), 1.0)
		} else if priceSlope < 0 && oiSlope > 0 {
			// Цена падает, OI растет = потенциальное замедление падения
			signal = 70 * math.Min(math.Abs(priceSlope * oiSlope * 1000), 1.0)
		}
	} else {
		// Нет дивергенции, тренды совпадают

		// Если и цена, и OI растут = подтверждение роста
		if priceSlope > 0 && oiSlope > 0 {
			signal = 40 * math.Min(priceSlope * oiSlope * 1000, 1.0)
		} else if priceSlope < 0 && oiSlope < 0 {
			// Если и цена, и OI падают = подтверждение падения
			signal = -40 * math.Min(math.Abs(priceSlope * oiSlope * 1000), 1.0)
		}
	}

	return signal
}

// analyzeOITrend анализирует тренд открытого интереса
func (a *Analyzer) analyzeOITrend(data []*models.OpenInterest) float64 {
	if len(data) < 3 {
		return 0
	}

	// Подготавливаем данные для анализа тренда
	oiValues := make([]float64, 0, len(data))
	for _, oi := range data {
		value, err := parseOI(oi.Value)
		if err != nil {
			continue
		}
		oiValues = append(oiValues, value)
	}

	if len(oiValues) < 3 {
		return 0
	}

	// Рассчитываем наклон тренда
	slope := calculateSlope(oiValues)

	// Интерпретация тренда OI
	var signal float64
	if slope > 0 {
		// Положительный тренд OI обычно бычий
		signal = 30 * math.Min(slope*1000, 1.0)
	} else {
		// Отрицательный тренд OI обычно медвежий
		signal = -30 * math.Min(math.Abs(slope)*1000, 1.0)
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

// parseOI парсит строковое представление открытого интереса в число
func parseOI(oiStr string) (float64, error) {
	return strconv.ParseFloat(oiStr, 64)
}