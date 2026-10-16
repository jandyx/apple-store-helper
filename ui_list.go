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

// listenList 监听列表：每行显示状态、时间、地区、门店、型号，并可单独停用或删除
type listenList struct {
	mu    sync.Mutex
	rows  []services.Row
	list  *widget.List
	empty *widget.Label
}

func newListenList() *listenList {
	l := &listenList{}
	l.empty = widget.NewLabel("还没有监听项。选择门店和型号后点「添加」。")

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
				desc := fmt.Sprintf("%s %s", row.Item.Store.CityStoreName, row.Item.Product.Title)
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
	return fmt.Sprintf("%s  %s  %s  %s", text, item.Area.Title, item.Store.CityStoreName, item.Product.Title)
}

// Refresh 从监听服务重新读取数据。可在任意协程调用
func (l *listenList) Refresh() {
	rows := services.Listen.Rows()
	l.mu.Lock()
	l.rows = rows
	l.mu.Unlock()

	if len(rows) == 0 {
		l.empty.Show()
	} else {
		l.empty.Hide()
	}
	l.list.Refresh()
}

// Widget 列表控件，无监听项时显示提示
func (l *listenList) Widget() fyne.CanvasObject {
	return container.NewStack(l.list, container.NewVBox(l.empty))
}
