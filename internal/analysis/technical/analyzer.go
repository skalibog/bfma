package technical

import (
	"context"
	"fmt"
	"math"

	"github.com/markcheno/go-talib"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/storage"
)

// Analyzer реализует анализатор технических индикаторов
type Analyzer struct {
	config config.TechnicalConfig
}

// NewAnalyzer создает новый анализатор технических индикаторов
func NewAnalyzer(cfg config.TechnicalConfig) *Analyzer {
	return &Analyzer{
		config: cfg,
	}
}

// Analyze выполняет технический анализ для символа
func (a *Analyzer) Analyze(ctx context.Context, storage storage.Storage, symbol, interval string) (float64, error) {
	// Получаем исторические свечи
	candles, err := storage.GetCandles(ctx, symbol, interval, 100)
	if err != nil {
		return 0, fmt.Errorf("ошибка получения свечей: %w", err)
	}

	if len(candles) < a.config.MACDSlow+a.config.MACDSignal {
		return 0, fmt.Errorf("недостаточно данных для анализа: %d свечей", len(candles))
	}

	// Подготавливаем данные для анализа
	closes := make([]float64, len(candles))
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	volumes := make([]float64, len(candles))

	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		volumes[i] = c.Volume
	}

	// Рассчитываем индикаторы
	rsiSignal := a.calculateRSI(closes)
	macdSignal := a.calculateMACD(closes)
	bbSignal := a.calculateBollingerBands(closes)
	ichimokuSignal := a.calculateIchimoku(highs, lows, closes)
	atrSignal := a.calculateATR(highs, lows, closes)

	// Комбинируем сигналы с весами
	weightedSignal := (rsiSignal * 0.25) +
		(macdSignal * 0.25) +
		(bbSignal * 0.2) +
		(ichimokuSignal * 0.2) +
		(atrSignal * 0.1)

	return weightedSignal, nil
}

// calculateRSI рассчитывает RSI и возвращает сигнал от -100 до 100
func (a *Analyzer) calculateRSI(closes []float64) float64 {
	rsi := talib.Rsi(closes, a.config.RSIPeriod)
	// Получаем последнее значение RSI
	lastRSI := rsi[len(rsi)-1]

	// Нормализуем RSI к диапазону -100..100
	// RSI находится в диапазоне 0-100:
	// < 30: перепроданность (сигнал на покупку)
	// > 70: перекупленность (сигнал на продажу)
	var signal float64
	if lastRSI < 30 {
		// Перепроданность: сильный сигнал на покупку
		signal = 100 * (30 - lastRSI) / 30
	} else if lastRSI > 70 {
		// Перекупленность: сильный сигнал на продажу
		signal = -100 * (lastRSI - 70) / 30
	} else {
		// Нейтральная зона: слабый сигнал
		signal = (50 - lastRSI) * 2
	}

	return signal
}

// calculateMACD рассчитывает MACD и возвращает сигнал
func (a *Analyzer) calculateMACD(closes []float64) float64 {
	macd, signal, hist := talib.Macd(
		closes,
		a.config.MACDFast,
		a.config.MACDSlow,
		a.config.MACDSignal,
	)

	// Получаем последние значения
	lastMACD := macd[len(macd)-1]
	lastSignal := signal[len(signal)-1]
	lastHist := hist[len(hist)-1]

	// Нормализуем к диапазону -100..100
	// Используем гистограмму и разницу MACD-Signal для определения силы сигнала
	var signalStrength float64

	// Нормализуем гистограмму
	maxHist := 0.0
	for _, h := range hist {
		if math.Abs(h) > maxHist {
			maxHist = math.Abs(h)
		}
	}

	if maxHist > 0 {
		// Нормализованная сила сигнала
		normHist := lastHist / maxHist * 100

		// Определяем направление на основе положения MACD относительно сигнальной линии
		if lastMACD > lastSignal {
			signalStrength = normHist  // Положительный сигнал (покупка)
		} else {
			signalStrength = -normHist // Отрицательный сигнал (продажа)
		}
	}

	return signalStrength
}

// calculateBollingerBands рассчитывает Bollinger Bands и возвращает сигнал
func (a *Analyzer) calculateBollingerBands(closes []float64) float64 {
	upper, middle, lower := talib.BBands(
		closes,
		a.config.BBPeriod,
		2.0, // Стандартное отклонение
		2.0,
		0,
	)

	// Получаем последние значения
	lastUpper := upper[len(upper)-1]
	lastMiddle := middle[len(middle)-1]
	lastLower := lower[len(lower)-1]
	lastClose := closes[len(closes)-1]

	// Ширина полосы как процент от средней линии
	bandwidth := (lastUpper - lastLower) / lastMiddle

	// Позиция цены в полосе (0 = нижняя граница, 1 = верхняя граница)
	percentB := (lastClose - lastLower) / (lastUpper - lastLower)

	// Нормализуем к диапазону -100..100
	var signal float64

	if percentB > 1 {
		// Цена выше верхней полосы: сильный сигнал на продажу
		signal = -100
	} else if percentB < 0 {
		// Цена ниже нижней полосы: сильный сигнал на покупку
		signal = 100
	} else if percentB > 0.8 {
		// Цена близка к верхней полосе: умеренный сигнал на продажу
		signal = -80 * bandwidth  // Сильнее, если полоса широкая
	} else if percentB < 0.2 {
		// Цена близка к нижней полосе: умеренный сигнал на покупку
		signal = 80 * bandwidth   // Сильнее, если полоса широкая
	} else {
		// Цена в середине полосы: слабый сигнал
		signal = (0.5 - percentB) * 100 * bandwidth
	}

	return signal
}

// calculateIchimoku рассчитывает Ichimoku Cloud и возвращает сигнал
func (a *Analyzer) calculateIchimoku(highs, lows, closes []float64) float64 {
	tenkanPeriod := 9
	kijunPeriod := 26
	senkouBPeriod := 52

	// Tenkan-sen (конверсионная линия)
	tenkan := calculateIchimokuLine(highs, lows, tenkanPeriod)

	// Kijun-sen (базовая линия)
	kijun := calculateIchimokuLine(highs, lows, kijunPeriod)

	// Senkou Span A (первая линия облака)
	senkouA := make([]float64, len(tenkan))
	for i := range tenkan {
		if i >= len(tenkan)-kijunPeriod || tenkan[i] == 0 || kijun[i] == 0 {
			senkouA[i] = 0
		} else {
			senkouA[i] = (tenkan[i] + kijun[i]) / 2
		}
	}

	// Senkou Span B (вторая линия облака)
	senkouB := calculateIchimokuLine(highs, lows, senkouBPeriod)

	// Последние значения для анализа
	lastClose := closes[len(closes)-1]
	lastTenkan := tenkan[len(tenkan)-1]
	lastKijun := kijun[len(kijun)-1]
	lastSenkouA := senkouA[len(senkouA)-1]
	lastSenkouB := senkouB[len(senkouB)-1]

	// Определяем положение цены относительно облака
	var signal float64

	// Цена выше облака: бычий сигнал
	if lastClose > math.Max(lastSenkouA, lastSenkouB) {
		signal = 50

		// Дополнительная сила, если Tenkan выше Kijun (бычий кросс)
		if lastTenkan > lastKijun {
			signal += 30
		}

		// Еще сильнее, если облако восходящее
		if lastSenkouA > lastSenkouB {
			signal += 20
		}
	}
	// Цена ниже облака: медвежий сигнал
	else if lastClose < math.Min(lastSenkouA, lastSenkouB) {
		signal = -50

		// Дополнительная сила, если Tenkan ниже Kijun (медвежий кросс)
		if lastTenkan < lastKijun {
			signal -= 30
		}

		// Еще сильнее, если облако нисходящее
		if lastSenkouA < lastSenkouB {
			signal -= 20
		}
	}
	// Цена внутри облака: слабый сигнал или неопределенность
	else {
		// Направление определяется положением Tenkan относительно Kijun
		if lastTenkan > lastKijun {
			signal = 25
		} else if lastTenkan < lastKijun {
			signal = -25
		}
	}

	return signal
}

// calculateIchimokuLine вспомогательная функция для расчета линий Ichimoku
func calculateIchimokuLine(highs, lows []float64, period int) []float64 {
	result := make([]float64, len(highs))

	for i := 0; i < len(highs); i++ {
		if i < period-1 {
			result[i] = 0 // Нет значений для начальных точек
			continue
		}

		// Находим максимум и минимум за период
		periodHigh := highs[i]
		periodLow := lows[i]

		for j := i - period + 1; j < i; j++ {
			if highs[j] > periodHigh {
				periodHigh = highs[j]
			}
			if lows[j] < periodLow {
				periodLow = lows[j]
			}
		}

		// Рассчитываем значение линии
		result[i] = (periodHigh + periodLow) / 2
	}

	return result
}

// calculateATR рассчитывает ATR (Average True Range) и интерпретирует его
func (a *Analyzer) calculateATR(highs, lows, closes []float64) float64 {
	period := 14 // Стандартный период для ATR

	atr := talib.Atr(highs, lows, closes, period)
	lastATR := atr[len(atr)-1]
	lastClose := closes[len(closes)-1]

	// Нормализуем ATR как процент от цены
	atrPercent := (lastATR / lastClose) * 100

	// ATR сам по себе не дает направления, но помогает оценить волатильность
	// Используем ATR для корректировки силы сигнала в зависимости от волатильности:
	// - Высокий ATR: возможна коррекция после сильного движения
	// - Низкий ATR: возможно, скоро начнется движение

	// Определим нормы для криптовалютного рынка
	var signal float64

	if atrPercent > 5 {
		// Очень высокая волатильность: возможен разворот
		signal = -20
	} else if atrPercent > 3 {
		// Высокая волатильность: движение может продолжиться, но риски высоки
		signal = -10
	} else if atrPercent < 0.5 {
		// Очень низкая волатильность: вероятен скорый прорыв
		signal = 20
	} else if atrPercent < 1 {
		// Низкая волатильность: накопление энергии для движения
		signal = 10
	} else {
		// Средняя волатильность: нейтральный сигнал
		signal = 0
	}

	return signal
}