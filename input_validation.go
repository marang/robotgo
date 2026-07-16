package robotgo

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

func uppercaseSingleRuneKey(key string) bool {
	if !utf8.ValidString(key) {
		return false
	}
	value, size := utf8.DecodeRuneInString(key)
	return size == len(key) && unicode.IsUpper(value)
}

func validateKeyArgument(key string) error {
	if _, err := portalKeysymForKey(key); err != nil {
		return fmt.Errorf("robotgo: invalid keyboard key %q", key)
	}
	return nil
}

func parseKeyArguments(args []interface{}, toggle bool) (pid int, down bool, modifiers []string, err error) {
	down = true
	pidSet := false
	for _, arg := range args {
		switch value := arg.(type) {
		case string:
			modifiers = append(modifiers, value)
		case []string:
			modifiers = append(modifiers, value...)
		case int:
			if pidSet {
				return 0, false, nil, errors.New("robotgo: multiple process IDs in key arguments")
			}
			pid = value
			pidSet = true
		default:
			return 0, false, nil, fmt.Errorf("robotgo: key argument must be a modifier string, modifier slice, or process ID, got %T", arg)
		}
	}
	if toggle && len(modifiers) > 0 {
		switch strings.ToLower(modifiers[0]) {
		case "down":
			modifiers = modifiers[1:]
		case "up":
			down = false
			modifiers = modifiers[1:]
		}
	}
	return pid, down, modifiers, nil
}

func normalizeKeyModifiers(modifiers []string) ([]string, error) {
	result := make([]string, len(modifiers))
	for index, modifier := range modifiers {
		normalized := strings.ToLower(modifier)
		switch normalized {
		case "none",
			"alt", "lalt", "ralt",
			"cmd", "command", "lcmd", "rcmd",
			"ctrl", "control", "lctrl", "rctrl",
			"shift", "lshift", "rshift", "right_shift":
			result[index] = normalized
		default:
			return nil, fmt.Errorf("robotgo: unsupported key modifier %q", modifier)
		}
	}
	return result, nil
}

func parseSmoothMoveArguments(args []interface{}) (lowDelay, highDelay float64, extraDelay int, ok bool) {
	lowDelay, highDelay, extraDelay = 1, 3, 1
	switch len(args) {
	case 0:
		return lowDelay, highDelay, extraDelay, true
	case 2, 3:
	default:
		return 0, 0, 0, false
	}
	var lowOK, highOK bool
	lowDelay, lowOK = args[0].(float64)
	highDelay, highOK = args[1].(float64)
	if !lowOK || !highOK {
		return 0, 0, 0, false
	}
	if len(args) == 3 {
		var delayOK bool
		extraDelay, delayOK = args[2].(int)
		if !delayOK {
			return 0, 0, 0, false
		}
	}
	if !validSmoothDelayRange(lowDelay, highDelay) || extraDelay < 0 {
		return 0, 0, 0, false
	}
	return lowDelay, highDelay, extraDelay, true
}

func validSmoothDelayRange(lowDelay, highDelay float64) bool {
	return !math.IsNaN(lowDelay) && !math.IsNaN(highDelay) &&
		!math.IsInf(lowDelay, 0) && !math.IsInf(highDelay, 0) &&
		lowDelay >= 0 && highDelay >= lowDelay
}

func validateUnicodeScalar(value uint32) error {
	if value > 0x10ffff || value >= 0xd800 && value <= 0xdfff {
		return fmt.Errorf("robotgo: invalid Unicode code point U+%X", value)
	}
	return nil
}

func parseTextInput(text string, args []int) (pid, delay int, err error) {
	if len(args) > 3 {
		return 0, 0, fmt.Errorf("robotgo: text input accepts at most pid, delay, and X11 delay, got %d arguments", len(args))
	}
	if !utf8.ValidString(text) {
		return 0, 0, errors.New("robotgo: text is not valid UTF-8")
	}
	if len(args) > 0 {
		pid = args[0]
	}
	if len(args) > 1 {
		delay = args[1]
		if delay < 0 {
			return 0, 0, errors.New("robotgo: text delay must be non-negative")
		}
	}
	if len(args) > 2 && args[2] < 0 {
		return 0, 0, errors.New("robotgo: X11 text delay must be non-negative")
	}
	return pid, delay, nil
}

func parseScrollDelay(args []int) (int, error) {
	if len(args) > 1 {
		return 0, fmt.Errorf("robotgo: scroll accepts at most one delay argument, got %d", len(args))
	}
	delay := 10
	if len(args) == 1 {
		delay = args[0]
	}
	if delay < 0 {
		return 0, errors.New("robotgo: scroll delay must be non-negative")
	}
	return delay, nil
}

func parseScrollDirection(args []interface{}) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("robotgo: scroll direction accepts at most one argument, got %d", len(args))
	}
	direction := "down"
	if len(args) == 1 {
		value, ok := args[0].(string)
		if !ok {
			return "", fmt.Errorf("robotgo: scroll direction must be a string, got %T", args[0])
		}
		direction = value
	}
	switch direction {
	case "up", "down", "left", "right":
		return direction, nil
	default:
		return "", fmt.Errorf("robotgo: unknown scroll direction %q", direction)
	}
}
