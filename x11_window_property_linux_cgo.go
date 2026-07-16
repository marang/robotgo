//go:build linux && cgo
// +build linux,cgo

package robotgo

import (
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xprop"
)

const x11WindowPIDProperty = "_NET_WM_PID"

func x11WindowPID(xu *xgbutil.XUtil, window xproto.Window) (uint, error) {
	reply, err := xprop.GetProperty(xu, window, x11WindowPIDProperty)
	if err != nil {
		return 0, err
	}
	if reply == nil || reply.Type != xproto.AtomCardinal || reply.Format != 32 ||
		reply.ValueLen < 1 || len(reply.Value) < 4 {
		if reply == nil {
			return 0, fmt.Errorf("invalid _NET_WM_PID property on X11 window %#x: empty reply", window)
		}
		return 0, fmt.Errorf(
			"invalid _NET_WM_PID property on X11 window %#x: type=%d format=%d items=%d bytes=%d",
			window, reply.Type, reply.Format, reply.ValueLen, len(reply.Value),
		)
	}
	return uint(xgb.Get32(reply.Value)), nil
}
