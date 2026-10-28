package robotgo

import (
	"slices"
	"testing"
)

func TestToUCUnicodeAndEscapes(t *testing.T) {
	want := []string{"a", "U20ac", "U0001f600", "U0010ffff", `"`, `\`}
	if got := ToUC("a€😀\U0010ffff\"\\"); !slices.Equal(got, want) {
		t.Fatalf("ToUC = %q, want %q", got, want)
	}
	if got := ToUC(""); len(got) != 0 {
		t.Fatalf("ToUC(empty) = %q", got)
	}
}

func TestUnicodeScalarBoundaries(t *testing.T) {
	for _, value := range []uint32{0, 'a', 0x20ac, 0xd7ff, 0xe000, 0xffff, 0x10000, 0x1f600, 0x10ffff} {
		if err := validateUnicodeScalar(value); err != nil {
			t.Fatalf("validateUnicodeScalar(%#x) = %v", value, err)
		}
	}
	for _, value := range []uint32{0xd800, 0xdbff, 0xdc00, 0xdfff, 0x110000, ^uint32(0)} {
		// Invalid public input must fail before any native backend is touched.
		if err := UnicodeTypeE(value); err == nil {
			t.Fatalf("UnicodeTypeE(%#x) succeeded", value)
		}
	}
}
