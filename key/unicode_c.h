/* Native Unicode text helpers. Included once by keypress_c.h after its
 * platform event helpers and ownership lock have been defined. */
#if defined(IS_MACOSX) || defined(IS_WINDOWS)
static size_t ROBOTGO_UTF16_UNITS(unsigned value, uint16_t units[2]) {
	if (value > 0x10ffff || (value >= 0xd800 && value <= 0xdfff)) {
		return 0;
	}
	if (value < 0x10000) {
		units[0] = (uint16_t)value;
		return 1;
	}
	value -= 0x10000;
	units[0] = (uint16_t)(0xd800 | (value >> 10));
	units[1] = (uint16_t)(0xdc00 | (value & 0x3ff));
	return 2;
}
#endif

#if defined(IS_MACOSX)
static CGEventRef ROBOTGO_MAC_UNICODE_EVENT(CGEventSourceRef source,
	const UniChar *units, UniCharCount length, bool down) {
	CGEventRef event = CGEventCreateKeyboardEvent(source, 0, down);
	if (event != NULL) {
		CGEventKeyboardSetUnicodeString(event, length, units);
	}
	return event;
}

/* Retain the legacy C-facing signature; checked text uses the transaction
 * below so both events exist before the first one is posted. */
void toggleUnicode(UniChar ch, const bool down, uintptr pid) {
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	if (source == NULL) {
		return;
	}
	CGEventRef event = ROBOTGO_MAC_UNICODE_EVENT(source, &ch, 1, down);
	if (event != NULL) {
		SendTo(pid, event);
	}
	CFRelease(source);
}

static int ROBOTGO_NATIVE_UNICODE_TYPE(unsigned value, uintptr pid, int8_t isPid) {
	(void)isPid;
	uint16_t encoded[2];
	size_t length = ROBOTGO_UTF16_UNITS(value, encoded);
	if (length == 0) {
		return ROBOTGO_KEY_INVALID;
	}
	/* Copy by value instead of aliasing uint16_t storage as an SDK typedef. */
	UniChar units[2] = { encoded[0], length == 2 ? encoded[1] : 0 };
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	if (source == NULL) {
		return ROBOTGO_KEY_INJECTION_FAILED;
	}
	CGEventRef down = ROBOTGO_MAC_UNICODE_EVENT(source, units, (UniCharCount)length, true);
	CGEventRef up = NULL;
	if (down != NULL) {
		up = ROBOTGO_MAC_UNICODE_EVENT(source, units, (UniCharCount)length, false);
	}
	CFRelease(source);
	if (down == NULL || up == NULL) {
		if (down != NULL) { CFRelease(down); }
		return ROBOTGO_KEY_INJECTION_FAILED;
	}
	SendTo(pid, down);
	microsleep(5.0);
	SendTo(pid, up);
	return ROBOTGO_KEY_OK;
}
#elif defined(IS_WINDOWS)
/* At most two Unicode downs can survive one partial transaction. No new text
 * is sent until these owned releases succeed. The existing keyboard lock
 * protects this ledger and CloseMainDisplayE retries it via the cleanup seam. */
static INPUT robotgo_win32_unicode_releases[2];
static UINT robotgo_win32_unicode_release_count;

/* Caller holds robotgo_win32_toggle_lock. Retain every unaccepted release. */
static int ROBOTGO_WIN32_RELEASE_UNICODE(void) {
	UINT count = robotgo_win32_unicode_release_count;
	if (count == 0) {
		return ROBOTGO_KEY_OK;
	}
	UINT sent = SendInput(count, robotgo_win32_unicode_releases, sizeof(INPUT));
	if (sent > count) { sent = count; }
	robotgo_win32_unicode_release_count = count - sent;
	memmove(robotgo_win32_unicode_releases,
		robotgo_win32_unicode_releases + sent, (count - sent) * sizeof(INPUT));
	return sent == count ? ROBOTGO_KEY_OK : ROBOTGO_KEY_INJECTION_FAILED;
}

static int ROBOTGO_WIN32_RELEASE_OWNED_UNICODE(void) {
	AcquireSRWLockExclusive(&robotgo_win32_toggle_lock);
	int status = ROBOTGO_WIN32_RELEASE_UNICODE();
	ReleaseSRWLockExclusive(&robotgo_win32_toggle_lock);
	return status;
}

static int ROBOTGO_NATIVE_UNICODE_TYPE(unsigned value, uintptr pid, int8_t isPid) {
	uint16_t units[2];
	size_t length = ROBOTGO_UTF16_UNITS(value, units);
	if (length == 0) {
		return ROBOTGO_KEY_INVALID;
	}
	AcquireSRWLockExclusive(&robotgo_win32_toggle_lock);
	int status = ROBOTGO_WIN32_RELEASE_UNICODE();
	if (status != ROBOTGO_KEY_OK) {
		ReleaseSRWLockExclusive(&robotgo_win32_toggle_lock);
		return status;
	}
	if (pid != 0) {
		HWND hwnd = getHwnd(pid, isPid);
		/* NULL would post to our own thread queue, not the requested target. */
		if (hwnd == NULL) {
			status = ROBOTGO_KEY_INVALID;
		} else {
			for (size_t index = 0; index < length; index++) {
				if (!PostMessageW(hwnd, WM_CHAR, units[index], 0)) {
					status = ROBOTGO_KEY_INJECTION_FAILED;
					break;
				}
			}
		}
		ReleaseSRWLockExclusive(&robotgo_win32_toggle_lock);
		return status;
	}
	INPUT input[4];
	memset(input, 0, sizeof(input));
	for (size_t index = 0; index < length; index++) {
		input[index].type = input[index + length].type = INPUT_KEYBOARD;
		input[index].ki.wScan = input[index + length].ki.wScan = units[index];
		input[index].ki.dwFlags = KEYEVENTF_UNICODE;
		input[index + length].ki.dwFlags = KEYEVENTF_UNICODE | KEYEVENTF_KEYUP;
	}
	UINT count = (UINT)(2 * length);
	UINT sent = SendInput(count, input, sizeof(INPUT));
	if (sent < count) {
		UINT downs = sent < length ? sent : (UINT)length;
		UINT ups = sent > length ? sent - (UINT)length : 0;
		for (UINT index = ups; index < downs; index++) {
			robotgo_win32_unicode_releases[robotgo_win32_unicode_release_count++] =
				input[index + length];
		}
		(void)ROBOTGO_WIN32_RELEASE_UNICODE();
		/* Cleanup success does not turn a partial input into success. Failed
		 * cleanup remains owned for a later Unicode call or checked close. */
		status = ROBOTGO_KEY_INJECTION_FAILED;
	}
	ReleaseSRWLockExclusive(&robotgo_win32_toggle_lock);
	return status;
}
#endif
