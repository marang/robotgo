package robotgo

import "fmt"

func parseClickArguments(args []interface{}) (name string, double bool, err error) {
	name = "left"
	if len(args) > 0 {
		value, ok := args[0].(string)
		if !ok {
			return "", false, fmt.Errorf("robotgo: mouse button must be a string, got %T", args[0])
		}
		name = value
	}
	if len(args) > 1 {
		value, ok := args[1].(bool)
		if !ok {
			return "", false, fmt.Errorf("robotgo: double-click flag must be a bool, got %T", args[1])
		}
		double = value
	}
	return name, double, nil
}

func parseToggleArguments(args []interface{}) (name string, down bool, err error) {
	name, down = "left", true
	if len(args) > 0 {
		value, ok := args[0].(string)
		if !ok {
			return "", false, fmt.Errorf("robotgo: mouse button must be a string, got %T", args[0])
		}
		name = value
	}
	if len(args) > 1 {
		value, ok := args[1].(string)
		if !ok {
			return "", false, fmt.Errorf("robotgo: mouse state must be a string, got %T", args[1])
		}
		down = value != "up"
	}
	return name, down, nil
}

func parseWindowStateArguments(args []interface{}) (state bool, err error) {
	state = true
	if len(args) == 0 {
		return state, nil
	}
	value, ok := args[0].(bool)
	if !ok {
		return false, fmt.Errorf("robotgo: window state must be a bool, got %T", args[0])
	}
	return value, nil
}
