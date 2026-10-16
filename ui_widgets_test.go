package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/model"
	"apple-store-helper/services"
	"apple-store-helper/view"
)

// setupTestWindow 用 fyne 的测试驱动创建应用和窗口，弹窗类控件依赖 view.Window
func setupTestWindow(t *testing.T) fyne.Window {
	t.Helper()
	app := test.NewApp()
	view.App = app
	view.Window = test.NewWindow(nil)
	view.Window.Resize(fyne.NewSize(1000, 800))
	t.Cleanup(func() {
		view.Window.Close()
		app.Quit()
	})
	return view.Window
}

func TestPickerFieldTapRunsCallback(t *testing.T) {
	setupTestWindow(t)
	tapped := 0
	f := newPickerField("请选择", func() { tapped++ })

	test.Tap(f)
	f.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	f.TypedKey(&fyne.KeyEvent{Name: fyne.KeyA})
	if tapped != 2 {
		t.Fatalf("tap and space should open picker, got %d", tapped)
	}

	f.Disable()
	test.Tap(f)
	if tapped != 2 {
		t.Fatal("disabled field must not open picker")
	}
}

func TestPickerFieldText(t *testing.T) {
	setupTestWindow(t)
	f := newPickerField("请选择门店", nil)
	if f.Text() != "" || f.PlaceHolder != "请选择门店" {
		t.Fatalf("initial text %q placeholder %q", f.Text(), f.PlaceHolder)
	}
	f.SetText("广东-天环广场")
	if f.Text() != "广东-天环广场" {
		t.Fatalf("text = %q", f.Text())
	}
	f.SetText("")
	if f.Text() != "" || len(f.Options) != 0 {
		t.Fatalf("clearing should show placeholder, got %q %v", f.Text(), f.Options)
	}
}

// 多选框与查询频率、筛选等下拉框高度一致，箭头才能对齐
func TestPickerAlignsWithDropdowns(t *testing.T) {
	w := setupTestWindow(t)
	picker := newMultiPicker("选择门店", "请选择自提门店（可多选）", "家", []string{"A"})
	interval := widget.NewSelectEntry([]string{"3 秒"})
	filter := widget.NewSelect([]string{"全部"}, nil)
	w.SetContent(container.NewVBox(picker.Widget(), interval, filter))

	ph := picker.Widget().MinSize().Height
	if fh := filter.MinSize().Height; ph != fh {
		t.Errorf("picker height %v != select height %v", ph, fh)
	}
	if ih := interval.MinSize().Height; abs(ph-ih) > 4 {
		t.Errorf("picker height %v too different from select entry height %v", ph, ih)
	}
	// 宽度铺满后右边缘一致，箭头位置由右边缘决定
	if picker.Widget().Size().Width != interval.Size().Width {
		t.Errorf("widths differ: %v vs %v", picker.Widget().Size().Width, interval.Size().Width)
	}
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func TestMultiPickerDisplayKeepsRawValues(t *testing.T) {
	setupTestWindow(t)
	options := []string{"iphoneduo - 夜空色 - 256gb", "iphoneduo - 夜空色 - 2tb", "iphoneduo - 星光白色 - 512gb", "iphoneduo - 星光白色 - 1tb"}
	p := newMultiPicker("选择型号", "请选择型号", "个", options)
	p.display = services.TitleName

	p.SetSelected([]string{options[0], "不存在的选项", " " + options[1] + " "})
	if got := p.Selected(); len(got) != 2 || got[0] != options[0] || got[1] != options[1] {
		t.Fatalf("selected should keep raw values and drop unknown ones: %q", got)
	}
	if got := p.field.Text(); got != "iPhone Duo 夜空色 256GB、iPhone Duo 夜空色 2TB" {
		t.Fatalf("field text = %q", got)
	}

	p.SetSelected(options)
	if got := p.field.Text(); got != "iPhone Duo 夜空色 256GB、iPhone Duo 夜空色 2TB、iPhone Duo 星光白色 512GB 等 4 个" {
		t.Fatalf("summary text = %q", got)
	}

	p.SetOptions([]string{"x"})
	if len(p.Selected()) != 0 || p.field.Text() != "" {
		t.Fatal("changing options should clear selection")
	}
}

// tapRadio 按坐标点击单选框里的选项，模拟真实操作
func tapRadio(w fyne.Window, radio *widget.RadioGroup) {
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(radio)
	test.TapCanvas(w.Canvas(), pos.Add(fyne.NewPos(10, radio.Size().Height/2)))
}

func TestRadioRowSingleSelection(t *testing.T) {
	w := setupTestWindow(t)
	var changes []string
	r := newRadioRow([]string{"中国大陆", "中国香港", "日本"}, func(v string) { changes = append(changes, v) })
	w.SetContent(r.Widget())

	r.SetSelected("中国大陆")
	tapRadio(w, r.radios[1])
	if r.Selected != "中国香港" || r.radios[0].Selected != "" || r.radios[1].Selected != "中国香港" {
		t.Fatalf("tap should switch selection: selected=%q radios=%q,%q", r.Selected, r.radios[0].Selected, r.radios[1].Selected)
	}

	// 再点一次已选中的项，不应变成都不选
	tapRadio(w, r.radios[1])
	if r.Selected != "中国香港" || r.radios[1].Selected != "中国香港" {
		t.Fatalf("re-tapping selected option must keep it selected, got %q", r.radios[1].Selected)
	}

	r.SetSelected("不存在")
	r.SetSelected("中国香港")
	if len(changes) != 2 || changes[0] != "中国大陆" || changes[1] != "中国香港" {
		t.Fatalf("OnChanged should fire once per real change, got %q", changes)
	}
}

// 横向单选框按自身宽度排列：短选项占的宽度应小于长选项
func TestRadioRowUsesNaturalWidths(t *testing.T) {
	w := setupTestWindow(t)
	r := newRadioRow([]string{"日本", "Australia"}, nil)
	w.SetContent(r.Widget())
	short, long := r.radios[0].Size().Width, r.radios[1].Size().Width
	if short >= long {
		t.Fatalf("short option width %v should be less than long option width %v", short, long)
	}
}

func TestSelectionMemory(t *testing.T) {
	m := newSelectionMemory()
	m.save("", []string{"x"}, nil)
	m.save("中国大陆", []string{"天环"}, []string{"duo"})
	stores, products := m.load("中国大陆")
	if len(stores) != 1 || stores[0] != "天环" || len(products) != 1 {
		t.Fatalf("load = %q %q", stores, products)
	}
	if s, _ := m.load("中国香港"); s != nil {
		t.Fatalf("unknown area should be empty, got %q", s)
	}
}

func TestListenListFilterAndHeader(t *testing.T) {
	w := setupTestWindow(t)
	services.Listen.SetListenItems(map[string]services.ListenItem{
		"R1.A": {Status: services.StatusFailed + "：HTTP 541"},
		"R1.B": {Status: services.StatusOutStock},
		"R2.A": {Status: services.StatusOutStock},
	}, model0())
	t.Cleanup(services.Listen.Clean)

	l := newListenList()
	w.SetContent(container.NewBorder(l.Header(), nil, nil, nil, l.Widget()))
	l.Refresh()
	if l.list.Length() != 3 || l.hint.Text != "共 3 项" || l.empty.Visible() {
		t.Fatalf("all: len=%d hint=%q empty=%v", l.list.Length(), l.hint.Text, l.empty.Visible())
	}

	l.choose.SetSelected(services.StatusOutStock)
	if l.list.Length() != 2 || l.hint.Text != "2 / 3 项" {
		t.Fatalf("filter out of stock: len=%d hint=%q", l.list.Length(), l.hint.Text)
	}

	l.choose.SetSelected(services.StatusInStock)
	if l.list.Length() != 0 || !l.empty.Visible() {
		t.Fatalf("empty filter should show hint, len=%d", l.list.Length())
	}
}

func TestWarningBannerVisibility(t *testing.T) {
	w := setupTestWindow(t)
	b := newWarningBanner()
	w.SetContent(container.NewVBox(b.box))

	b.Update(services.RoundStats{AllFailed: true, TopReason: "HTTP 541"})
	if !b.box.Visible() || b.label.Text == "" {
		t.Fatal("banner should show when all failed")
	}
	b.Update(services.RoundStats{})
	if b.box.Visible() {
		t.Fatal("banner should hide after recovery")
	}
}

func model0() model.Area { return model.Areas[0] }
