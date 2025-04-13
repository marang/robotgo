#pragma once
#ifndef OS_H
#define OS_H

#include <cstdlib>  // For getenv()
#include <string>   // For std::string

#if !defined(IS_MACOSX) && defined(__APPLE__) && defined(__MACH__)
    #define IS_MACOSX
#endif /* IS_MACOSX */

#if !defined(IS_WINDOWS) && (defined(WIN32) || defined(_WIN32) || \
                             defined(__WIN32__) || defined(__WINDOWS__) || defined(__CYGWIN__))
    #define IS_WINDOWS
#endif /* IS_WINDOWS */

#if !defined(IS_LINUX) && (defined(__linux__) || defined(__LINUX__))
    #define IS_LINUX
#endif

#if defined(IS_WINDOWS)
	#define STRICT /* Require use of exact types. */
	#define WIN32_LEAN_AND_MEAN 1 /* Speed up compilation. */
	#include <windows.h>
#elif !defined(IS_MACOSX) && !defined(IS_LINUX)
	#error "Sorry, this platform isn't supported yet!"
#endif

/* Interval to align by for large buffers (e.g. bitmaps). Must be a power of 2. */
#ifndef BYTE_ALIGN
	#define BYTE_ALIGN 4 /* Bytes to align pixel buffers to. */
	/* #include <stddef.h> */
	/* #define BYTE_ALIGN (sizeof(size_t)) */
#endif /* BYTE_ALIGN */

#if BYTE_ALIGN == 0
	/* No alignment needed. */
    #define ADD_PADDING(width) (width)
#else
	/* Aligns given width to padding. */
    // bugged version which overpads? #define ADD_PADDING(width) (BYTE_ALIGN + (((width) - 1) & ~(BYTE_ALIGN - 1)))
	#define ADD_PADDING(width) (((width) + BYTE_ALIGN - 1) & ~(BYTE_ALIGN - 1))
#endif

#if defined(IS_WINDOWS)
    #if defined (_WIN64)
        #define RobotGo_64
    #else
        #define RobotGo_32
    #endif
#else
    #if defined (__x86_64__)
        #define RobotGo_64
    #else
        #define RobotGo_32
    #endif
#endif

// ---------------------
// Display Server Detection (Runtime)
// ---------------------
enum class DisplayServer {
    Wayland,
    X11,
    Unknown
};

inline DisplayServer detectDisplayServer() {
#if defined(IS_LINUX)
    const char* wayland = std::getenv("WAYLAND_DISPLAY");
    const char* x11 = std::getenv("DISPLAY");

    if (wayland && *wayland != '\0') {
        return DisplayServer::Wayland;
    } else if (x11 && *x11 != '\0') {
        return DisplayServer::X11;
    } else {
        return DisplayServer::Unknown;
    }
#else
    return DisplayServer::Unknown;
#endif
}


#endif /* OS_H */
