// #include "os.h"
#if defined(IS_MACOSX)
	#include <CoreFoundation/CoreFoundation.h>
#endif

enum robotgo_alert_status {
	ROBOTGO_ALERT_FAILED = -1,
	ROBOTGO_ALERT_ACCEPTED = 0,
	ROBOTGO_ALERT_REJECTED = 1
};

#if defined(IS_MACOSX)
// Use a static inline helper to avoid duplicate symbol definitions when this
// header is included by multiple translation units.
static inline CFStringRef robotgo_CFStringCreateWithUTF8String(const char *title) {
	if (title == NULL) { return NULL; }
	return CFStringCreateWithCString(NULL, title, kCFStringEncodingUTF8);
}
#endif

static inline int showAlert(const char *title, const char *msg, 
		const char *defaultButton, const char *cancelButton) {
	#if defined(IS_MACOSX)
		CFStringRef alertHeader = robotgo_CFStringCreateWithUTF8String(title);
		CFStringRef alertMessage = robotgo_CFStringCreateWithUTF8String(msg);
		CFStringRef defaultButtonTitle = robotgo_CFStringCreateWithUTF8String(defaultButton);
		CFStringRef cancelButtonTitle = robotgo_CFStringCreateWithUTF8String(cancelButton);
		CFOptionFlags responseFlags;
		
		SInt32 err = CFUserNotificationDisplayAlert(
			0.0, kCFUserNotificationNoteAlertLevel, NULL, NULL, NULL, alertHeader, alertMessage,
			defaultButtonTitle, cancelButtonTitle, NULL, &responseFlags);
												
		if (alertHeader != NULL) CFRelease(alertHeader);
		if (alertMessage != NULL) CFRelease(alertMessage);
		if (defaultButtonTitle != NULL) CFRelease(defaultButtonTitle);
		if (cancelButtonTitle != NULL) CFRelease(cancelButtonTitle);

		if (err != 0) { return ROBOTGO_ALERT_FAILED; }
		return (responseFlags == kCFUserNotificationDefaultResponse) ?
			ROBOTGO_ALERT_ACCEPTED : ROBOTGO_ALERT_REJECTED;
	#elif defined(IS_LINUX)
		return ROBOTGO_ALERT_ACCEPTED;
	#else
		/* TODO: Display custom buttons instead of the pre-defined "OK" and "Cancel". */
		int response = MessageBox(NULL, msg, title,
								(cancelButton == NULL) ? MB_OK : MB_OKCANCEL );
		if (response == 0) { return ROBOTGO_ALERT_FAILED; }
		return (response == IDOK) ? ROBOTGO_ALERT_ACCEPTED : ROBOTGO_ALERT_REJECTED;
	#endif
}
