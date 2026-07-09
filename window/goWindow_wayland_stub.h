#pragma once

#include "../base/types.h"
#include <stdbool.h>
#include <stdint.h>

typedef struct _MData {
	uintptr XWin;
} MData;

typedef struct _Bounds {
	int32_t X;
	int32_t Y;
	int32_t W;
	int32_t H;
} Bounds;

static MData pub_mData = {0};

static inline bool setHandle(uintptr handle) {
	pub_mData.XWin = handle;
	return true;
}

static inline bool is_valid(void) {
	return false;
}

static inline MData set_handle_pid(uintptr pid, int8_t isPid) {
	(void)isPid;
	MData m = {0};
	m.XWin = pid;
	return m;
}

static inline void set_handle_pid_mData(uintptr pid, int8_t isPid) {
	pub_mData = set_handle_pid(pid, isPid);
}

static inline void set_active(const MData win) {
	pub_mData = win;
}

static inline MData get_active(void) {
	return pub_mData;
}

static inline void min_window(uintptr pid, bool state, int8_t isPid) {
	(void)pid;
	(void)state;
	(void)isPid;
}

static inline void max_window(uintptr pid, bool state, int8_t isPid) {
	(void)pid;
	(void)state;
	(void)isPid;
}

static inline uintptr get_handle(void) {
	return pub_mData.XWin;
}

static inline uintptr b_get_handle(void) {
	return pub_mData.XWin;
}

static inline void active_PID(uintptr pid, int8_t isPid) {
	(void)pid;
	(void)isPid;
}

static inline void close_main_window(void) {}

static inline void close_window_by_PId(uintptr pid, int8_t isPid) {
	(void)pid;
	(void)isPid;
}

static inline char* get_main_title(void) {
	return "";
}

static inline char* get_title_by_pid(uintptr pid, int8_t isPid) {
	(void)pid;
	(void)isPid;
	return "";
}

static inline int32_t get_PID(void) {
	return 0;
}

static inline Bounds get_bounds(uintptr pid, int8_t isPid) {
	(void)pid;
	(void)isPid;
	Bounds b = {0};
	return b;
}

static inline Bounds get_client(uintptr pid, int8_t isPid) {
	(void)pid;
	(void)isPid;
	Bounds b = {0};
	return b;
}

static inline bool Is64Bit(void) {
	return sizeof(void*) == 8;
}
