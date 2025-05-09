package ui

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/skalibog/bfma/internal/analysis/aggregator"
	"github.com/skalibog/bfma/internal/config"
	"github.com/skalibog/bfma/pkg/models"
)

// TermUI представляет терминальный интерфейс
type TermUI struct {
	app          *tview.Application
	analyzer     *aggregator.Analyzer
	signalsTable *tview.Table
	detailsView  *tview.TextView
	logsView     *tview.TextView
	signals      map[string]*models.SignalResult
	signalsMutex sync.RWMutex
	config       config.UIConfig
}

// NewTermUI создает новый терминальный интерфейс
func NewTermUI(cfg config.UIConfig, analyzer *aggregator.Analyzer) (*TermUI, error) {
	app := tview.NewApplication()

	// Таблица сигналов
	signalsTable := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false)

	// Заголовки
	signalsTable.SetCell(0, 0, tview.NewTableCell("Символ").SetSelectable(false).SetAttributes(tcell.AttrBold))
	signalsTable.SetCell(0, 1, tview.NewTableCell("Сигнал").SetSelectable(false).SetAttributes(tcell.AttrBold))
	signalsTable.SetCell(0, 2, tview.NewTableCell("Сила").SetSelectable(false).SetAttributes(tcell.AttrBold))
	signalsTable.SetCell(0, 3, tview.NewTableCell("Размер").SetSelectable(false).SetAttributes(tcell.AttrBold))
	signalsTable.SetCell(0, 4, tview.NewTableCell("Цена").SetSelectable(false).SetAttributes(tcell.AttrBold))
	signalsTable.SetCell(0, 5, tview.NewTableCell("Время").SetSelectable(false).SetAttributes(tcell.AttrBold))

	// Подробности сигнала
	detailsView := tview.NewTextView().
		SetDynamicColors(true).
		SetBorder(true).
		SetTitle("Детали сигнала")

	// Лог событий
	logsView := tview.NewTextView().
		SetScrollable(true).
		SetDynamicColors(true).
		SetBorder(true).
		SetTitle("Журнал событий")

	// Главный layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(signalsTable, 0, 3, true).
		AddItem(tview.NewFlex().
			AddItem(detailsView, 0, 1, false).
			AddItem(logsView, 0, 1, false),
			0, 2, false)

	// Обработка выбора строки в таблице
	signalsTable.SetSelectedFunc(func(row, column int) {
		if row <= 0 {
			return
		}
		cellSymbol := signalsTable.GetCell(row, 0)
		if cellSymbol == nil {
			return
		}
		symbol := cellSymbol.Text
		ui.updateDetailsView(symbol)
	})

	ui := &TermUI{
		app:          app,
		analyzer:     analyzer,
		signalsTable: signalsTable,
		detailsView:  detailsView,
		logsView:     logsView,
		signals:      make(map[string]*models.SignalResult),
		config:       cfg,
	}

	app.SetRoot(flex, true).EnableMouse(true)

	// Добавляем обработку клавиш
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			app.Stop()
			return nil
		}
		return event
	})

	return ui, nil
}

// Start запускает пользовательский интерфейс
func (ui *TermUI) Start() {
	// Запускаем обновление UI в отдельном потоке
	go func() {
		ticker := time.NewTicker(time.Duration(ui.config.RefreshRate) * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			ui.app.QueueUpdateDraw(func() {
				ui.refreshSignalsTable()
			})
		}
	}()

	// Запускаем основной цикл UI
	if err := ui.app.Run(); err != nil {
		fmt.Printf("Ошибка запуска UI: %v\n", err)
	}
}

// UpdateSignals обновляет сигналы
func (ui *TermUI) UpdateSignals(signals map[string]*models.SignalResult) {
	ui.signalsMutex.Lock()
	defer ui.signalsMutex.Unlock()

	// Обновляем сигналы
	for symbol, signal := range signals {
		ui.signals[symbol] = signal
		ui.logSignalChange(symbol, signal)
	}
}

// refreshSignalsTable обновляет таблицу сигналов
func (ui *TermUI) refreshSignalsTable() {
	ui.signalsMutex.RLock()
	defer ui.signalsMutex.RUnlock()

	// Очищаем таблицу, оставляя заголовки
	ui.signalsTable.Clear()
	ui.signalsTable.SetCell(0, 0, tview.NewTableCell("Символ").SetSelectable(false).SetAttributes(tcell.AttrBold))
	ui.signalsTable.SetCell(0, 1, tview.NewTableCell("Сигнал").SetSelectable(false).SetAttributes(tcell.AttrBold))
	ui.signalsTable.SetCell(0, 2, tview.NewTableCell("Сила").SetSelectable(false).SetAttributes(tcell.AttrBold))
	ui.signalsTable.SetCell(0, 3, tview.NewTableCell("Размер").SetSelectable(false).SetAttributes(tcell.AttrBold))
	ui.signalsTable.SetCell(0, 4, tview.NewTableCell("Цена").SetSelectable(false).SetAttributes(tcell.AttrBold))
	ui.signalsTable.SetCell(0, 5, tview.NewTableCell("Время").SetSelectable(false).SetAttributes(tcell.AttrBold))

	// Добавляем строки с сигналами
	row := 1
	for symbol, signal := range ui.signals {
		// Символ
		ui.signalsTable.SetCell(row, 0, tview.NewTableCell(symbol))

		// Сигнал
		signalCell := tview.NewTableCell(signal.Recommendation)
		switch signal.Recommendation {
		case "СИЛЬНАЯ ПОКУПКА":
			signalCell.SetTextColor(tcell.ColorGreen).SetAttributes(tcell.AttrBold)
		case "ПОКУПКА":
			signalCell.SetTextColor(tcell.ColorGreen)
		case "СИЛЬНАЯ ПРОДАЖА":
			signalCell.SetTextColor(tcell.ColorRed).SetAttributes(tcell.AttrBold)
		case "ПРОДАЖА":
			signalCell.SetTextColor(tcell.ColorRed)
		default:
			signalCell.SetTextColor(tcell.ColorYellow)
		}
		ui.signalsTable.SetCell(row, 1, signalCell)

		// Сила сигнала
		strengthStr := fmt.Sprintf("%.2f", signal.SignalStrength)
		strengthCell := tview.NewTableCell(strengthStr)
		if signal.SignalStrength > 0 {
			strengthCell.SetTextColor(tcell.ColorGreen)
		} else if signal.SignalStrength < 0 {
			strengthCell.SetTextColor(tcell.ColorRed)
		}
		ui.signalsTable.SetCell(row, 2, strengthCell)

		// Размер позиции
		ui.signalsTable.SetCell(row, 3, tview.NewTableCell(fmt.Sprintf("%.1f", signal.PositionSize)))

		// Цена
		ui.signalsTable.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%.2f", signal.CurrentPrice)))

		// Время
		ui.signalsTable.SetCell(row, 5, tview.NewTableCell(signal.Timestamp.Format("15:04:05")))

		row++
	}
}

// updateDetailsView обновляет подробную информацию о сигнале
func (ui *TermUI) updateDetailsView(symbol string) {
	ui.signalsMutex.RLock()
	defer ui.signalsMutex.RUnlock()

	signal, ok := ui.signals[symbol]
	if !ok {
		ui.detailsView.SetText(fmt.Sprintf("Нет данных для %s", symbol))
		return
	}

	details := fmt.Sprintf("Символ: [yellow]%s[white]\n", symbol)
	details += fmt.Sprintf("Рекомендация: [blue]%s[white]\n", signal.Recommendation)
	details += fmt.Sprintf("Сила сигнала: [blue]%.2f[white]\n", signal.SignalStrength)
	details += fmt.Sprintf("Размер позиции: [blue]%.1f[white]\n", signal.PositionSize)
	details += fmt.Sprintf("Текущая цена: [blue]%.2f[white]\n", signal.CurrentPrice)
	details += fmt.Sprintf("Время сигнала: [blue]%s[white]\n\n", signal.Timestamp.Format("15:04:05"))

	details += "Компоненты сигнала:\n"
	for name, value := range signal.Components {
		var color string
		if value > 0 {
			color = "green"
		} else if value < 0 {
			color = "red"
		} else {
			color = "white"
		}
		details += fmt.Sprintf("  - %s: [%s]%.2f[white]\n", name, color, value)
	}

	ui.detailsView.SetText(details)
}

// logSignalChange добавляет запись в журнал при изменении сигнала
func (ui *TermUI) logSignalChange(symbol string, signal *models.SignalResult) {
	logEntry := fmt.Sprintf("[%s] %s: %s (%.2f)\n",
		signal.Timestamp.Format("15:04:05"),
		symbol,
		signal.Recommendation,
		signal.SignalStrength)

	ui.app.QueueUpdateDraw(func() {
		fmt.Fprint(ui.logsView, logEntry)
		ui.logsView.ScrollToEnd()
	})
}
