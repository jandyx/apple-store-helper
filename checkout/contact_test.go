package checkout

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func nativeContactFixture(t *testing.T) Model {
	t.Helper()
	return fixture(t, `{"checkout":{"pickupContact":{"c":["selfPickupContact"],"selfPickupContact":{"c":["selfContact","nationalIdSelf"],"selfContact":{"c":["address"],"address":{"d":{"firstName":"old","lastName":"old","emailAddress":"old","fullDaytimePhone":"old"},"b":{"firstName":{},"lastName":{},"emailAddress":{},"fullDaytimePhone":{}}}},"nationalIdSelf":{"d":{"enabled":true,"nationalIdSelf":""},"b":{"nationalIdSelf":{"key":"d.nationalIdSelf","masked":true}}}}}}}`)
}

func TestFillNativeContact(t *testing.T) {
	_, target := testConfig(t)
	target.Contact.IDLast4 = "A123"
	m := nativeContactFixture(t)
	if err := fillContact(m, target.Contact); err != nil {
		t.Fatal(err)
	}
	d, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.selfContact.address.d")
	if d["firstName"] != target.Contact.FirstName || d["lastName"] != target.Contact.LastName || d["emailAddress"] != target.Contact.Email || d["fullDaytimePhone"] != target.Contact.Phone {
		t.Fatal("native contact fields not filled")
	}
	id, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.nationalIdSelf.d")
	if id["nationalIdSelf"] != target.Contact.IDLast4 {
		t.Fatal("identification suffix not filled")
	}
}

func TestFillContactFailuresAreAtomic(t *testing.T) {
	cases := []struct {
		name   string
		change func(Model, *Contact)
	}{
		{"missing phone", func(m Model, c *Contact) {
			d, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.selfContact.address.d")
			delete(d, "fullDaytimePhone")
		}},
		{"missing phone binding", func(m Model, c *Contact) {
			b, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.selfContact.address.b")
			delete(b, "fullDaytimePhone")
		}},
		{"duplicate phone", func(m Model, c *Contact) {
			a, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.selfContact.address")
			a["d"].(map[string]any)["mobilePhone"] = "old"
			a["b"].(map[string]any)["mobilePhone"] = Model{}
		}},
		{"missing suffix", func(m Model, c *Contact) { c.IDLast4 = "" }},
		{"masked suffix", func(m Model, c *Contact) { c.IDLast4 = "••••" }},
		{"full identification", func(m Model, c *Contact) { c.IDLast4 = "123456789012345678" }},
		{"invalid suffix", func(m Model, c *Contact) { c.IDLast4 = "12|4" }},
		{"unknown enabled", func(m Model, c *Contact) {
			d, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.nationalIdSelf.d")
			d["enabled"] = "true"
		}},
		{"missing ID binding", func(m Model, c *Contact) {
			n, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.nationalIdSelf")
			delete(n, "b")
		}},
		{"duplicate ID binding", func(m Model, c *Contact) {
			b, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.selfContact.address.b")
			b["nationalIdSelf"] = Model{"key": "d.nationalIdSelf"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, target := testConfig(t)
			c := target.Contact
			c.IDLast4 = "a123"
			m := nativeContactFixture(t)
			tc.change(m, &c)
			before, _ := json.Marshal(m)
			if fillContact(m, c) == nil {
				t.Fatal("accepted unsupported contact")
			}
			after, _ := json.Marshal(m)
			if string(before) != string(after) {
				t.Fatal("failed fill mutated model")
			}
		})
	}
}

func TestDisabledIdentificationIsNotFilled(t *testing.T) {
	for _, suffix := range []string{"", "A123"} {
		_, target := testConfig(t)
		target.Contact.IDLast4 = suffix
		m := nativeContactFixture(t)
		d, _ := rawNode(m, "checkout.pickupContact.selfPickupContact.nationalIdSelf.d")
		d["enabled"] = false
		d["nationalIdSelf"] = "unchanged"
		if err := fillContact(m, target.Contact); err != nil {
			t.Fatal(err)
		}
		if d["nationalIdSelf"] != "unchanged" {
			t.Fatal("disabled identification mutated")
		}
	}
}

func TestLoadContactIdentificationSuffix(t *testing.T) {
	for _, suffix := range []string{"", "aB09", "123", "12345", "１２３４", "••••", "12|4", "12 4", "123456789012345678"} {
		t.Run(suffix, func(t *testing.T) {
			cfg, target := testConfig(t)
			target.Contact.IDLast4 = suffix
			privateWrite(t, cfg.ContactFile, target.Contact)
			_, err := LoadContact(cfg.ContactFile)
			valid := suffix == "" || suffix == "aB09"
			if (err == nil) != valid {
				t.Fatal("incorrect suffix validation")
			}
		})
	}
}

func TestSubmitWithIdentificationRefusesBeforeNetwork(t *testing.T) {
	cfg, target := testConfig(t)
	target.Contact.IDLast4 = "A123"
	privateWrite(t, cfg.ContactFile, target.Contact)
	r, err := NewRunner(cfg, ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	r.newClient = func(Session) (*http.Client, error) { t.Fatal("network client must not be created"); return nil, nil }
	result, err := r.Run(context.Background(), target)
	if err == nil || result.Stage != "configuration" {
		t.Fatal("submit with unverified identification review was not rejected")
	}
}
