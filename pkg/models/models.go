package models

import (
	"time"
)

// Candle представляет свечу
type Candle struct {
	Symbol    string
	Interval  string
	OpenTime  time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime time.Time
}

// OrderBookLevel представляет уровень стакана
type OrderBookLevel struct {
	Price  string
	Amount string
}

// OrderBook представляет стакан заявок
type OrderBook struct {
	Symbol    string
	Timestamp time.Time
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
}

// FundingRate представляет ставку финансирования
type FundingRate struct {
	Symbol          string
	Rate            string
	Timestamp       time.Time
	NextFundingTime time.Time
}

// OpenInterest представляет открытый интерес
type OpenInterest struct {
	Symbol    string
	Value     string
	Timestamp time.Time
}

// SignalResult представляет результат сигнала
type SignalResult struct {
	Symbol         string
	Timestamp      time.Time
	Recommendation string
	SignalStrength float64
	PositionSize   float64
	CurrentPrice   float64
	Components     map[string]float64
}