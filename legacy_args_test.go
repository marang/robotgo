package robotgo

import "testing"

func TestErrorReturningInputAPIsRejectInvalidArgumentTypes(t *testing.T) {
	tests := []struct {
		name string
		call func() error
	}{
		{name: "click button", call: func() error { return ClickE(1) }},
		{name: "click flag", call: func() error { return ClickE("left", "double") }},
		{name: "click button name", call: func() error { return ClickE("typo") }},
		{name: "click arity", call: func() error { return ClickE("left", false, "extra") }},
		{name: "toggle button", call: func() error { return Toggle(false) }},
		{name: "toggle state", call: func() error { return Toggle("left", true) }},
		{name: "toggle button name", call: func() error { return Toggle("typo") }},
		{name: "toggle arity", call: func() error { return Toggle("left", "down", "extra") }},
		{name: "key modifier", call: func() error { return KeyTap("a", struct{}{}) }},
		{name: "key toggle modifier", call: func() error { return KeyToggle("a", struct{}{}) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil {
				t.Fatal("invalid argument type unexpectedly accepted")
			}
		})
	}
}

func TestParseToggleArgumentsRejectsUnknownState(t *testing.T) {
	for _, state := range []string{"", "pressed", "UP"} {
		if _, _, err := parseToggleArguments([]interface{}{"left", state}); err == nil {
			t.Fatalf("parseToggleArguments accepted state %q", state)
		}
	}
}
