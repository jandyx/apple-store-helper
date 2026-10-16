package main

import (
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/services"
	"apple-store-helper/view"
)

// filterAll 不筛选
const filterAll = "全部"

// filterOptions 按状态筛选的选项，只影响展示，不影响监听范围
var filterOptions = []string{
	filterAll,
	services.StatusInStock,
	services.StatusOutStock,
	services.StatusFailed,
	services.StatusStoreClosed,
	services.StatusDisabled,
	services.StatusWait,
}

// listenList 监听列表：每行显示状态、时间、地区、门店、型号，并可单独停用或删除
type listenList struct {
	mu     sync.Mutex
	rows   []services.Row
	filter string
	list   *widget.List
	empty  *widget.Label
	hint   *widget.Label
	choose *widget.Select
}

func newListenList() *listenList {
	l := &listenList{filter: filterAll}
	l.empty = widget.NewLabel("还没有监听项。选择门店和型号后点「添加」。")
	l.hint = widget.NewLabel("")
	l.choose = widget.NewSelect(filterOptions, func(value string) {
		l.mu.Lock()
		l.filter = value
		l.mu.Unlock()
		l.Refresh()
	})
	l.choose.Selected = filterAll

	l.list = widget.NewList(
		func() int {
			l.mu.Lock()
			defer l.mu.Unlock()
			return len(l.rows)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			toggle := widget.NewButton("停用", nil)
			remove := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			remove.Importance = widget.LowImportance
			return container.NewBorder(nil, nil, nil, container.NewHBox(toggle, remove), label)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			l.mu.Lock()
			if id >= len(l.rows) {
				l.mu.Unlock()
				return
			}
			row := l.rows[id]
			l.mu.Unlock()

			box := obj.(*fyne.Container)
			label := box.Objects[0].(*widget.Label)
			buttons := box.Objects[1].(*fyne.Container)
			toggle := buttons.Objects[0].(*widget.Button)
			remove := buttons.Objects[1].(*widget.Button)

			label.SetText(rowText(row.Item))
			if row.Item.Disabled {
				toggle.SetText("启用")
			} else {
				toggle.SetText("停用")
			}
			toggle.OnTapped = func() {
				services.Listen.SetDisabled(row.Key, !row.Item.Disabled)
				saveSettings()
			}
			remove.OnTapped = func() {
				desc := fmt.Sprintf("%s %s", row.Item.Store.CityStoreName, services.ProductName(row.Item.Product))
				dialog.ShowConfirm("删除监听项", "确定删除「"+desc+"」吗？", func(ok bool) {
					if ok {
						services.Listen.Remove(row.Key)
						saveSettings()
					}
				}, view.Window)
			}
		},
	)
	l.list.OnSelected = func(id widget.ListItemID) { l.list.Unselect(id) }
	return l
}

func rowText(item services.ListenItem) string {
	text := fmt.Sprintf("[%s]", item.Status)
	if t := item.Time.String(); t != "" {
		text += " " + t
	}
	return fmt.Sprintf("%s  %s  %s  %s", text, item.Area.Title, item.Store.CityStoreName, services.ProductName(item.Product))
}

// Refresh 从监听服务重新读取数据。可在任意协程调用
func (l *listenList) Refresh() {
	all := services.Listen.Rows()
	l.mu.Lock()
	filter := l.filter
	l.rows = filterRows(all, filter)
	shown := len(l.rows)
	l.mu.Unlock()

	switch {
	case len(all) == 0:
		l.empty.SetText("还没有监听项。选择门店和型号后点「添加」。")
		l.empty.Show()
	case shown == 0:
		l.empty.SetText("没有「" + filter + "」的监听项。")
		l.empty.Show()
	default:
		l.empty.Hide()
	}

	if filter == filterAll {
		l.hint.SetText(fmt.Sprintf("共 %d 项", len(all)))
	} else {
		l.hint.SetText(fmt.Sprintf("%d / %d 项", shown, len(all)))
	}
	l.list.Refresh()
}

// filterRows 按状态筛选，失败原因不参与匹配
func filterRows(rows []services.Row, filter string) []services.Row {
	if filter == filterAll || filter == "" {
		return rows
	}
	var out []services.Row
	for _, row := range rows {
		if services.StatusCategory(row.Item.Status) == filter {
			out = append(out, row)
		}
	}
	return out
}

// Header 列表标题栏：标题、数量和筛选
func (l *listenList) Header() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("监听列表", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	filter := container.NewGridWrap(fyne.NewSize(140, l.choose.MinSize().Height), l.choose)
	return container.NewBorder(nil, nil, title, container.NewHBox(l.hint, widget.NewLabel("筛选:"), filter))
}

// Widget 列表控件，无监听项时显示提示
func (l *listenList) Widget() fyne.CanvasObject {
	return container.NewStack(l.list, container.NewVBox(l.empty))
}
