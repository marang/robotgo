package robotgo

import "testing"

func TestErrorReturningInputAPIsRejectInvalidArgumentTypes(t *testing.T) {
	tests := []struct {
		name string
		call func() error
	}{
		{name: "click button", call: func() error { return ClickE(1) }},
		{name: "click flag", call: func() error { return ClickE("left", "double") }},
		{name: "toggle button", call: func() error { return Toggle(false) }},
		{name: "toggle state", call: func() error { return Toggle("left", true) }},
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
