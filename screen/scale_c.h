#ifndef ROBOTGO_SCALE_C_H
#define ROBOTGO_SCALE_C_H

/* Platform declarations are supplied by screen_c.h or the hermetic C harness. */
static inline intptr scaleX();

static inline double sys_scale(int32_t display_id) {
	#if defined(IS_MACOSX)
		CGDirectDisplayID displayID = (CGDirectDisplayID) display_id;
		if (display_id == -1) {
			displayID = CGMainDisplayID();
		}

		CGDisplayModeRef modeRef = CGDisplayCopyDisplayMode(displayID);
		if (modeRef == NULL) {
			return 0.0;
		}
		double pixelWidth = CGDisplayModeGetPixelWidth(modeRef);
		double targetWidth = CGDisplayModeGetWidth(modeRef);
		CGDisplayModeRelease(modeRef);
		if (pixelWidth <= 0.0 || targetWidth <= 0.0) {
			return 0.0;
		}
		return pixelWidth / targetWidth;
	#elif defined(IS_LINUX)
#if !defined(DISPLAY_SERVER_WAYLAND)
    if (detectDisplayServer() == Wayland) {
        return 1.0; // No global DPI query; assume 1.0 scaling
    }
    Display *dpy = XGetMainDisplay();
    if (!dpy) { return 1.0; }

    int scr = 0; /* Screen number */
    double xres = ((((double) DisplayWidth(dpy, scr)) * 25.4) /
            ((double) DisplayWidthMM(dpy, scr)));

    char *rms = XResourceManagerString(dpy);
    if (rms) {
        XrmDatabase db = XrmGetStringDatabase(rms);
        if (db) {
            XrmValue value;
            char *type = NULL;

            if (XrmGetResource(db, "Xft.dpi", "String", &type, &value)) {
                if (value.addr) {
                    xres = atof(value.addr);
                }
            }

            XrmDestroyDatabase(db);
        }
    }
    return xres / 96.0;
#else
    return 1.0;
#endif
	#elif defined(IS_WINDOWS)
		double s = scaleX() / 96.0;
		return s;
	#endif
}

static inline intptr scaleX(){
	#if defined(IS_MACOSX)
		return 0;
	#elif defined(IS_LINUX)
		return 0;
	#elif defined(IS_WINDOWS)
		HDC desktopDc = GetDC(NULL);
		if (desktopDc == NULL) {
			return 0;
		}
		intptr horizontalDPI = GetDeviceCaps(desktopDc, LOGPIXELSX);
		ReleaseDC(NULL, desktopDc);
		return horizontalDPI > 0 ? horizontalDPI : 0;
	#endif
}

#endif /* ROBOTGO_SCALE_C_H */
