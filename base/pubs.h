#if defined(IS_WINDOWS)
    static BOOL CALLBACK MonitorEnumProc(HMONITOR hMonitor, HDC hdcMonitor, LPRECT lprcMonitor, LPARAM dwData) {
        uint32_t *count = (uint32_t*)dwData;
        (*count)++;
        return TRUE;
    }
   
    typedef struct{
	    HWND hWnd;
	    DWORD dwPid;
	}WNDINFO;

    static BOOL CALLBACK EnumWindowsProc(HWND hWnd, LPARAM lParam){
	    WNDINFO* pInfo = (WNDINFO*)lParam;
	    DWORD dwProcessId = 0;
	    GetWindowThreadProcessId(hWnd, &dwProcessId);

	    if (dwProcessId == pInfo->dwPid) {
	        pInfo->hWnd = hWnd;
	        return FALSE;
	    }
	    return TRUE;
	}

    static inline HWND GetHwndByPid(DWORD dwProcessId) {
	    WNDINFO info = {0};
	    info.hWnd = NULL;
	    info.dwPid = dwProcessId;
	    EnumWindows(EnumWindowsProc, (LPARAM)&info);

	    return info.hWnd;
	}
#endif
