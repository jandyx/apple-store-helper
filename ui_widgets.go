package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// pickerField 外观与下拉框完全一致（高度、内边距、箭头位置），点击时执行 onTap 而不是弹出下拉菜单。
// 门店、型号多选框用它，和「查询频率」「筛选」等下拉框对齐
type pickerField struct {
	widget.Select
	onTap func()
}

func newPickerField(placeHolder string, onTap func()) *pickerField {
	f := &pickerField{onTap: onTap}
	f.PlaceHolder = placeHolder
	f.ExtendBaseWidget(f)
	return f
}

// Tapped 打开自定义的选择框
func (f *pickerField) Tapped(*fyne.PointEvent) {
	if f.Disabled() || f.onTap == nil {
		return
	}
	f.onTap()
}

// TypedKey 获得焦点时按空格、回车或下方向键也能打开选择框
func (f *pickerField) TypedKey(event *fyne.KeyEvent) {
	switch event.Name {
	case fyne.KeySpace, fyne.KeyReturn, fyne.KeyEnter, fyne.KeyDown:
		f.Tapped(nil)
	}
}

// SetText 显示的文字；为空时显示占位文字
func (f *pickerField) SetText(text string) {
	if text == "" {
		f.Options = nil
	} else {
		f.Options = []string{text}
	}
	f.Selected = text
	f.Refresh()
}

// Text 当前显示的文字
func (f *pickerField) Text() string {
	return f.Selected
}

// radioRow 横向单选：每个选项按自身宽度排列、间距一致。
// fyne 的横向 RadioGroup 给每个选项分配同样宽度，短选项（如「日本」）后面会留出大段空白
type radioRow struct {
	options []string
	radios  []*widget.RadioGroup
	box     *fyne.Container
	// Selected 当前选中的选项
	Selected string
	// OnChanged 选中项变化时回调
	OnChanged func(string)
}

func newRadioRow(options []string, changed func(string)) *radioRow {
	r := &radioRow{options: options, OnChanged: changed}
	r.box = container.NewHBox()
	for _, option := range options {
		option := option
		radio := widget.NewRadioGroup([]string{option}, nil)
		radio.OnChanged = func(value string) { r.onRadio(option, radio, value) }
		r.radios = append(r.radios, radio)
		r.box.Add(radio)
	}
	return r
}

func (r *radioRow) onRadio(option string, radio *widget.RadioGroup, value string) {
	if value == "" {
		// 再点一次已选中的项会取消选中，单选框不应出现「都不选」，恢复选中
		if r.Selected == option {
			radio.SetSelected(option)
		}
		return
	}
	r.SetSelected(option)
}

// SetSelected 选中某项并回调 OnChanged；不在选项中的值忽略
func (r *radioRow) SetSelected(value string) {
	valid := false
	for _, o := range r.options {
		if o == value {
			valid = true
		}
	}
	if !valid || value == r.Selected {
		return
	}
	r.Selected = value
	for i, radio := range r.radios {
		if r.options[i] == value {
			radio.SetSelected(value)
		} else if radio.Selected != "" {
			radio.SetSelected("")
		}
	}
	if r.OnChanged != nil {
		r.OnChanged(value)
	}
}

// Widget 横向排列的单选框
func (r *radioRow) Widget() fyne.CanvasObject {
	return r.box
}

// selectionMemory 按地区记住门店和型号的勾选
type selectionMemory struct {
	stores   map[string][]string
	products map[string][]string
}

func newSelectionMemory() *selectionMemory {
	return &selectionMemory{stores: map[string][]string{}, products: map[string][]string{}}
}

func (m *selectionMemory) save(area string, stores, products []string) {
	if area == "" {
		return
	}
	m.stores[area] = append([]string(nil), stores...)
	m.products[area] = append([]string(nil), products...)
}

func (m *selectionMemory) load(area string) (stores, products []string) {
	return m.stores[area], m.products[area]
}
