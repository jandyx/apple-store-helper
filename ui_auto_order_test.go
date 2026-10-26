package main

import (
	"testing"

	"apple-store-helper/services"
)

func TestAutoOrderSettingsRestoreNeverRearms(t *testing.T) {
	setupTestWindow(t)
	settingsLoaded = false
	createAutoOrderControls()
	if got := currentAutoOrderOptions(); got.Mode != services.AutoOrderOff || autoOrderConfig.Visible() {
		t.Fatal("default must remain reminder-only", got)
	}
	autoOrderMode.SetSelectedIndex(1)
	if !autoOrderConfig.Visible() || currentAutoOrderOptions().Mode != "review" {
		t.Fatal("review mode must explicitly expose config")
	}
	loadAutoOrderOptions(services.AutoOrderOptions{ConfigPath: "/private/order.json", Mode: "submit"})
	if got := currentAutoOrderOptions(); got.Mode != services.AutoOrderOff || got.ConfigPath != "/private/order.json" {
		t.Fatal("settings restore rearmed checkout", got)
	}
}
