package checkout

import "testing"

func TestPhaseWithNativeUtilityModules(t *testing.T) {
	for _, stage := range []string{"fulfillment", "pickupContact", "billing", "review", "routeOut", "poi"} {
		t.Run(stage, func(t *testing.T) {
			m := Model{"checkout": Model{"c": []any{"companionBar", stage, "locationConsent", "session"}}}
			if got := phase(m); got != stage {
				t.Fatalf("phase=%q want %q", got, stage)
			}
		})
	}
}
func TestPhaseRejectsAmbiguousOrUnknownChildren(t *testing.T) {
	for _, children := range [][]any{
		{}, {"companionBar", "session"}, {"fulfillment", "review"}, {"fulfillment", "fulfillment"}, {"fulfillment", "unexpected"}, {"fulfillment", 42}, {"unexpected"},
	} {
		m := Model{"checkout": Model{"c": children}}
		if got := phase(m); got != "unknown" {
			t.Fatalf("children=%v phase=%q", children, got)
		}
	}
}
func TestPhaseAuthenticationTakesPrecedence(t *testing.T) {
	m := Model{"signIn": Model{}, "checkout": Model{"c": []any{"fulfillment"}}}
	if got := phase(m); got != "authentication" {
		t.Fatal(got)
	}
}
