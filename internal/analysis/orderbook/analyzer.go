package orderbook

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/internal/storage"
	"github.com/skalibog/bfma/pkg/models"
)

// Analyzer реализует анализатор стакана заявок
type Analyzer struct {
	config config.OrderBookConfig
}

// NewAnalyzer создает новый анализатор стакана заявок
func NewAnalyzer(cfg config.OrderBookConfig) *Analyzer {
	return &Analyzer{
		config: cfg,
	}
}

// Analyze анализирует стакан заявок и возвращает сигнал от -100 до 100
func (a *Analyzer) Analyze(ctx context.Context, storage storage.Storage, symbol string) (float64, error) {
	// Получаем последнее состояние стакана
	orderBook, err := storage.GetLatestOrderBook(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("ошибка получения стакана: %w", err)
	}

	// Конвертируем строковые значения в числа для анализа
	bids, asks, err := a.convertOrderBookLevels(orderBook)
	if err != nil {
		return 0, fmt.Errorf("ошибка конвертации уровней стакана: %w", err)
	}

	// Рассчитываем различные метрики стакана
	imbalanceSignal := a.calculateImbalance(bids, asks)
	depthSignal := a.calculateDepth(bids, asks, orderBook.Timestamp)
	supportResistanceSignal := a.calculateSupportResistance(bids, asks)
	spreadsSignal := a.calculateSpreads(bids, asks)

	// Комбинируем сигналы с весами
	weightedSignal := (imbalanceSignal * 0.4) +
		(depthSignal * 0.2) +
		(supportResistanceSignal * 0.25) +
		(spreadsSignal * 0.15)

	return weightedSignal, nil
}

// convertOrderBookLevels конвертирует строковые цены и объемы в числа
func (a *Analyzer) convertOrderBookLevels(orderBook *models.OrderBook) ([]OrderLevel, []OrderLevel, error) {
	bids := make([]OrderLevel, len(orderBook.Bids))
	asks := make([]OrderLevel, len(orderBook.Asks))

	// Обрабатываем биды
	for i, bid := range orderBook.Bids {
		price, err := strconv.ParseFloat(bid.Price, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("ошибка парсинга цены бида: %w", err)
		}

		amount, err := strconv.ParseFloat(bid.Amount, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("ошибка парсинга объема бида: %w", err)
		}

		bids[i] = OrderLevel{
			Price:  price,
			Amount: amount,
		}
	}

	// Обрабатываем аски
	for i, ask := range orderBook.Asks {
		price, err := strconv.ParseFloat(ask.Price, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("ошибка парсинга цены аска: %w", err)
		}

		amount, err := strconv.ParseFloat(ask.Amount, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("ошибка парсинга объема аска: %w", err)
		}

		asks[i] = OrderLevel{
			Price:  price,
			Amount: amount,
		}
	}

	// Сортируем биды по убыванию цены
	sort.Slice(bids, func(i, j int) bool {
		return bids[i].Price > bids[j].Price
	})

	// Сортируем аски по возрастанию цены
	sort.Slice(asks, func(i, j int) bool {
		return asks[i].Price < asks[j].Price
	})

	return bids, asks, nil
}

// calculateImbalance рассчитывает дисбаланс между спросом и предложением
func (a *Analyzer) calculateImbalance(bids, asks []OrderLevel) float64 {
	// Рассчитываем суммарный объем на покупку и продажу
	var totalBidVolume, totalAskVolume float64

	for _, bid := range bids {
		totalBidVolume += bid.Amount
	}

	for _, ask := range asks {
		totalAskVolume += ask.Amount
	}

	// Если объемы нулевые, возвращаем 0
	if totalBidVolume == 0 && totalAskVolume == 0 {
		return 0
	}

	// Рассчитываем дисбаланс как пропорцию между объемами
	totalVolume := totalBidVolume + totalAskVolume
	bidRatio := totalBidVolume / totalVolume
	askRatio := totalAskVolume / totalVolume

	// Нормализуем к диапазону -100..100
	// Положительные значения указывают на преобладание покупателей
	imbalance := (bidRatio - askRatio) * 100

	// Применяем порог дисбаланса
	if math.Abs(imbalance) < a.config.ImbalanceThreshold {
		imbalance = 0
	}

	return imbalance
}

// calculateDepth анализирует глубину стакана и концентрацию ликвидности
func (a *Analyzer) calculateDepth(bids, asks []OrderLevel, timestamp interface{}) float64 {
	// Уровни для анализа (% от текущей цены)
	depthLevels := []float64{0.005, 0.01, 0.02, 0.05} // 0.5%, 1%, 2%, 5%

	// Рассчитываем среднюю цену между лучшим бидом и аском (mid price)
	midPrice := (bids[0].Price + asks[0].Price) / 2

	// Объемы на различных уровнях глубины
	bidDepthVolumes := make([]float64, len(depthLevels))
	askDepthVolumes := make([]float64, len(depthLevels))

	// Заполняем объемы по бидам
	for _, bid := range bids {
		priceDeviation := 1 - (bid.Price / midPrice)
		for i, level := range depthLevels {
			if priceDeviation <= level {
				bidDepthVolumes[i] += bid.Amount
			}
		}
	}

	// Заполняем объемы по аскам
	for _, ask := range asks {
		priceDeviation := (ask.Price / midPrice) - 1
		for i, level := range depthLevels {
			if priceDeviation <= level {
				askDepthVolumes[i] += ask.Amount
			}
		}
	}

	// Сравниваем объемы на разных уровнях глубины
	var depthRatios []float64
	for i := range depthLevels {
		if bidDepthVolumes[i] == 0 && askDepthVolumes[i] == 0 {
			depthRatios = append(depthRatios, 0)
			continue
		}

		// Соотношение объемов покупки/продажи на данном уровне глубины
		ratio := 0.0
		totalVolume := bidDepthVolumes[i] + askDepthVolumes[i]
		if totalVolume > 0 {
			ratio = (bidDepthVolumes[i] - askDepthVolumes[i]) / totalVolume
		}
		depthRatios = append(depthRatios, ratio)
	}

	// Взвешенный сигнал глубины
	// Близкие уровни имеют больший вес
	weights := []float64{0.4, 0.3, 0.2, 0.1}
	var weightedSignal float64
	for i, ratio := range depthRatios {
		weightedSignal += ratio * weights[i]
	}

	// Нормализуем к диапазону -100..100
	return weightedSignal * 100
}

// calculateSupportResistance анализирует уровни поддержки и сопротивления
func (a *Analyzer) calculateSupportResistance(bids, asks []OrderLevel) float64 {
	// Нужно как минимум несколько уровней для анализа
	if len(bids) < 3 || len(asks) < 3 {
		return 0
	}

	// Находим уровни с высокой концентрацией ордеров
	bidLevels := findSignificantLevels(bids)
	askLevels := findSignificantLevels(asks)

	// Если нет значимых уровней, возвращаем 0
	if len(bidLevels) == 0 && len(askLevels) == 0 {
		return 0
	}

	// Текущая цена (средняя между лучшим бидом и аском)
	currentPrice := (bids[0].Price + asks[0].Price) / 2

	// Находим ближайшие значимые уровни
	closestSupport := findClosestLevel(bidLevels, currentPrice, false)
	closestResistance := findClosestLevel(askLevels, currentPrice, true)

	// Если не удалось найти уровни, возвращаем 0
	if closestSupport == nil || closestResistance == nil {
		return 0
	}

	// Рассчитываем расстояние до уровней в процентах
	supportDistance := (currentPrice - closestSupport.Price) / currentPrice
	resistanceDistance := (closestResistance.Price - currentPrice) / currentPrice

	// Оцениваем силу уровней по объему
	supportStrength := math.Min(1.0, closestSupport.Amount/1000) // Нормализация объема
	resistanceStrength := math.Min(1.0, closestResistance.Amount/1000)

	// Рассчитываем сигнал на основе расстояния и силы уровней
	// Чем ближе поддержка и дальше сопротивление, тем более бычий сигнал
	var signal float64
	if supportDistance == 0 && resistanceDistance == 0 {
		signal = 0
	} else {
		// Нормализуем расстояния для сравнения
		normalizedSupportDist := math.Min(supportDistance, 0.1) / 0.1
		normalizedResistanceDist := math.Min(resistanceDistance, 0.1) / 0.1

		// Рассчитываем соотношение расстояний, с учетом силы уровней
		supportFactor := (1 - normalizedSupportDist) * supportStrength
		resistanceFactor := normalizedResistanceDist * resistanceStrength

		// Итоговый сигнал на основе факторов
		signal = (supportFactor - resistanceFactor) * 100
	}

	return signal
}

// calculateSpreads анализирует спреды и распределение ордеров
func (a *Analyzer) calculateSpreads(bids, asks []OrderLevel) float64 {
	// Текущий спред
	currentSpread := (asks[0].Price - bids[0].Price) / ((asks[0].Price + bids[0].Price) / 2)

	// Рассчитываем средние спреды между уровнями
	bidSpreads := calculateAverageSpreads(bids, 5)
	askSpreads := calculateAverageSpreads(asks, 5)

	// Сравниваем спреды между бидами и асками
	// Более широкие спреды на бидах означают меньшую поддержку снизу
	// Более широкие спреды на асках означают меньшее сопротивление сверху
	spreadRatio := 0.0
	if bidSpreads > 0 && askSpreads > 0 {
		spreadRatio = (askSpreads - bidSpreads) / math.Max(bidSpreads, askSpreads)
	}

	// Нормализуем текущий спред и расширение спреда
	// Узкий спред обычно означает высокую ликвидность и стабильность
	spreadFactor := math.Min(currentSpread*100, 1.0)

	// Рассчитываем сигнал на основе спредов
	// Положительный, если аски имеют более широкие спреды (меньше сопротивления)
	signal := spreadRatio * (1 - spreadFactor) * 50

	return signal
}

// findSignificantLevels находит уровни с высокой концентрацией ордеров
func findSignificantLevels(levels []OrderLevel) []OrderLevel {
	if len(levels) == 0 {
		return []OrderLevel{}
	}

	// Рассчитываем средний объем
	var totalVolume float64
	for _, level := range levels {
		totalVolume += level.Amount
	}
	avgVolume := totalVolume / float64(len(levels))

	// Находим уровни с объемом выше среднего
	var significantLevels []OrderLevel
	for _, level := range levels {
		if level.Amount > avgVolume*1.5 {
			significantLevels = append(significantLevels, level)
		}
	}

	return significantLevels
}

// findClosestLevel находит ближайший уровень к заданной цене
func findClosestLevel(levels []OrderLevel, price float64, above bool) *OrderLevel {
	if len(levels) == 0 {
		return nil
	}

	var closestLevel *OrderLevel
	closestDistance := math.MaxFloat64

	for i, level := range levels {
		// Проверяем условие - выше или ниже заданной цены
		if (above && level.Price <= price) || (!above && level.Price >= price) {
			continue
		}

		distance := math.Abs(level.Price - price)
		if distance < closestDistance {
			closestDistance = distance
			closestLevel = &levels[i]
		}
	}

	return closestLevel
}

// calculateAverageSpreads рассчитывает средние спреды между уровнями
func calculateAverageSpreads(levels []OrderLevel, count int) float64 {
	if len(levels) < count+1 {
		count = len(levels) - 1
	}

	if count <= 0 {
		return 0
	}

	var totalSpread float64
	for i := 0; i < count; i++ {
		if i+1 < len(levels) {
			// Для асков: levels[i+1].Price > levels[i].Price
			// Для бидов: levels[i].Price > levels[i+1].Price (они отсортированы по убыванию)
			var higher, lower float64
			if levels[0].Price > levels[len(levels)-1].Price {
				// Биды
				higher = levels[i].Price
				lower = levels[i+1].Price
			} else {
				// Аски
				higher = levels[i+1].Price
				lower = levels[i].Price
			}

			spread := (higher - lower) / lower
			totalSpread += spread
		}
	}

	return totalSpread / float64(count)
}

// OrderLevel представляет уровень с численными значениями
type OrderLevel struct {
	Price  float64
	Amount float64
}
