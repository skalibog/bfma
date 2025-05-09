package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/skalibog/bfma/pkg/logger"
	"go.uber.org/zap"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skalibog/bfma/internal/analysis/aggregator"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/pkg/models"
)

// Стили UI
var (
	// Основные цвета
	primaryColor   = lipgloss.Color("#0077cc")
	secondaryColor = lipgloss.Color("#333333")
	errorColor     = lipgloss.Color("#cc3300")
	successColor   = lipgloss.Color("#33cc33")
	warningColor   = lipgloss.Color("#cccc00")
	// Главный контейнер - будет адаптироваться к размеру экрана
	appStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor)
	// Заголовок - будет адаптироваться к размеру экрана
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(primaryColor).
			Padding(0, 1).
			Align(lipgloss.Center)
	// Секция сигналов - будет адаптироваться к размеру экрана
	signalsHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(secondaryColor).
				Padding(0, 1)
	signalsSectionStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(secondaryColor).
				Padding(0, 1)
	// Секция логов - будет адаптироваться к размеру экрана
	logsHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(secondaryColor).
			Padding(0, 1)
	logsSectionStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(secondaryColor).
				Padding(0, 1)
	// Футер - будет адаптироваться к размеру экрана
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999999")).
			Padding(0, 1)
)

// TermUI представляет терминальный интерфейс
type TermUI struct {
	analyzer      *aggregator.Analyzer
	signals       map[string]*models.SignalResult
	signalsMutex  sync.RWMutex
	logs          []string
	logsMutex     sync.RWMutex
	config        config.UIConfig
	program       *tea.Program
	selectedIndex int
	width         int
	height        int
	logFile       string // Путь к файлу логов
}

// Сообщения для обновления UI
type refreshMsg struct{}
type windowSizeMsg tea.WindowSizeMsg

// bubbleModel - модель для bubbletea
type bubbleModel struct {
	ui *TermUI
}

func NewTermUI(cfg config.UIConfig, analyzer *aggregator.Analyzer, ctx context.Context) (*TermUI, error) {
	ui := &TermUI{
		analyzer:      analyzer,
		signals:       make(map[string]*models.SignalResult),
		logs:          []string{"BFMA запущен. Ожидание данных..."},
		config:        cfg,
		selectedIndex: 0,
		width:         120,
		height:        40,
		logFile:       "app.json.log", // Путь к файлу логов по умолчанию
	}

	// Загружаем логи из файла при запуске
	if err := ui.loadLogsFromFile(); err != nil {
		ui.logs = append(ui.logs, fmt.Sprintf("Ошибка загрузки логов: %v", err))
	}

	// Запускаем таймер для обновления логов
	go func() {
		ticker := time.NewTicker(1 * time.Second) // Интервал обновления логов
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := ui.loadLogsFromFile(); err != nil {
					// Если произошла ошибка, просто игнорируем ее
					// и продолжаем работу
					// Перезагрузка логов
					logger.Warn("Ошибка загрузки логов", zap.Error(err))
				}
			}
		}
	}()

	return ui, nil
}

func (ui *TermUI) Start() {
	model := bubbleModel{ui: ui}
	ui.program = tea.NewProgram(model, tea.WithAltScreen())

	// Запускаем UI
	if err := ui.program.Start(); err != nil {
		fmt.Printf("Ошибка запуска UI: %v\n", err)
	}
}

func (ui *TermUI) UpdateSignals(signals map[string]*models.SignalResult) {
	ui.signalsMutex.Lock()
	defer ui.signalsMutex.Unlock()

	ui.signals = signals

	if ui.program != nil {
		ui.program.Send(refreshMsg{})
	}
}

// Запись лога в файл
func (ui *TermUI) loadLogsFromFile() error {
	file, err := os.Open(ui.logFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Файл не существует, это не ошибка
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var logs []string

	// Регулярное выражение для удаления ANSI-цветов
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)

	// Читаем строки из файла
	for scanner.Scan() {
		line := scanner.Text()

		// Пытаемся распарсить JSON
		var zapLog map[string]interface{}
		if err := json.Unmarshal([]byte(line), &zapLog); err == nil {
			// Успешно распарсили JSON

			// Получаем основные поля
			level, _ := zapLog["level"].(string)
			ts, _ := zapLog["ts"].(string)
			msg, _ := zapLog["msg"].(string)

			// Удаляем ANSI-цвета из уровня логирования
			level = ansiRegex.ReplaceAllString(level, "")

			// Форматируем сообщение
			timestamp := ""
			if t, err := time.Parse("02.01.2006 - 15:04:05.999999999Z07:00", ts); err == nil {
				timestamp = t.Format("15:04:05")
			}

			formattedMsg := fmt.Sprintf("[%s] [%s] %s", timestamp, level, msg)

			// Добавляем дополнительные поля, если они есть
			for k, v := range zapLog {
				if k != "level" && k != "ts" && k != "msg" && k != "caller" {
					formattedMsg += fmt.Sprintf(" (%s: %v)", k, v)
				}
			}

			logs = append(logs, formattedMsg)
		} else {
			// Не удалось распарсить JSON, добавляем как есть
			logs = append(logs, line)
		}

		// Ограничиваем количество логов
		if len(logs) > 50 {
			logs = logs[1:] // Удаляем самую старую запись
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Добавляем загруженные логи
	ui.logsMutex.Lock()
	defer ui.logsMutex.Unlock()

	if len(logs) > 0 {
		ui.logs = logs
		// Ограничиваем количество логов
		if len(ui.logs) > 50 {
			ui.logs = ui.logs[len(ui.logs)-50:]
		}
	}

	return nil
}

func renderLogsSection(logs []string) string {
	header := logsHeaderStyle.Render("ЛОГИ")
	content := strings.Builder{}

	// Показываем последние 6 логов (или больше, если размер экрана позволяет)
	maxLogsToShow := 50
	if logsSectionStyle.GetHeight() > 8 {
		maxLogsToShow = logsSectionStyle.GetHeight() - 2
	}

	start := 0
	if len(logs) > maxLogsToShow {
		start = len(logs) - maxLogsToShow
	}

	for i := start; i < len(logs); i++ {
		// Форматируем лог
		log := logs[i]

		// Выделение по уровню логирования
		if strings.Contains(log, "[ERROR]") {
			log = lipgloss.NewStyle().Foreground(errorColor).Render(log)
		} else if strings.Contains(log, "[INFO]") {
			log = lipgloss.NewStyle().Foreground(successColor).Render(log)
		} else if strings.Contains(log, "[WARN]") {
			log = lipgloss.NewStyle().Foreground(warningColor).Render(log)
		} else if strings.Contains(log, "[DEBUG]") {
			log = lipgloss.NewStyle().Foreground(lipgloss.Color("#9999ff")).Render(log)
		}

		content.WriteString("  " + log + "\n")
	}

	return logsSectionStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			header,
			content.String(),
		),
	)
}

// Методы для bubbletea
func (m bubbleModel) Init() tea.Cmd {
	return nil
}

func (m bubbleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up":
			m.ui.selectedIndex = max(0, m.ui.selectedIndex-1)
		case "down":
			symbols := getSymbolsFromSignals(m.ui.signals)
			m.ui.selectedIndex = min(len(symbols)-1, m.ui.selectedIndex+1)
		case "r": // Добавлена клавиша для перезагрузки логов из файла

		}

	case tea.WindowSizeMsg:
		m.ui.width = msg.Width
		m.ui.height = msg.Height

	case refreshMsg:
		// Просто обновляем UI
	}

	return m, nil
}

func (m bubbleModel) View() string {
	m.ui.signalsMutex.RLock()
	m.ui.logsMutex.RLock()
	defer m.ui.signalsMutex.RUnlock()
	defer m.ui.logsMutex.RUnlock()

	// Создаем компоненты UI
	title := titleStyle.Render("BFMA - Binance Futures Market Analyzer")
	signals := renderSignalsSection(m.ui.signals, m.ui.selectedIndex)
	logs := renderLogsSection(m.ui.logs)
	footer := footerStyle.Render("Клавиши: ↑/↓ - навигация, R - перезагрузить логи, Q - выход")

	// Собираем UI
	return appStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title,
			"\n",
			signals,
			"\n",
			logs,
			"\n",
			footer,
		),
	)
}

// Вспомогательные функции
func renderSignalsSection(signals map[string]*models.SignalResult, selectedIndex int) string {
	header := signalsHeaderStyle.Render("СИГНАЛЫ")
	content := strings.Builder{}

	symbols := getSymbolsFromSignals(signals)

	if len(symbols) == 0 {
		content.WriteString("  Ожидание данных...\n")
	} else {
		for i, symbol := range symbols {
			signal := signals[symbol]

			// Форматируем сигнал с цветом
			signalText := formatSignalText(signal.Recommendation, signal.SignalStrength)

			// Создаем строку данных
			line := fmt.Sprintf("  %s: %s (%.2f) Цена: %.2f",
				symbol, signalText, signal.SignalStrength, signal.CurrentPrice)

			// Выделяем выбранную строку
			if i == selectedIndex {
				line = "> " + line[2:]
				line = lipgloss.NewStyle().Background(lipgloss.Color("#222222")).Render(line)
			}

			content.WriteString(line + "\n")
		}
	}

	return signalsSectionStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			header,
			content.String(),
		),
	)
}

// Вспомогательные функции
func formatSignalText(recommendation string, strength float64) string {
	var style lipgloss.Style

	switch recommendation {
	case "СИЛЬНАЯ ПОКУПКА":
		style = lipgloss.NewStyle().Foreground(successColor).Bold(true)
	case "ПОКУПКА":
		style = lipgloss.NewStyle().Foreground(successColor)
	case "СИЛЬНАЯ ПРОДАЖА":
		style = lipgloss.NewStyle().Foreground(errorColor).Bold(true)
	case "ПРОДАЖА":
		style = lipgloss.NewStyle().Foreground(errorColor)
	default:
		style = lipgloss.NewStyle().Foreground(warningColor)
	}

	return style.Render(recommendation)
}

func getSymbolsFromSignals(signals map[string]*models.SignalResult) []string {
	symbols := make([]string, 0, len(signals))
	for symbol := range signals {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// Вспомогательные функции min/max для Go до 1.21
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
