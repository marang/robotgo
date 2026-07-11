//go:build !linux

package portal

import "context"

func openScreenCast(context.Context, ScreenCastOptions) (ScreenCast, error) {
	return nil, ErrScreenCastUnavailable
}

func probeScreenCast(context.Context) (ScreenCastCapability, error) {
	return ScreenCastCapability{}, ErrScreenCastUnavailable
}
