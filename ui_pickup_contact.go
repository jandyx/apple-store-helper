package main

import (
	"errors"
	"net/mail"
	"os"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf16"

	"apple-store-helper/checkout"
	"apple-store-helper/services"
	"apple-store-helper/view"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var pickupContactStatus *widget.Label

// The editor keeps personal data out of UserSettings and main-window summaries.
// Merely typing, cancelling, or restoring settings never writes a contact file.
type pickupContactEditor struct {
	path, configPath                           string
	firstName, lastName, email, phone, idLast4 *widget.Entry
	status                                     *widget.Label
	saveButton, clearButton                    *widget.Button
	beforeWrite                                func() error
	afterWrite                                 func()
}

func pickupContactFile(configPath string) (string, error) {
	if strings.TrimSpace(configPath) == "" {
		return checkout.DefaultContactPath()
	}
	config, err := checkout.LoadConfig(configPath)
	if err != nil {
		return "", errors.New("无法读取所选 API 配置，提货资料未修改；可清空配置路径后使用应用默认位置")
	}
	return config.ContactFile, nil
}

func refreshPickupContactStatus() {
	if pickupContactStatus == nil {
		return
	}
	if runtime.GOOS == "windows" {
		pickupContactStatus.SetText("提货资料：暂不支持 Windows 私有存储")
		return
	}
	path, err := pickupContactFile(currentAutoOrderOptions().ConfigPath)
	if err != nil {
		pickupContactStatus.SetText("提货资料：配置路径无效")
		return
	}
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		pickupContactStatus.SetText("提货资料：尚未保存")
		return
	}
	if err != nil || !st.Mode().IsRegular() {
		pickupContactStatus.SetText("提货资料：无法读取")
		return
	}
	if _, err = checkout.LoadContact(path); err != nil {
		pickupContactStatus.SetText("提货资料：请打开检查")
		return
	}
	pickupContactStatus.SetText("提货资料：已保存到本机")
}

func validatePickupName(value string, max int) error {
	value = strings.TrimSpace(value)
	if value == "" || len(utf16.Encode([]rune(value))) > max || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return errors.New("请填写与取货证件一致的姓名，且不要超过页面长度限制")
	}
	return nil
}
func validatePickupEmail(value string) error {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("请填写有效的联系邮箱")
	}
	return nil
}
func validatePickupPhone(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 11 || len(value) > 12 {
		return errors.New("请填写 11 至 12 位数字的手机号")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return errors.New("手机号只能包含数字")
		}
	}
	return nil
}
func validatePickupIDLast4(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) != 4 {
		return errors.New("只填写证件号码后 4 位，不要输入完整证件号")
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return errors.New("证件后 4 位只能包含字母或数字，不能使用掩码")
		}
	}
	return nil
}

func newPickupContactEditor(configPath string) (*pickupContactEditor, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("提货资料私有保存暂仅支持 macOS/Linux；Windows 原生文件保护尚未验收")
	}
	path, err := pickupContactFile(configPath)
	if err != nil {
		return nil, err
	}
	e := &pickupContactEditor{path: path, configPath: configPath, beforeWrite: disarmForContactEdit, status: widget.NewLabel("尚未保存")}
	e.firstName = widget.NewEntry()
	e.lastName = widget.NewEntry()
	e.email = widget.NewEntry()
	e.phone = widget.NewPasswordEntry()
	e.idLast4 = widget.NewPasswordEntry()
	e.firstName.SetPlaceHolder("名字，与取货证件一致")
	e.lastName.SetPlaceHolder("姓氏，与取货证件一致")
	e.email.SetPlaceHolder("接收取货通知的邮箱")
	e.phone.SetPlaceHolder("手机号；可点眼睛查看")
	e.idLast4.SetPlaceHolder("选填：仅后 4 位，官网要求时使用")
	e.firstName.Validator = func(s string) error { return validatePickupName(s, 14) }
	e.lastName.Validator = func(s string) error { return validatePickupName(s, 20) }
	e.email.Validator = validatePickupEmail
	e.phone.Validator = validatePickupPhone
	e.idLast4.Validator = validatePickupIDLast4
	e.saveButton = widget.NewButton("保存到本机", func() {
		if err := e.save(); err != nil {
			dialog.ShowError(err, view.Window)
		}
	})
	e.saveButton.Importance = widget.HighImportance
	e.clearButton = widget.NewButton("清除已保存资料", func() {
		dialog.ShowConfirm("清除本机提货资料", "只清除当前资料文件，不修改 Apple 账号或订单。清除会暂停监听并解除 API 武装。", func(confirmed bool) {
			if confirmed {
				if err := e.clear(); err != nil {
					dialog.ShowError(err, view.Window)
				}
			}
		}, view.Window)
	})
	for _, field := range e.entries() {
		field.OnChanged = func(string) { e.updateSaveState() }
	}
	e.load()
	e.updateSaveState()
	return e, nil
}
func (e *pickupContactEditor) entries() []*widget.Entry {
	return []*widget.Entry{e.lastName, e.firstName, e.email, e.phone, e.idLast4}
}
func (e *pickupContactEditor) contact() checkout.Contact {
	return checkout.Contact{FirstName: strings.TrimSpace(e.firstName.Text), LastName: strings.TrimSpace(e.lastName.Text), Email: strings.TrimSpace(e.email.Text), Phone: strings.TrimSpace(e.phone.Text), IDLast4: strings.TrimSpace(e.idLast4.Text)}
}
func (e *pickupContactEditor) validate() error {
	for _, field := range e.entries() {
		if err := field.Validator(field.Text); err != nil {
			return err
		}
	}
	if err := checkout.ValidateContact(e.contact()); err != nil {
		return errors.New("请检查提货资料是否完整、有效")
	}
	return nil
}
func (e *pickupContactEditor) updateSaveState() {
	if e.saveButton == nil {
		return
	}
	if e.validate() != nil {
		e.saveButton.Disable()
	} else {
		e.saveButton.Enable()
	}
}
func (e *pickupContactEditor) load() {
	st, err := os.Lstat(e.path)
	if os.IsNotExist(err) {
		e.status.SetText("尚未保存提货资料")
		e.clearButton.Disable()
		return
	}
	if err != nil || !st.Mode().IsRegular() {
		e.status.SetText("无法读取资料：文件类型或访问权限不安全")
		e.clearButton.Disable()
		return
	}
	e.clearButton.Enable()
	c, err := checkout.LoadContact(e.path)
	if err != nil {
		e.status.SetText("无法读取私有资料，请检查文件权限或重新填写")
		return
	}
	e.firstName.SetText(c.FirstName)
	e.lastName.SetText(c.LastName)
	e.email.SetText(c.Email)
	e.phone.SetText(c.Phone)
	e.idLast4.SetText(c.IDLast4)
	e.status.SetText("已从本机私有文件载入；保存不会启动下单")
}
func (e *pickupContactEditor) prepareWrite() error {
	if currentAutoOrderOptions().ConfigPath != e.configPath {
		return errors.New("API 配置已改变，请关闭后重新打开提货资料，避免写错文件")
	}
	path, err := pickupContactFile(e.configPath)
	if err != nil || path != e.path {
		return errors.New("API 配置中的资料位置已改变，请关闭后重新打开，避免写错文件")
	}
	if e.beforeWrite != nil {
		return e.beforeWrite()
	}
	return nil
}
func (e *pickupContactEditor) save() error {
	if err := e.validate(); err != nil {
		return err
	}
	if err := e.prepareWrite(); err != nil {
		return err
	}
	if err := checkout.SaveContact(e.path, e.contact()); err != nil {
		return errors.New("无法确认提货资料已安全保存；请检查私有目录权限和磁盘，再重新打开资料确认")
	}
	e.status.SetText("已保存到本机；监听已暂停，API 模式已关闭")
	e.clearButton.Enable()
	if e.afterWrite != nil {
		e.afterWrite()
	}
	return nil
}
func (e *pickupContactEditor) clear() error {
	if err := e.prepareWrite(); err != nil {
		return err
	}
	if err := checkout.DeleteContact(e.path); err != nil {
		return errors.New("无法确认资料清除结果；请检查文件及目录权限，再重新打开资料确认")
	}
	e.clearMemory()
	e.clearButton.Disable()
	e.status.SetText("已清除本机资料；监听已暂停，API 模式已关闭")
	if e.afterWrite != nil {
		e.afterWrite()
	}
	return nil
}
func (e *pickupContactEditor) clearMemory() {
	for _, field := range e.entries() {
		field.SetText("")
	}
}

func disarmForContactEdit() error {
	// installAutoOrder retires an idle arm under the same mutex as tryStart and
	// refuses a running attempt. Never rewrite the expected contact mid-request.
	if err := services.Listen.ConfigureAutoOrder(services.AutoOrderOptions{Mode: services.AutoOrderOff}); err != nil {
		return errors.New("API 尝试正在执行，请停止并等待结束后再保存或清除资料")
	}
	services.Listen.SetStatus(services.Pause)
	if autoOrderMode != nil {
		autoOrderMode.SetSelectedIndex(0)
	}
	return nil
}
func showPickupContactEditor() {
	e, err := newPickupContactEditor(currentAutoOrderOptions().ConfigPath)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
	e.afterWrite = refreshPickupContactStatus
	d := dialog.NewCustom("提货资料（本机保存）", "关闭", pickupContactContent(e), view.Window)
	d.SetOnClosed(e.clearMemory)
	d.Resize(fyne.NewSize(620, 530))
	d.Show()
}

func pickupContactContent(e *pickupContactEditor) *fyne.Container {
	form := widget.NewForm(widget.NewFormItem("姓氏", e.lastName), widget.NewFormItem("名字", e.firstName), widget.NewFormItem("联系邮箱", e.email), widget.NewFormItem("手机号", e.phone), widget.NewFormItem("证件后 4 位（选填）", e.idLast4))
	help := widget.NewLabel("保存到本机私有文件（未加密），不写入普通设置或日志。只有明确启用 API 流程时才使用；保存不会启动监听或下单。证件只填写后 4 位，官网要求时才发送；当前填此项仅可演练，不可自动提交。")
	help.Wrapping = fyne.TextWrapWord
	policy := widget.NewLabel("保存或清除会暂停监听并解除 API 武装；正在执行的尝试结束前不能修改。")
	policy.Wrapping = fyne.TextWrapWord
	location := widget.NewLabel("保存位置：" + e.path)
	location.Wrapping = fyne.TextWrapBreak
	fields := container.NewVScroll(container.NewVBox(help, form, policy, location))
	foot := container.NewVBox(e.status, container.NewHBox(e.saveButton, e.clearButton))
	return container.NewBorder(nil, foot, nil, nil, fields)
}
