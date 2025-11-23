//go:build windows

package windows

import (
	"log"
	"syscall"
	"unsafe"
)

// Windows API constants (from winuser.h)
const (
	SW_RESTORE    = 9
	SW_SHOWNORMAL = 1
)

// FLASHW flags
const (
	FLASHW_STOP      = 0
	FLASHW_CAPTION   = 0x00000001
	FLASHW_TRAY      = 0x00000002
	FLASHW_ALL       = FLASHW_CAPTION | FLASHW_TRAY
	FLASHW_TIMER     = 0x00000004
	FLASHW_TIMERNOFG = 0x0000000C
)

// FLASHWINFO struct
type FLASHWINFO struct {
	CbSize    uint32
	Hwnd      syscall.Handle
	DwFlags   uint32
	UCout     uint32
	DwTimeout uint32
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procFlashWindowEx       = user32.NewProc("FlashWindowEx")
)

// FindWindow finds a window by title.
func FindWindow(title string) (syscall.Handle, error) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	hwnd, _, err := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	// Check for specific error "Invalid window handle." which means not found
	if err != nil && err.Error() != "The operation completed successfully." {
		// Check if error indicates not found (this might vary, often it's just HWND=0 with success error)
		if hwnd == 0 { // Best check is often just if handle is zero
			return 0, nil // Not found, but not an API error
		}
		return 0, err // Actual API error
	}
	return syscall.Handle(hwnd), nil
}

// SetForegroundWindow brings a window to the foreground.
func SetForegroundWindow(hwnd syscall.Handle) bool {
	ret, _, _ := procSetForegroundWindow.Call(uintptr(hwnd))
	return ret != 0
}

// ShowWindow changes the visibility state of a window.
func ShowWindow(hwnd syscall.Handle, cmdshow int) bool {
	ret, _, _ := procShowWindow.Call(uintptr(hwnd), uintptr(cmdshow))
	return ret != 0
}

// FlashWindowEx flashes the window using FlashWindowEx.
func FlashWindowEx(hwnd syscall.Handle, flags uint32, count uint32, timeout uint32) bool {
	var fi FLASHWINFO
	fi.CbSize = uint32(unsafe.Sizeof(fi))
	fi.Hwnd = hwnd
	fi.DwFlags = flags
	fi.UCout = count
	fi.DwTimeout = timeout

	ret, _, _ := procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
	return ret != 0
}

// BringWindowToFront finds the existing window, tries to set foreground, and flashes it
func BringWindowToFront(windowTitle string) {
	hwnd, err := FindWindow(windowTitle)
	if err != nil {
		log.Printf("Error finding window: %v", err)
		return
	}
	if hwnd == 0 {
		log.Println("Existing window not found.")
		return
	}

	// Try restoring and setting foreground first
	ShowWindow(hwnd, SW_RESTORE)    // Restore if minimized
	if !SetForegroundWindow(hwnd) { // Attempt to set foreground
		// If SetForegroundWindow fails, flash the window
		log.Println("SetForegroundWindow failed (maybe window is not allowed to take focus?). Flashing instead.")
		FlashWindowEx(hwnd, FLASHW_ALL|FLASHW_TIMERNOFG, 0, 0) // Flash indefinitely until focus
	} else {
		log.Println("SetForegroundWindow succeeded.")
		// Optional: Maybe stop flashing if it was started? But SetForegroundWindow should take precedence.
	}
}
