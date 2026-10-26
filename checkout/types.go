// Package checkout implements the request-only China store pickup checkout flow.
// It never polls inventory, drives browser input, or processes a payment.
package checkout

import (
	"context"
	"net/http"
)

type Model map[string]any

const (
	ModeReview = "review"
	ModeSubmit = "submit"
)

// Config is loaded explicitly; merely having a file on disk never arms checkout.
// SessionFile and ContactFile are private local files, not browser profile paths.
type Config struct {
	SessionFile   string   `json:"session_file"`
	ContactFile   string   `json:"contact_file,omitempty"`
	StateDir      string   `json:"state_dir"`
	SKU           string   `json:"sku"`
	StoreIDs      []string `json:"store_ids"`
	Province      string   `json:"province"`
	City          string   `json:"city"`
	District      string   `json:"district"`
	MaxTotalCents int64    `json:"max_total_cents"`
	Payment       string   `json:"payment"`
}

type Contact struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	IDLast4   string `json:"id_last4,omitempty"`
}

// Target is a snapshot of a single inventory hit, never a bag of all candidates.
type Target struct {
	SKU           string
	ProductName   string
	StoreID       string
	StoreName     string
	Province      string
	City          string
	District      string
	MaxTotalCents int64
	Payment       string
	Contact       Contact
	PickupSlotID  string
}

// Result is deliberately small: no model, cookies, contact data or payment QR.
type Result struct {
	State       string `json:"state"`
	Stage       string `json:"stage,omitempty"`
	SKU         string `json:"sku"`
	StoreID     string `json:"store_id"`
	TotalCents  int64  `json:"total_cents,omitempty"`
	OrderNumber string `json:"order_number,omitempty"`
}

type Executor interface {
	Run(context.Context, Target) (Result, error)
}

// Cookie represents only a user's explicitly exported Apple store cookie.
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"http_only"`
	Expires  int64  `json:"expires,omitempty"`
}

type Session struct {
	UserAgent string   `json:"user_agent"`
	Cookies   []Cookie `json:"cookies"`
}

// ClientFactory exists to isolate production HTTP from local contract fixtures.
// Production construction must enforce Apple HTTPS origin checks independently.
type ClientFactory func(Session) (*http.Client, error)
