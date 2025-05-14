package config

import (
	"github.com/skalibog/bfma/pkg/logger"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

// Config представляет полную конфигурацию приложения
type Config struct {
	Binance  BinanceConfig  `yaml:"binance"`
	Trading  TradingConfig  `yaml:"trading"`
	Analysis AnalysisConfig `yaml:"analysis"`
	Storage  StorageConfig  `yaml:"storage"`
	UI       UIConfig       `yaml:"ui"`
}

// BinanceConfig содержит настройки подключения к Binance
type BinanceConfig struct {
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
	Testnet   bool   `yaml:"testnet"`
}

// TradingConfig содержит настройки торговли
type TradingConfig struct {
	Symbols      []string `yaml:"symbols"`
	Interval     string   `yaml:"interval"`
	RiskPerTrade float64  `yaml:"risk_per_trade"`
}

// AnalysisConfig содержит настройки аналитических модулей
type AnalysisConfig struct {
	IntervalSeconds  int                `yaml:"interval_seconds"`
	Technical        TechnicalConfig    `yaml:"technical"`
	OrderBook        OrderBookConfig    `yaml:"orderbook"`
	Funding          FundingConfig      `yaml:"funding"`
	OpenInterest     OpenInterestConfig `yaml:"open_interest"`
	VolumeDelta      VolumeDeltaConfig  `yaml:"volume_delta"`
	SignalThresholds SignalThresholds   `yaml:"signal"`
}

// TechnicalConfig настройки технического анализа
type TechnicalConfig struct {
	Weight     float64 `yaml:"weight"`
	RSIPeriod  int     `yaml:"rsi_period"`
	BBPeriod   int     `yaml:"bb_period"`
	MACDFast   int     `yaml:"macd_fast"`
	MACDSlow   int     `yaml:"macd_slow"`
	MACDSignal int     `yaml:"macd_signal"`
}

// OrderBookConfig настройки анализа стакана
type OrderBookConfig struct {
	Weight             float64 `yaml:"weight"`
	Depth              int     `yaml:"depth"`
	ImbalanceThreshold float64 `yaml:"imbalance_threshold"`
}

// FundingConfig настройки анализа ставок финансирования
type FundingConfig struct {
	Weight           float64 `yaml:"weight"`
	Periods          int     `yaml:"periods"`
	ExtremeThreshold float64 `yaml:"extreme_threshold"`
}

// OpenInterestConfig настройки анализа открытого интереса
type OpenInterestConfig struct {
	Weight          float64 `yaml:"weight"`
	Lookback        int     `yaml:"lookback"`
	ChangeThreshold float64 `yaml:"change_threshold"`
}

// VolumeDeltaConfig настройки анализа дельты объемов
type VolumeDeltaConfig struct {
	Weight                float64 `yaml:"weight"`
	Lookback              int     `yaml:"lookback"`
	SignificanceThreshold float64 `yaml:"significance_threshold"`
}

// SignalThresholds пороговые значения для сигналов
type SignalThresholds struct {
	StrongBuy  float64 `yaml:"threshold_strong_buy"`
	Buy        float64 `yaml:"threshold_buy"`
	Sell       float64 `yaml:"threshold_sell"`
	StrongSell float64 `yaml:"threshold_strong_sell"`
}

// StorageConfig настройки хранения данных
type StorageConfig struct {
	Type         string `yaml:"type"`
	URL          string `yaml:"url"`
	Token        string `yaml:"token"`
	Organization string `yaml:"organization"`
	Bucket       string `yaml:"bucket"`
}

// UIConfig настройки пользовательского интерфейса
type UIConfig struct {
	RefreshRate int  `yaml:"refresh_rate_ms"`
	ShowCharts  bool `yaml:"show_charts"`
}

// Load загружает конфигурацию из файла
func Load(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		logger.Fatal("Ошибка чтения файла конфигурации", zap.Error(err))
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		logger.Fatal("Ошибка разбора файла конфигурации", zap.Error(err))
	}

	logger.Debug("Загружена конфигурация", zap.String("path", path), zap.Any("config", config))

	logger.Info("Загружена конфигурация", zap.Any("Symbols", config.Trading.Symbols))
	return &config, nil
}
