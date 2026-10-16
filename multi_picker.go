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

// multiPicker 多选下拉：点击弹出勾选列表，用于门店和型号
type multiPicker struct {
	field       *pickerField
	title       string
	placeHolder string
	unit        string // 计数量词，如「家」「个」
	options     []string
	selected    []string
	// display 选项的展示文字，选项本身仍作为保存与匹配的值；为空时原样展示
	display func(string) string
}

func newMultiPicker(title string, placeHolder string, unit string, options []string) *multiPicker {
	p := &multiPicker{title: title, placeHolder: placeHolder, unit: unit, options: options}
	p.field = newPickerField(placeHolder, p.showDialog)
	return p
}

// Widget 选择控件，外观与其他下拉框一致
func (p *multiPicker) Widget() fyne.CanvasObject {
	return p.field
}

// SetOptions 切换地区时更新选项，并清空已选
func (p *multiPicker) SetOptions(options []string) {
	p.options = options
	p.SetSelected(nil)
}

// SetSelected 设置已选项，忽略不在当前选项中的值
func (p *multiPicker) SetSelected(selected []string) {
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

func (p *multiPicker) Selected() []string {
	return p.selected
}

func (p *multiPicker) label(option string) string {
	if p.display == nil {
		return option
	}
	return p.display(option)
}

func (p *multiPicker) labels(options []string) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = p.label(o)
	}
	return out
}

func (p *multiPicker) refresh() {
	selected := p.labels(p.selected)
	switch n := len(selected); {
	case n == 0:
		p.field.SetText("")
	case n <= 3:
		p.field.SetText(strings.Join(selected, "、"))
	default:
		p.field.SetText(fmt.Sprintf("%s 等 %d %s", strings.Join(selected[:3], "、"), n, p.unit))
	}
}

func (p *multiPicker) showDialog() {
	// 勾选框显示展示文字，确定时换回选项值
	byLabel := map[string]string{}
	for _, o := range p.options {
		byLabel[p.label(o)] = o
	}
	all := p.labels(p.options)
	check := widget.NewCheckGroup(all, nil)
	check.SetSelected(p.labels(p.selected))

	tools := container.NewHBox(
		widget.NewButton("全选", func() { check.SetSelected(append([]string(nil), all...)) }),
		widget.NewButton("全不选", func() { check.SetSelected(nil) }),
	)
	scroll := container.NewVScroll(check)
	scroll.SetMinSize(fyne.NewSize(420, 420))

	d := dialog.NewCustomConfirm(p.title, "确定", "取消", container.NewBorder(tools, nil, nil, nil, scroll), func(ok bool) {
		if ok {
			values := make([]string, 0, len(check.Selected))
			for _, l := range check.Selected {
				values = append(values, byLabel[l])
			}
			p.SetSelected(values)
		}
	}, view.Window)
	d.Show()
}
