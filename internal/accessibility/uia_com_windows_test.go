//go:build windows

package accessibility

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

func TestBoundsFromWindowsRect(t *testing.T) {
	got, err := boundsFromWindowsRect(windows.Rect{Left: -10, Top: 20, Right: 31, Bottom: 65})
	if err != nil {
		t.Fatal(err)
	}
	want := Bounds{X: -10, Y: 20, Width: 41, Height: 45}
	if got == nil || *got != want {
		t.Fatalf("bounds = %+v, want %+v", got, want)
	}

	for _, rectangle := range []windows.Rect{
		{},
		{Left: 10, Top: 20, Right: 10, Bottom: 30},
		{Left: 10, Top: 20, Right: 20, Bottom: 20},
		{Left: 20, Top: 20, Right: 10, Bottom: 30},
	} {
		got, err := boundsFromWindowsRect(rectangle)
		if err != nil || got != nil {
			t.Fatalf("empty bounds for %+v = %+v, %v", rectangle, got, err)
		}
	}
}

func TestBoundedBSTRReportsByteAndUnitTruncation(t *testing.T) {
	for _, test := range []struct {
		name          string
		value         string
		maxBytes      uint32
		want          string
		wantTruncated bool
	}{
		{
			name: "ASCII exact boundary", value: strings.Repeat("a", maxElementConditionValueBytes),
			maxBytes: maxElementConditionValueBytes, want: strings.Repeat("a", maxElementConditionValueBytes),
		},
		{
			name: "ASCII extra unit", value: strings.Repeat("a", maxElementConditionValueBytes) + "x",
			maxBytes: maxElementConditionValueBytes, want: strings.Repeat("a", maxElementConditionValueBytes),
			wantTruncated: true,
		},
		{name: "UTF-8 exact boundary", value: "éé", maxBytes: 4, want: "éé"},
		{name: "UTF-8 byte overflow", value: "ééx", maxBytes: 4, want: "éé", wantTruncated: true},
		{name: "partial rune removed", value: "😀", maxBytes: 3, wantTruncated: true},
		{name: "embedded NUL", value: "a\x00b", maxBytes: 3, want: "a\x00b"},
		{name: "empty", maxBytes: 4},
		{name: "zero limit", value: "x", wantTruncated: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value := ole.SysAllocStringLen(test.value)
			if value == nil {
				t.Fatal("SysAllocStringLen returned nil")
			}
			defer func() { _ = ole.SysFreeString(value) }()
			got, truncated := boundedBSTRWithTruncation((*uint16)(unsafe.Pointer(value)), test.maxBytes)
			if got != test.want || truncated != test.wantTruncated {
				t.Fatalf(
					"boundedBSTRWithTruncation(%q, %d) = %q, %t; want %q, %t",
					test.value, test.maxBytes, got, truncated, test.want, test.wantTruncated,
				)
			}
		})
	}
	if got, truncated := boundedBSTRWithTruncation(nil, 4); got != "" || truncated {
		t.Fatalf("nil BSTR = %q, %t; want empty, false", got, truncated)
	}
}

func TestUIAValueReportsTruncatedPropertyText(t *testing.T) {
	reader := &uiaPropertyReader{cache: map[int32]uiaPropertyValue{
		uiaPropertyValueAvailable: {kind: uiaPropertyBoolean, boolean: true},
		uiaPropertyValueValue:     {kind: uiaPropertyText, text: "prefix", truncated: true},
	}}
	value, observable, truncated, err := (&uiaCOMQuery{}).value("textbox", reader)
	if err != nil || value != "prefix" || observable || !truncated {
		t.Fatalf("truncated UIA value = %q, observable=%t, truncated=%t, err=%v",
			value, observable, truncated, err)
	}
}
