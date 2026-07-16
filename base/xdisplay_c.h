#ifndef ROBOTGO_XDISPLAY_C_H
#define ROBOTGO_XDISPLAY_C_H

#include <limits.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h> /* For strdup() */
#include <X11/Xlib.h>

static Display *mainDisplay = NULL;
static char *displayName = NULL;
static unsigned long mainDisplayGeneration = 1;

/*
 * XSetErrorHandler is process-global. RobotGo serializes all accesses to the
 * shared X11 connection on the Go side; this scoped trap additionally limits
 * captured errors to the target Display and to requests issued by the scope.
 * Errors from other displays or request ranges are delegated to the handler
 * that was installed before RobotGo entered the scope.
 */
typedef struct RobotGoXErrorTrap {
	unsigned long token;
} RobotGoXErrorTrap;

typedef struct RobotGoXErrorTrapState {
	_Atomic(Display *) display;
	_Atomic unsigned long first_serial;
	_Atomic unsigned long last_serial;
	_Atomic int bounded;
	_Atomic int error_code;
	_Atomic int active;
	_Atomic unsigned long token;
} RobotGoXErrorTrapState;

static RobotGoXErrorTrapState robotgo_xerror_state;
static _Atomic(XErrorHandler) robotgo_xerror_previous = NULL;
static unsigned long robotgo_xerror_next_token = 1;

static int robotgo_xerror_handler(Display *display, XErrorEvent *event) {
	if (atomic_load_explicit(&robotgo_xerror_state.active,
	                         memory_order_acquire) &&
	    display == atomic_load_explicit(&robotgo_xerror_state.display,
	                                    memory_order_relaxed) &&
	    event->serial >= atomic_load_explicit(
	                         &robotgo_xerror_state.first_serial,
	                         memory_order_relaxed) &&
	    (!atomic_load_explicit(&robotgo_xerror_state.bounded,
	                           memory_order_acquire) ||
	     event->serial <= atomic_load_explicit(&robotgo_xerror_state.last_serial,
	                                           memory_order_relaxed))) {
		atomic_store_explicit(&robotgo_xerror_state.error_code,
		                      event->error_code,
		                      memory_order_release);
		return 0;
	}

	XErrorHandler previous = atomic_load_explicit(
		&robotgo_xerror_previous, memory_order_acquire);
	if (previous != NULL && previous != robotgo_xerror_handler) {
		return previous(display, event);
	}

	/* NULL denotes Xlib's default handler, which terminates on X errors. */
	char message[256] = {0};
	XGetErrorText(display, event->error_code, message, sizeof(message));
	fprintf(stderr,
	        "RobotGo: unscoped X11 error: %s (request=%u, minor=%u, "
	        "serial=%lu)\n",
	        message, event->request_code, event->minor_code, event->serial);
	exit(EXIT_FAILURE);
}

static inline int robotgo_xerror_trap_begin(Display *display,
					     RobotGoXErrorTrap *trap) {
	if (display == NULL || trap == NULL ||
	    atomic_load_explicit(&robotgo_xerror_state.active,
	                         memory_order_acquire)) {
		return 0;
	}

	/* Let the pre-existing handler process errors from earlier requests. */
	XSync(display, False);
	trap->token = robotgo_xerror_next_token++;
	atomic_store_explicit(&robotgo_xerror_state.display, display,
	                      memory_order_relaxed);
	atomic_store_explicit(&robotgo_xerror_state.first_serial,
	                      NextRequest(display), memory_order_relaxed);
	atomic_store_explicit(&robotgo_xerror_state.last_serial, ULONG_MAX,
	                      memory_order_relaxed);
	atomic_store_explicit(&robotgo_xerror_state.bounded, 0,
	                      memory_order_relaxed);
	atomic_store_explicit(&robotgo_xerror_state.error_code, 0,
	                      memory_order_relaxed);
	atomic_store_explicit(&robotgo_xerror_state.token, trap->token,
	                      memory_order_relaxed);
	XErrorHandler previous = XSetErrorHandler(robotgo_xerror_handler);
	atomic_store_explicit(&robotgo_xerror_previous, previous,
	                      memory_order_release);
	atomic_store_explicit(&robotgo_xerror_state.active, 1,
	                      memory_order_release);
	return 1;
}

static inline int robotgo_xerror_trap_end(RobotGoXErrorTrap *trap) {
	if (trap == NULL ||
	    !atomic_load_explicit(&robotgo_xerror_state.active,
	                          memory_order_acquire) ||
	    atomic_load_explicit(&robotgo_xerror_state.token,
	                         memory_order_relaxed) != trap->token) {
		return 0;
	}

	Display *display = atomic_load_explicit(&robotgo_xerror_state.display,
	                                       memory_order_relaxed);
	atomic_store_explicit(&robotgo_xerror_state.last_serial,
	                      NextRequest(display) - 1,
	                      memory_order_relaxed);
	atomic_store_explicit(&robotgo_xerror_state.bounded, 1,
	                      memory_order_release);
	XSync(display, False);
	atomic_store_explicit(&robotgo_xerror_state.active, 0,
	                      memory_order_release);
	XErrorHandler previous = atomic_load_explicit(
		&robotgo_xerror_previous, memory_order_acquire);
	XSetErrorHandler(previous);
	atomic_store_explicit(&robotgo_xerror_previous, NULL,
	                      memory_order_release);
	return atomic_load_explicit(&robotgo_xerror_state.error_code,
	                           memory_order_acquire) == 0;
}

void XCloseMainDisplay(void) {
	if (mainDisplay != NULL) {
		XCloseDisplay(mainDisplay);
		mainDisplay = NULL;
	}
	mainDisplayGeneration++;
}

Display *XGetMainDisplay(void) {
	if (mainDisplay == NULL) {
		/* An explicit display name never falls through to another server. */
		mainDisplay = XOpenDisplay(displayName);
		if (mainDisplay != NULL) {
			mainDisplayGeneration++;
		}
	}

	return mainDisplay;
}

int setXDisplay(const char *name) {
	char *next = NULL;
	if (name != NULL && name[0] != '\0') {
		next = strdup(name);
		if (next == NULL) {
			return -1;
		}
	}
	free(displayName);
	displayName = next;
	XCloseMainDisplay();
	return 0;
}

char *getXDisplay(void) {
	return displayName;
}

unsigned long XGetMainDisplayGeneration(void) {
	return mainDisplayGeneration;
}

#endif /* ROBOTGO_XDISPLAY_C_H */
