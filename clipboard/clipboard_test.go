// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build desktopintegration && (darwin || windows)
// +build desktopintegration
// +build darwin windows

package clipboard_test

import (
	"os"
	"testing"

	"github.com/marang/robotgo/clipboard"
)

func TestCopyAndPaste(t *testing.T) {
	preserveTextClipboard(t)
	expected := "日本語"

	err := clipboard.WriteAll(expected)
	if err != nil {
		t.Fatal(err)
	}

	actual, err := clipboard.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if actual != expected {
		t.Errorf("want %s, got %s", expected, actual)
	}
}

func TestMultiCopyAndPaste(t *testing.T) {
	preserveTextClipboard(t)
	expected1 := "French: éèêëàùœç"
	expected2 := "Weird UTF-8: 💩☃"

	err := clipboard.WriteAll(expected1)
	if err != nil {
		t.Fatal(err)
	}

	actual1, err := clipboard.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if actual1 != expected1 {
		t.Errorf("want %s, got %s", expected1, actual1)
	}

	err = clipboard.WriteAll(expected2)
	if err != nil {
		t.Fatal(err)
	}

	actual2, err := clipboard.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if actual2 != expected2 {
		t.Errorf("want %s, got %s", expected2, actual2)
	}
}

func BenchmarkReadAll(b *testing.B) {
	preserveTextClipboard(b)
	for i := 0; i < b.N; i++ {
		clipboard.ReadAll()
	}
}

func BenchmarkWriteAll(b *testing.B) {
	preserveTextClipboard(b)
	text := "いろはにほへと"
	for i := 0; i < b.N; i++ {
		clipboard.WriteAll(text)
	}
}

func preserveTextClipboard(tb testing.TB) {
	tb.Helper()
	if os.Getenv("ROBOTGO_REQUIRE_DESKTOP_INTEGRATION") != "1" {
		tb.Skip(
			"live clipboard integration requires " +
				"ROBOTGO_REQUIRE_DESKTOP_INTEGRATION=1 on a disposable, self-owned desktop",
		)
	}
	previous, err := clipboard.ReadAll()
	if err != nil {
		tb.Skipf("cannot preserve the existing text clipboard: %v", err)
	}
	tb.Cleanup(func() {
		if err := clipboard.WriteAll(previous); err != nil {
			tb.Errorf("restore text clipboard: %v", err)
			return
		}
		restored, err := clipboard.ReadAll()
		if err != nil {
			tb.Errorf("verify restored text clipboard: %v", err)
			return
		}
		if restored != previous {
			tb.Error("restored text clipboard does not match its previous value")
		}
	})
}
