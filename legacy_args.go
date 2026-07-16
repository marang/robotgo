package robotgo

import "fmt"

func validateMouseButtonName(name string) error {
	switch name {
	case "", "left", "center", "middle", "right", "wheelDown", "wheelUp", "wheelLeft", "wheelRight":
		return nil
	default:
		return fmt.Errorf("robotgo: unknown mouse button %q", name)
	}
}

func parseClickArguments(args []interface{}) (name string, double bool, err error) {
	if len(args) > 2 {
		return "", false, fmt.Errorf("robotgo: click accepts at most a button and double-click flag, got %d arguments", len(args))
	}
	name = "left"
	if len(args) > 0 {
		value, ok := args[0].(string)
		if !ok {
			return "", false, fmt.Errorf("robotgo: mouse button must be a string, got %T", args[0])
		}
		name = value
	}
	if err := validateMouseButtonName(name); err != nil {
		return "", false, err
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
	if len(args) > 2 {
		return "", false, fmt.Errorf("robotgo: toggle accepts at most a button and state, got %d arguments", len(args))
	}
	name, down = "left", true
	if len(args) > 0 {
		value, ok := args[0].(string)
		if !ok {
			return "", false, fmt.Errorf("robotgo: mouse button must be a string, got %T", args[0])
		}
		name = value
	}
	if err := validateMouseButtonName(name); err != nil {
		return "", false, err
	}
	if len(args) > 1 {
		value, ok := args[1].(string)
		if !ok {
			return "", false, fmt.Errorf("robotgo: mouse state must be a string, got %T", args[1])
		}
		switch value {
		case "down":
			down = true
		case "up":
			down = false
		default:
			return "", false, fmt.Errorf("robotgo: mouse state must be %q or %q, got %q", "down", "up", value)
		}
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
