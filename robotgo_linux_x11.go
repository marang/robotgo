//go:build linux && !wayland
// +build linux,!wayland

package robotgo

import (
	"errors"
	"log"

	"github.com/robotn/xgb"
	"github.com/robotn/xgb/xinerama"
	"github.com/robotn/xgb/xproto"
	"github.com/robotn/xgbutil"
	"github.com/robotn/xgbutil/ewmh"
)

var xu *xgbutil.XUtil

// GetBounds returns the window bounds using X11.
func GetBounds(pid int, args ...int) (int, int, int, int) {
	var isPid int
	if len(args) > 0 || NotPid {
		isPid = 1
		return internalGetBounds(pid, isPid)
	}
	xid, err := GetXid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return 0, 0, 0, 0
	}
	return internalGetBounds(int(xid), isPid)
}

// GetClient returns the client bounds using X11.
func GetClient(pid int, args ...int) (int, int, int, int) {
	var isPid int
	if len(args) > 0 || NotPid {
		isPid = 1
		return internalGetClient(pid, isPid)
	}
	xid, err := GetXid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return 0, 0, 0, 0
	}
	return internalGetClient(int(xid), isPid)
}

// internalGetTitle gets the window title using X11.
func internalGetTitle(pid int, args ...int) string {
	var isPid int
	if len(args) > 0 || NotPid {
		isPid = 1
		return cgetTitle(pid, isPid)
	}
	xid, err := GetXid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return ""
	}
	return cgetTitle(int(xid), isPid)
}

// ActivePidC activates the window by PID via X11.
func ActivePidC(pid int, args ...int) error {
	var isPid int
	if len(args) > 0 || NotPid {
		isPid = 1
		internalActive(pid, isPid)
		return nil
	}
	xid, err := GetXid(xu, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return err
	}
	internalActive(int(xid), isPid)
	return nil
}

// ActivePid activates the window by PID via X11.
func ActivePid(pid int, args ...int) error {
	if xu == nil {
		var err error
		xu, err = xgbutil.NewConn()
		if err != nil {
			return err
		}
	}
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
	if xu == nil {
		var err error
		xu, err = xgbutil.NewConn()
		if err != nil {
			return 0, err
		}
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
		wmPid, err := ewmh.WmPidGet(xu, window)
		if err != nil {
			return 0, err
		}
		if uint(pid) == wmPid {
			return window, nil
		}
	}
	return 0, errors.New("failed to find a window with a matching pid")
}

// DisplaysNum returns the count of displays using Xinerama.
func DisplaysNum() int {
	c, err := xgb.NewConn()
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
	conn, err := xgb.NewConn()
	if err != nil {
		return -1
	}
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
