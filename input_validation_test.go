package robotgo

import (
	"errors"
	"math"
	"reflect"
	"runtime"
	"testing"
)

var _ func() error = CloseMainDisplayE

func TestKeyArgumentParsingIsBuildIndependent(t *testing.T) {
	pid, down, modifiers, err := parseKeyArguments(
		[]interface{}{[]string{"CTRL"}, 42, "shift"}, false,
	)
	if err != nil || pid != 42 || !down || !reflect.DeepEqual(modifiers, []string{"CTRL", "shift"}) {
		t.Fatalf("parseKeyArguments(tap) = (%d,%t,%v,%v)", pid, down, modifiers, err)
	}
	pid, down, modifiers, err = parseKeyArguments(
		[]interface{}{[]string{"UP", "alt"}, 7}, true,
	)
	if err != nil || pid != 7 || down || !reflect.DeepEqual(modifiers, []string{"alt"}) {
		t.Fatalf("parseKeyArguments(toggle) = (%d,%t,%v,%v)", pid, down, modifiers, err)
	}
	for _, args := range [][]interface{}{
		{1, 2},
		{struct{}{}},
	} {
		if _, _, _, err := parseKeyArguments(args, false); err == nil {
			t.Fatalf("parseKeyArguments(%v) unexpectedly succeeded", args)
		}
	}
	if got, err := normalizeKeyModifiers([]string{"CTRL", "right_shift", "NONE"}); err != nil ||
		!reflect.DeepEqual(got, []string{"ctrl", "right_shift", "none"}) {
		t.Fatalf("normalizeKeyModifiers(valid) = (%v,%v)", got, err)
	}
	if _, err := normalizeKeyModifiers([]string{"not-a-modifier"}); err == nil {
		t.Fatal("normalizeKeyModifiers accepted an unknown modifier")
	}
}

func TestUppercaseShiftDetectionOnlyAppliesToSingleRuneKeys(t *testing.T) {
	for _, test := range []struct {
		key  string
		want bool
	}{
		{key: "A", want: true},
		{key: "Ä", want: true},
		{key: "a"},
		{key: "Enter"},
		{key: ""},
		{key: string([]byte{0xff})},
	} {
		if got := uppercaseSingleRuneKey(test.key); got != test.want {
			t.Fatalf("uppercaseSingleRuneKey(%q) = %t, want %t", test.key, got, test.want)
		}
	}
}

func TestParseSmoothMoveArgumentsIsStrict(t *testing.T) {
	for _, test := range []struct {
		name string
		args []interface{}
		ok   bool
	}{
		{name: "defaults", ok: true},
		{name: "range", args: []interface{}{1.0, 2.0}, ok: true},
		{name: "range and delay", args: []interface{}{1.0, 2.0, 3}, ok: true},
		{name: "one argument", args: []interface{}{"typo"}},
		{name: "too many", args: []interface{}{1.0, 2.0, 3, 4}},
		{name: "wrong range types", args: []interface{}{1, 2}},
		{name: "wrong delay type", args: []interface{}{1.0, 2.0, 3.0}},
		{name: "descending range", args: []interface{}{2.0, 1.0}},
		{name: "negative delay", args: []interface{}{1.0, 2.0, -1}},
		{name: "NaN", args: []interface{}{math.NaN(), 2.0}},
		{name: "infinity", args: []interface{}{1.0, math.Inf(1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, ok := parseSmoothMoveArguments(test.args)
			if ok != test.ok {
				t.Fatalf("parseSmoothMoveArguments(%#v) ok = %t, want %t", test.args, ok, test.ok)
			}
		})
	}
}

func TestTextAndUnicodeValidationIsBuildIndependent(t *testing.T) {
	for _, value := range []uint32{0xd800, 0x110000} {
		if err := validateUnicodeScalar(value); err == nil {
			t.Fatalf("validateUnicodeScalar(%#x) unexpectedly succeeded", value)
		}
	}
	if err := validateUnicodeScalar('€'); err != nil {
		t.Fatalf("validateUnicodeScalar(valid) = %v", err)
	}
	for _, test := range []struct {
		text string
		args []int
	}{
		{text: string([]byte{0xff})},
		{text: "a\x00b"},
		{text: "text", args: []int{0, -1}},
		{text: "text", args: []int{0, 0, -1}},
		{text: "text", args: []int{0, 0, 0, 0}},
	} {
		if _, _, err := parseTextInput(test.text, test.args); err == nil {
			t.Fatalf("parseTextInput(%q,%v) unexpectedly succeeded", test.text, test.args)
		}
	}
	if pid, delay, err := parseTextInput("text", []int{17, 3, 7}); err != nil || pid != 17 || delay != 3 {
		t.Fatalf("parseTextInput(valid) = (%d,%d,%v), want (17,3,nil)", pid, delay, err)
	}
}

func TestParseScrollDelayIsStrict(t *testing.T) {
	for _, test := range []struct {
		args []int
		want int
		ok   bool
	}{
		{want: 10, ok: true},
		{args: []int{3}, want: 3, ok: true},
		{args: []int{-1}},
		{args: []int{1, 2}},
	} {
		got, err := parseScrollDelay(test.args)
		if (err == nil) != test.ok || test.ok && got != test.want {
			t.Fatalf("parseScrollDelay(%v) = (%d,%v), want (%d,ok=%t)", test.args, got, err, test.want, test.ok)
		}
	}
}

func TestParseScrollDirectionIsBuildIndependent(t *testing.T) {
	for _, test := range []struct {
		args []interface{}
		want string
		ok   bool
	}{
		{want: "down", ok: true},
		{args: []interface{}{"up"}, want: "up", ok: true},
		{args: []interface{}{7}},
		{args: []interface{}{"diagonal"}},
		{args: []interface{}{"up", "extra"}},
	} {
		got, err := parseScrollDirection(test.args)
		if (err == nil) != test.ok || test.ok && got != test.want {
			t.Fatalf("parseScrollDirection(%v) = (%q,%v), want (%q,ok=%t)", test.args, got, err, test.want, test.ok)
		}
	}
	ScrollDir(1, 7)
	ScrollDir(1, "diagonal")
	ScrollDir(1, "up", "extra")
}

func TestMalformedSmoothMoveDoesNotReachPlatform(t *testing.T) {
	if MoveSmooth(10, 10, "typo") {
		t.Fatal("MoveSmooth accepted one malformed argument")
	}
	if MoveSmooth(10, 10, 1.0, 2.0, 3, "extra") {
		t.Fatal("MoveSmooth accepted surplus arguments")
	}
}

func TestPublicInputValidationRunsBeforeBackendSelection(t *testing.T) {
	for _, call := range []func() error{
		func() error { return KeyTap("") },
		func() error { return KeyToggle("not-a-key") },
	} {
		if err := call(); err == nil || errors.Is(err, ErrNotSupported) {
			t.Fatalf("invalid key error = %v, want a backend-independent argument error", err)
		}
	}
	if err := UnicodeTypeE(0x110000); err == nil {
		t.Fatal("UnicodeTypeE accepted an out-of-range code point")
	}
	if err := TypeStrE(string([]byte{0xff})); err == nil {
		t.Fatal("TypeStrE accepted invalid UTF-8")
	}
	if err := TypeStrE("a\x00b"); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("TypeStrE embedded-NUL error = %v, want backend-independent argument error", err)
	}
	if err := TypeStrE("text", 0, -1); err == nil {
		t.Fatal("TypeStrE accepted a negative delay")
	}
	if err := TypeStrE("text", 0, 0, 0, 0); err == nil {
		t.Fatal("TypeStrE accepted surplus arguments")
	}
	if err := ScrollE(0, 0, -1); err == nil {
		t.Fatal("ScrollE accepted a negative delay")
	}
	if err := ScrollE(0, 0, 1, 2); err == nil {
		t.Fatal("ScrollE accepted surplus arguments")
	}
}

func TestGetLocationColorPropagatesWaylandLocationError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Wayland location failure is Linux-specific")
	}
	t.Setenv(envWaylandDisplay, "robotgo-location-color-test")
	t.Setenv(envDisplay, "")
	color, err := GetLocationColor()
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("GetLocationColor error = %v, want ErrNotSupported", err)
	}
	if color != "" {
		t.Fatalf("GetLocationColor color = %q after location failure, want empty", color)
	}
}

func TestExactTextCodepointsNeverUsesEscapeText(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want []uint32
	}{
		{name: "supplementary rune", text: "😀", want: []uint32{0x1f600}},
		{name: "newline", text: "\n", want: []uint32{'\n'}},
		{name: "mixed", text: "A😀\n", want: []uint32{'A', 0x1f600, '\n'}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := exactTextCodepoints(test.text); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("exactTextCodepoints(%q) = %#v, want %#v", test.text, got, test.want)
			}
		})
	}
}

func TestUTF8SingleRuneKeyUsesExplicitBackendContract(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "")
	if err := validateKeyArgument("é"); err != nil {
		t.Fatalf("UTF-8 single rune should be a valid key argument: %v", err)
	}
	if err := KeyTap("é"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("KeyTap UTF-8 error = %v, want explicit ErrNotSupported without a capable backend", err)
	}
}
