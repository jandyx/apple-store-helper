package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"apple-store-helper/checkout"
	"apple-store-helper/model"

	"apple-store-helper/services"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func setupPickupContactTest(t *testing.T) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	setupTestWindow(t)
	settingsLoaded = false
	createAutoOrderControls()
}
func contactForUITest() checkout.Contact {
	return checkout.Contact{FirstName: "Alice", LastName: "Example", Email: "alice@example.invalid", Phone: "13000000000", IDLast4: "A1B2"}
}
func fillPickupEditor(e *pickupContactEditor, c checkout.Contact) {
	e.firstName.SetText(c.FirstName)
	e.lastName.SetText(c.LastName)
	e.email.SetText(c.Email)
	e.phone.SetText(c.Phone)
	e.idLast4.SetText(c.IDLast4)
}
func editorForTest(t *testing.T) *pickupContactEditor {
	t.Helper()
	e, err := newPickupContactEditor(currentAutoOrderOptions().ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPickupContactSaveReloadAndPrivateSettings(t *testing.T) {
	setupPickupContactTest(t)
	e := editorForTest(t)
	if !e.saveButton.Disabled() || !e.clearButton.Disabled() {
		t.Fatal("empty editor must not allow save or clear")
	}
	want := contactForUITest()
	fillPickupEditor(e, want)
	if e.saveButton.Disabled() {
		t.Fatal("valid contact cannot be saved")
	}
	autoOrderMode.SetSelectedIndex(2)
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	if currentAutoOrderOptions().Mode != services.AutoOrderOff || services.Listen.GetStatus() != services.Pause {
		t.Fatal("saving must not leave API armed or monitoring running")
	}
	got, err := checkout.LoadContact(e.path)
	if err != nil || got != want {
		t.Fatal("private contact roundtrip failed")
	}
	st, err := os.Stat(e.path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("contact file must be 0600")
	}
	loaded := editorForTest(t)
	if loaded.contact() != want || !loaded.phone.Password || !loaded.idLast4.Password {
		t.Fatal("saved private fields were not restored/masked")
	}
	refreshPickupContactStatus()
	if strings.Contains(pickupContactStatus.Text, want.Phone) || strings.Contains(pickupContactStatus.Text, want.Email) || strings.Contains(pickupContactStatus.Text, want.IDLast4) {
		t.Fatal("main-window status exposed private data")
	}
	if err := services.SaveSettings(services.UserSettings{AutoOrder: currentAutoOrderOptions()}); err != nil {
		t.Fatal(err)
	}
	dir, err := services.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "user_settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{want.FirstName, want.LastName, want.Email, want.Phone, want.IDLast4} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("ordinary settings contain contact data")
		}
	}
}

func TestPickupContactDraftCancelDoesNotSave(t *testing.T) {
	setupPickupContactTest(t)
	e := editorForTest(t)
	fillPickupEditor(e, contactForUITest())
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatal("typing unexpectedly saved private data")
	}
	e.clearMemory()
	for _, entry := range e.entries() {
		if entry.Text != "" {
			t.Fatal("closed editor retains displayed private data")
		}
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatal("closing unexpectedly saved private data")
	}
}

func TestPickupContactInvalidDraftPreservesSavedFile(t *testing.T) {
	setupPickupContactTest(t)
	e := editorForTest(t)
	want := contactForUITest()
	fillPickupEditor(e, want)
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(e.path)
	for _, tc := range []struct {
		name   string
		change func(*checkout.Contact)
	}{
		{"missing surname", func(c *checkout.Contact) { c.LastName = "" }},
		{"email", func(c *checkout.Contact) { c.Email = "not-email" }},
		{"phone", func(c *checkout.Contact) { c.Phone = "123" }},
		{"full ID", func(c *checkout.Contact) { c.IDLast4 = "123456789012345678" }},
		{"masked ID", func(c *checkout.Contact) { c.IDLast4 = "••••" }},
		{"UTF16 name", func(c *checkout.Contact) { c.FirstName = strings.Repeat("🙂", 8) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := want
			tc.change(&draft)
			fillPickupEditor(e, draft)
			if !e.saveButton.Disabled() || e.save() == nil {
				t.Fatal("accepted invalid contact draft")
			}
			after, _ := os.ReadFile(e.path)
			if !bytes.Equal(before, after) {
				t.Fatal("invalid save changed existing contact")
			}
		})
	}
	fillPickupEditor(e, want)
	e.idLast4.SetText("")
	if e.saveButton.Disabled() {
		t.Fatal("ID suffix must remain optional until website requires it")
	}
}

func TestPickupContactBusyAndConfigChangeDoNotWrite(t *testing.T) {
	setupPickupContactTest(t)
	e := editorForTest(t)
	fillPickupEditor(e, contactForUITest())
	e.beforeWrite = func() error { return errors.New("active attempt") }
	if e.save() == nil {
		t.Fatal("saved while checkout busy")
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatal("busy guard allowed private write")
	}
	e.beforeWrite = func() error { t.Fatal("changed config must be checked first"); return nil }
	autoOrderConfig.SetText("/new/order.json")
	if e.save() == nil || e.clear() == nil {
		t.Fatal("editor followed changed config")
	}
}

func TestPickupContactClearAndCancelConfirmation(t *testing.T) {
	setupPickupContactTest(t)
	e := editorForTest(t)
	fillPickupEditor(e, contactForUITest())
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	// Opening the confirmation alone must not delete anything.
	test.Tap(e.clearButton)
	if _, err := os.Stat(e.path); err != nil {
		t.Fatal("clear prompt deleted without confirmation")
	}
	if err := e.clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatal("confirmed clear did not remove profile")
	}
	for _, entry := range e.entries() {
		if entry.Text != "" {
			t.Fatal("clear retained contact fields")
		}
	}
	if !e.saveButton.Disabled() || !e.clearButton.Disabled() {
		t.Fatal("clear should reset editor actions")
	}
}

func TestPickupContactEditorUsesConfiguredRelativeFile(t *testing.T) {
	setupPickupContactTest(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "order.json")
	data := `{"session_file":"session.json","contact_file":"contact.json","state_dir":"state","sku":"MK2Q4CH/A","store_ids":["R639"],"province":"广东","city":"广州","district":"天河区","max_total_cents":1799900,"payment":"WECHAT"}`
	if os.WriteFile(path, []byte(data), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	autoOrderConfig.SetText(path)
	e := editorForTest(t)
	if e.path != filepath.Join(dir, "contact.json") {
		t.Fatal("configured contact path not reused")
	}
	fillPickupEditor(e, contactForUITest())
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := checkout.LoadContact(e.path); err != nil {
		t.Fatal(err)
	}
	defaultPath, _ := checkout.DefaultContactPath()
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Fatal("configured save also modified default profile")
	}
}

func TestPickupContactButtonAvailableInReminderMode(t *testing.T) {
	setupPickupContactTest(t)
	controls := createAutoOrderControls()
	// The editor entry point is visible without selecting an API mode or JSON path.
	var found bool
	for _, o := range controls.(*fyne.Container).Objects {
		if b, ok := o.(*widget.Button); ok && b.Text == "填写／编辑提货资料" {
			found = b.Visible()
		}
	}
	if !found || currentAutoOrderOptions().Mode != services.AutoOrderOff {
		t.Fatal("contact editor should be available while default remains off")
	}
}

func TestPickupContactSavedDefaultUsedByAPIPreflight(t *testing.T) {
	setupPickupContactTest(t)
	e := editorForTest(t)
	fillPickupEditor(e, contactForUITest())
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0700)
	configPath := filepath.Join(dir, "order.json")
	config := `{"session_file":"session.json","state_dir":"state","sku":"MK2Q4CH/A","store_ids":["R639"],"province":"广东","city":"广州","district":"天河区","max_total_cents":1799900,"payment":"WECHAT"}`
	session := `{"user_agent":"fixture","cookies":[{"name":"session","value":"fixture-not-a-real-session","domain":"www.apple.com.cn","path":"/","secure":true}]}`
	if os.WriteFile(configPath, []byte(config), 0600) != nil || os.WriteFile(filepath.Join(dir, "session.json"), []byte(session), 0600) != nil {
		t.Fatal("fixture setup failed")
	}
	autoOrderConfig.SetText(configPath)
	autoOrderMode.SetSelectedIndex(1)
	old := services.Listen.GetListenItems()
	t.Cleanup(func() {
		services.Listen.ConfigureAutoOrder(services.AutoOrderOptions{Mode: services.AutoOrderOff})
		services.Listen.SetListenItems(old, model.Areas[0])
	})
	hit := services.ListenItem{Area: model.Areas[0], Store: model.Store{StoreNumber: "R639"}, Product: model.Product{Code: "MK2Q4CH/A", Type: "iphoneduo"}, Status: services.StatusInStock}
	services.Listen.SetListenItems(map[string]services.ListenItem{"R639.MK2Q4CH/A": hit}, model.Areas[0])
	// Configuration only: never start polling or dispatch a hit to the network.
	if err := services.Listen.ConfigureAutoOrder(services.AutoOrderOptions{ConfigPath: configPath, Mode: checkout.ModeReview}); err != nil {
		t.Fatal("API preflight did not consume app-saved default contact", err)
	}
	if !services.Listen.AutoOrderHandles([]services.ListenItem{hit}) {
		t.Fatal("test preflight did not arm its matching policy")
	}
	edit := editorForTest(t)
	edit.firstName.SetText("Updated")
	if err := edit.save(); err != nil {
		t.Fatal(err)
	}
	if services.Listen.AutoOrderHandles([]services.ListenItem{hit}) || currentAutoOrderOptions().Mode != services.AutoOrderOff {
		t.Fatal("saving changed contact left old API policy armed")
	}
}

func TestPickupContactRechecksConfiguredFileBeforeWrite(t *testing.T) {
	setupPickupContactTest(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "order.json")
	data := `{"session_file":"session.json","contact_file":"old.json","state_dir":"state","sku":"MK2Q4CH/A","store_ids":["R639"],"province":"广东","city":"广州","district":"天河区","max_total_cents":1799900,"payment":"WECHAT"}`
	if os.WriteFile(path, []byte(data), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	autoOrderConfig.SetText(path)
	e := editorForTest(t)
	fillPickupEditor(e, contactForUITest())
	e.beforeWrite = func() error { t.Fatal("config conflict must be caught before disarming/writing"); return nil }
	if os.WriteFile(path, []byte(strings.ReplaceAll(data, "old.json", "new.json")), 0600) != nil {
		t.Fatal("fixture update failed")
	}
	if e.save() == nil {
		t.Fatal("saved to stale configured location")
	}
	for _, name := range []string{"old.json", "new.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal("config conflict wrote contact data")
		}
	}
}

func TestPickupContactActionsStayOutsideScrollingFields(t *testing.T) {
	setupPickupContactTest(t)
	e := editorForTest(t)
	content := pickupContactContent(e)
	var contains func(fyne.CanvasObject, fyne.CanvasObject) bool
	contains = func(root, target fyne.CanvasObject) bool {
		if root == target {
			return true
		}
		if box, ok := root.(*fyne.Container); ok {
			for _, o := range box.Objects {
				if contains(o, target) {
					return true
				}
			}
		}
		return false
	}
	scrolls := 0
	footerActions := false
	for _, o := range content.Objects {
		if scroll, ok := o.(*container.Scroll); ok {
			scrolls++
			if contains(scroll.Content, e.saveButton) || contains(scroll.Content, e.clearButton) {
				t.Fatal("private form scrolling hides save/clear actions")
			}
		} else if contains(o, e.saveButton) && contains(o, e.clearButton) && contains(o, e.status) {
			footerActions = true
		}
	}
	if scrolls != 1 || !footerActions {
		t.Fatal("expected one scrolling form and a fixed status/action footer")
	}
}
