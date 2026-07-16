//go:build linux && !wayland && cgo
// +build linux,!wayland,cgo

package robotgo

import (
	"errors"
	"fmt"
	"log"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xinerama"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
)

func newConfiguredX11XUtil() (*xgbutil.XUtil, error) {
	return newX11XUtilForDisplay(configuredX11DisplayName())
}

func newConfiguredX11Conn() (*xgb.Conn, error) {
	return xgb.NewConnDisplay(configuredX11DisplayName())
}

func newX11XUtilForDisplay(display string) (*xgbutil.XUtil, error) {
	return xgbutil.NewConnDisplay(display)
}

func configuredX11DisplayName() string {
	unlock := lockNativeX11Display()
	defer unlock()
	return getXDisplayNameLocked()
}

// GetBounds returns the window bounds using X11.
func GetBounds(pid int, args ...int) (int, int, int, int) {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		return internalGetBounds(pid, isPid)
	}
	unlock := lockNativeX11Display()
	defer unlock()
	xu, err := newX11XUtilForDisplay(getXDisplayNameLocked())
	if err != nil {
		log.Println("Open configured X11 target errors is: ", err)
		return 0, 0, 0, 0
	}
	defer xu.Conn().Close()
	xid, err := GetXidFromPid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return 0, 0, 0, 0
	}
	return internalGetBoundsLocked(int(xid), isPid)
}

// GetClient returns the client bounds using X11.
func GetClient(pid int, args ...int) (int, int, int, int) {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		return internalGetClient(pid, isPid)
	}
	unlock := lockNativeX11Display()
	defer unlock()
	xu, err := newX11XUtilForDisplay(getXDisplayNameLocked())
	if err != nil {
		log.Println("Open configured X11 target errors is: ", err)
		return 0, 0, 0, 0
	}
	defer xu.Conn().Close()
	xid, err := GetXidFromPid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return 0, 0, 0, 0
	}
	return internalGetClientLocked(int(xid), isPid)
}

// internalGetTitle gets the window title using X11.
func internalGetTitle(pid int, args ...int) string {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		return cgetTitle(pid, isPid)
	}
	unlock := lockNativeX11Display()
	defer unlock()
	xu, err := newX11XUtilForDisplay(getXDisplayNameLocked())
	if err != nil {
		log.Println("Open configured X11 target errors is: ", err)
		return ""
	}
	defer xu.Conn().Close()
	xid, err := GetXidFromPid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return ""
	}
	return cgetTitleLocked(int(xid), isPid)
}

// ActivePidC activates the window by PID via X11.
func ActivePidC(pid int, args ...int) error {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		if !internalActive(pid, isPid) {
			return fmt.Errorf("%w: native X11 backend could not activate target window", errWindowOperationFailed)
		}
		return nil
	}
	unlock := lockNativeX11Display()
	defer unlock()
	xu, err := newX11XUtilForDisplay(getXDisplayNameLocked())
	if err != nil {
		log.Println("Open configured X11 target errors is: ", err)
		return err
	}
	defer xu.Conn().Close()
	xid, err := GetXidFromPid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return err
	}
	if !internalActiveLocked(int(xid), isPid) {
		return fmt.Errorf("%w: native X11 backend could not activate target window", errWindowOperationFailed)
	}
	return nil
}

// ActivePid activates the window by PID via X11.
func ActivePid(pid int, args ...int) error {
	xu, err := newConfiguredX11XUtil()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()
	if len(args) > 0 {
		if err := ewmh.ActiveWindowReq(xu, xproto.Window(pid)); err != nil {
			return err
		}
		return nil
	}
	xid, err := GetXidFromPid(xu, pid)
	if err != nil {
		return err
	}
	if err := ewmh.ActiveWindowReq(xu, xid); err != nil {
		return err
	}
	return nil
}

// GetXid gets the XID for a given PID.
func GetXid(xu *xgbutil.XUtil, pid int) (xproto.Window, error) {
	owned := false
	if xu == nil {
		var err error
		xu, err = newConfiguredX11XUtil()
		if err != nil {
			return 0, err
		}
		owned = true
	}
	if owned {
		defer xu.Conn().Close()
	}
	xid, err := GetXidFromPid(xu, pid)
	return xid, err
}

// GetXidFromPid returns the XID for the given PID.
func GetXidFromPid(xu *xgbutil.XUtil, pid int) (xproto.Window, error) {
	windows, err := ewmh.ClientListGet(xu)
	if err != nil {
		return 0, err
	}
	for _, window := range windows {
		wmPid, err := x11WindowPID(xu, window)
		if err != nil {
			// A client can legitimately omit _NET_WM_PID. Keep scanning the
			// remaining client list instead of making its ordering observable.
			continue
		}
		if uint(pid) == wmPid {
			return window, nil
		}
	}
	return 0, errors.New("failed to find a window with a matching pid")
}

// DisplaysNum returns the count of displays using Xinerama.
func DisplaysNum() int {
	c, err := newConfiguredX11Conn()
	if err != nil {
		return 0
	}
	defer c.Close()
	if err := xinerama.Init(c); err != nil {
		return 0
	}
	reply, err := xinerama.QueryScreens(c).Reply()
	if err != nil {
		return 0
	}
	return int(reply.Number)
}

// GetMainId returns the primary display id.
func GetMainId() int {
	conn, err := newConfiguredX11Conn()
	if err != nil {
		return -1
	}
	defer conn.Close()
	setup := xproto.Setup(conn)
	defaultScreen := setup.DefaultScreen(conn)
	id := -1
	for i, screen := range setup.Roots {
		if defaultScreen.Root == screen.Root {
			id = i
			break
		}
	}
	return id
}
