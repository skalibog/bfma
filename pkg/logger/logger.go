package logger

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Глобальный экземпляр логгера
var (
	globalLogger *zap.Logger
	once         sync.Once
)

// Init инициализирует глобальный логгер
func Init() {
	once.Do(func() {
		globalLogger = newLogger()
	})

	// Очистка логов при перезапуске
	if err := os.Truncate("app.json.log", 0); err != nil {
		panic(err)
	}
}

// GetLogger возвращает глобальный экземпляр логгера
func GetLogger() *zap.Logger {
	if globalLogger == nil {
		Init()
	}
	return globalLogger
}

// Вспомогательные функции для удобства использования
func Info(msg string, fields ...zap.Field) {
	GetLogger().Info(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	GetLogger().Error(msg, fields...)
}

func Debug(msg string, fields ...zap.Field) {
	GetLogger().Debug(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	GetLogger().Warn(msg, fields...)
}

func Fatal(msg string, fields ...zap.Field) {
	GetLogger().Fatal(msg, fields...)
}

// newLogger создает новый экземпляр логгера (ваша существующая функция New)
func newLogger() *zap.Logger {
	// Конфигурация энкодера
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("02.01.2006 - 15:04:05.000000000Z07:00")
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// Создание энкодеров
	//consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	readableFileEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	jsonFileEncoder := zapcore.NewJSONEncoder(encoderConfig)

	// Файлы
	readableFile, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	jsonFile, err := os.OpenFile("app.json.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}

	// Writers
	//consoleWriter := zapcore.AddSync(os.Stdout)
	readableFileWriter := zapcore.AddSync(readableFile)
	jsonFileWriter := zapcore.AddSync(jsonFile)

	// Уровень логирования
	level := zapcore.DebugLevel

	// Tee: console + читаемый файл + JSON файл
	core := zapcore.NewTee(
		//zapcore.NewCore(consoleEncoder, consoleWriter, level),
		zapcore.NewCore(readableFileEncoder, readableFileWriter, level),
		zapcore.NewCore(jsonFileEncoder, jsonFileWriter, level),
	)

	return zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
}
