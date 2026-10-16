package main

import (
	"fmt"
	"strings"

	"apple-store-helper/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const storePickerPlaceHolder = "请选择自提门店（可多选）"

// storePicker 门店多选：点击按钮弹出勾选列表
type storePicker struct {
	Button   *widget.Button
	options  []string
	selected []string
}

func newStorePicker(options []string) *storePicker {
	p := &storePicker{options: options}
	p.Button = widget.NewButton(storePickerPlaceHolder, p.showDialog)
	p.Button.Alignment = widget.ButtonAlignLeading
	return p
}

// SetOptions 切换地区时更新可选门店，并清空已选
func (p *storePicker) SetOptions(options []string) {
	p.options = options
	p.SetSelected(nil)
}

// SetSelected 设置已选门店，忽略不在当前选项中的值
func (p *storePicker) SetSelected(selected []string) {
	valid := map[string]bool{}
	for _, option := range p.options {
		valid[option] = true
	}
	p.selected = nil
	for _, s := range selected {
		// 旧版门店名带首尾空格，去掉后再匹配
		s = strings.TrimSpace(s)
		if valid[s] {
			p.selected = append(p.selected, s)
		}
	}
	p.refresh()
}

func (p *storePicker) Selected() []string {
	return p.selected
}

func (p *storePicker) refresh() {
	switch n := len(p.selected); {
	case n == 0:
		p.Button.SetText(storePickerPlaceHolder)
	case n <= 3:
		p.Button.SetText(strings.Join(p.selected, "、"))
	default:
		p.Button.SetText(fmt.Sprintf("%s 等 %d 家", strings.Join(p.selected[:3], "、"), n))
	}
}

func (p *storePicker) showDialog() {
	check := widget.NewCheckGroup(p.options, nil)
	check.SetSelected(append([]string(nil), p.selected...))

	tools := container.NewHBox(
		widget.NewButton("全选", func() { check.SetSelected(append([]string(nil), p.options...)) }),
		widget.NewButton("全不选", func() { check.SetSelected(nil) }),
	)
	scroll := container.NewVScroll(check)
	scroll.SetMinSize(fyne.NewSize(420, 420))

	d := dialog.NewCustomConfirm("选择门店", "确定", "取消", container.NewBorder(tools, nil, nil, nil, scroll), func(ok bool) {
		if ok {
			p.SetSelected(check.Selected)
		}
	}, view.Window)
	d.Show()
}
