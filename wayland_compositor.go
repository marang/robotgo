package robotgo

import (
	"os"
	"runtime"
	"strings"
)

const (
	envDesktop           = "XDG_CURRENT_DESKTOP"
	envSessionDesktop    = "XDG_SESSION_DESKTOP"
	envSwaySock          = "SWAYSOCK"
	envHyprlandSignature = "HYPRLAND_INSTANCE_SIGNATURE"

	compositorWlroots     = "wlroots"
	compositorMutter      = "mutter"
	compositorKWin        = "kwin"
	compositorHyprland    = "hyprland"
	compositorSway        = "sway"
	compositorWayfire     = "wayfire"
	compositorRiver       = "river"
	compositorLabwc       = "labwc"
	compositorDwl         = "dwl"
	compositorGamescope   = "gamescope"
	compositorUnknown     = "unknown"
	desktopTokenSway      = "sway"
	desktopTokenGNOME     = "gnome"
	desktopTokenKDE       = "kde"
	desktopTokenPlasma    = "plasma"
	desktopTokenWayfire   = "wayfire"
	desktopTokenHyprland  = "hyprland"
	desktopTokenRiver     = "river"
	desktopTokenLabwc     = "labwc"
	desktopTokenDwl       = "dwl"
	desktopTokenGamescope = "gamescope"
)

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

func detectWaylandCompositor() string {
	if runtime.GOOS != "linux" || DetectDisplayServer() != DisplayServerWayland {
		return ""
	}

	desktop := strings.ToLower(os.Getenv(envDesktop))
	session := strings.ToLower(os.Getenv(envSessionDesktop))

	if isSwaySession(desktop, session) {
		return compositorSway
	}
	if isHyprlandSession(desktop, session) {
		return compositorHyprland
	}
	if containsAny(desktop, desktopTokenWayfire) ||
		containsAny(session, desktopTokenWayfire) {
		return compositorWayfire
	}
	if containsAny(desktop, desktopTokenRiver) ||
		containsAny(session, desktopTokenRiver) {
		return compositorRiver
	}
	if containsAny(desktop, desktopTokenLabwc) ||
		containsAny(session, desktopTokenLabwc) {
		return compositorLabwc
	}
	if containsAny(desktop, desktopTokenDwl) ||
		containsAny(session, desktopTokenDwl) {
		return compositorDwl
	}
	if containsAny(desktop, desktopTokenGamescope) ||
		containsAny(session, desktopTokenGamescope) {
		return compositorGamescope
	}
	if containsAny(desktop, desktopTokenGNOME) ||
		containsAny(session, desktopTokenGNOME) {
		return compositorMutter
	}
	if containsAny(desktop, desktopTokenKDE, desktopTokenPlasma) ||
		containsAny(session, desktopTokenKDE, desktopTokenPlasma) {
		return compositorKWin
	}

	return compositorUnknown
}

func isSwaySession(desktop, session string) bool {
	return os.Getenv(envSwaySock) != "" ||
		containsAny(desktop, desktopTokenSway) ||
		containsAny(session, desktopTokenSway)
}

func isHyprlandSession(desktop, session string) bool {
	return os.Getenv(envHyprlandSignature) != "" ||
		containsAny(desktop, desktopTokenHyprland) ||
		containsAny(session, desktopTokenHyprland)
}
