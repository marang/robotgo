//go:build cgo

package robotgo

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// CmdCtrl returns "cmd" on macOS and "ctrl" on other platforms.
func CmdCtrl() string {
	if runtime.GOOS == "darwin" {
		return "cmd"
	}
	return "ctrl"
}

// ToInterfaces convert []string to []interface{}
func ToInterfaces(fields []string) []interface{} {
	res := make([]interface{}, 0, len(fields))
	for _, s := range fields {
		res = append(res, s)
	}
	return res
}

// ToStrings convert []interface{} to []string
func ToStrings(fields []interface{}) []string {
	res, _ := toStringsE(fields)
	return res
}

func toStringsE(fields []interface{}) ([]string, error) {
	res := make([]string, 0, len(fields))
	for _, field := range fields {
		value, ok := field.(string)
		if !ok {
			return nil, fmt.Errorf("robotgo: key modifier must be a string, got %T", field)
		}
		res = append(res, value)
	}
	return res, nil
}

// CharCodeAt char code at utf-8
func CharCodeAt(s string, n int) rune {
	i := 0
	for _, r := range s {
		if i == n {
			return r
		}
		i++
	}

	return 0
}

// ToUC trans string to unicode []string
func ToUC(text string) []string {
	var uc []string

	for _, r := range text {
		textQ := strconv.QuoteToASCII(string(r))
		textUnQ := textQ[1 : len(textQ)-1]

		st := strings.ReplaceAll(textUnQ, "\\u", "U")
		if st == "\\\\" {
			st = "\\"
		}
		if st == `\"` {
			st = `"`
		}
		uc = append(uc, st)
	}

	return uc
}
