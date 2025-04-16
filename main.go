//go:build windows

package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall" // Import syscall for Windows API
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const lockPort = "34115"     // Port used for single instance check
const appTitle = "lhcontrol" // Define app title constant

// Windows API constants (from winuser.h)
const (
	SW_RESTORE    = 9
	SW_SHOWNORMAL = 1
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procFlashWindowEx       = user32.NewProc("FlashWindowEx")
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

// findWindow finds a window by title.
func findWindow(title string) (syscall.Handle, error) {
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

// setForegroundWindow brings a window to the foreground.
func setForegroundWindow(hwnd syscall.Handle) bool {
	ret, _, _ := procSetForegroundWindow.Call(uintptr(hwnd))
	return ret != 0
}

// showWindow changes the visibility state of a window.
func showWindow(hwnd syscall.Handle, cmdshow int) bool {
	ret, _, _ := procShowWindow.Call(uintptr(hwnd), uintptr(cmdshow))
	return ret != 0
}

// flashWindowEx flashes the window using FlashWindowEx.
func flashWindowEx(hwnd syscall.Handle, flags uint32, count uint32, timeout uint32) bool {
	var fi FLASHWINFO
	fi.CbSize = uint32(unsafe.Sizeof(fi))
	fi.Hwnd = hwnd
	fi.DwFlags = flags
	fi.UCout = count
	fi.DwTimeout = timeout

	ret, _, _ := procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
	return ret != 0
}

// BringWindowToFront finds the existing window, tries to set foreground, and flashes it (Windows specific)
func BringWindowToFront() {
	hwnd, err := findWindow(appTitle)
	if err != nil {
		log.Printf("Error finding window: %v", err)
		return
	}
	if hwnd == 0 {
		log.Println("Existing window not found.")
		return
	}

	// Try restoring and setting foreground first
	showWindow(hwnd, SW_RESTORE)    // Restore if minimized
	if !setForegroundWindow(hwnd) { // Attempt to set foreground
		// If SetForegroundWindow fails, flash the window
		log.Println("SetForegroundWindow failed (maybe window is not allowed to take focus?). Flashing instead.")
		flashWindowEx(hwnd, FLASHW_ALL|FLASHW_TIMERNOFG, 0, 0) // Flash indefinitely until focus
	} else {
		log.Println("SetForegroundWindow succeeded.")
		// Optional: Maybe stop flashing if it was started? But SetForegroundWindow should take precedence.
	}
}

// setupLogging configures logging to write to both console and a file.
// Assumes it's only called when file logging is desired.
func setupLogging() (*os.File, error) {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("ERROR getting executable path: %v", err)
		return nil, err
	}
	exeDir := filepath.Dir(exePath)
	logFilePath := filepath.Join(exeDir, "lhcontrol.log")

	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
	if err != nil {
		log.Printf("ERROR opening log file '%s': %v", logFilePath, err)
		return nil, err
	}

	// Write logs to both Stdout and the log file
	logWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(logWriter)
	// Flags are set in main before calling this

	log.Println("-----------------------------------------")
	log.Printf("File logging enabled. Log file: %s", logFilePath)
	log.Println("-----------------------------------------")

	return logFile, nil
}

func main() {
	// Define command-line flag for logging
	logToFile := flag.Bool("log", false, "Enable file logging to lhcontrol.log")
	flag.Parse() // Parse command line arguments

	// Setup standard logger flags (applies to console and potentially file)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Setup file logging only if requested
	var logFile *os.File
	if *logToFile {
		var errLog error
		logFile, errLog = setupLogging()
		if errLog != nil {
			log.Printf("Error setting up file logging, continuing with console only: %v", errLog)
			logFile = nil // Ensure logFile is nil if setup failed
		} else {
			// IMPORTANT: Defer close only if file was successfully opened
			defer func() {
				log.Println("Closing log file handle...")
				logFile.Sync() // Sync before close
				logFile.Close()
			}()
		}
	} else {
		log.Println("File logging disabled. Use -log flag to enable.")
	}

	// Attempt to acquire the instance lock
	lockAddr := fmt.Sprintf("127.0.0.1:%s", lockPort)
	listener, err := net.Listen("tcp", lockAddr)
	if err != nil {
		if strings.Contains(err.Error(), "address already in use") || strings.Contains(err.Error(), "bind: address already in use") || strings.Contains(err.Error(), "bind: Only one usage of each socket address") {
			log.Println("Application is already running. Bringing existing window to front...")
			BringWindowToFront()
			if logFile != nil {
				logFile.Sync()
			} // Sync before exit, only if file exists
			os.Exit(0)
		} else {
			log.Printf("FATAL: Failed to acquire instance lock on port %s: %v", lockPort, err)
			if logFile != nil {
				logFile.Sync()
			} // Sync before exit, only if file exists
			os.Exit(1)
		}
	}
	defer listener.Close()
	log.Printf("Acquired instance lock on port %s", lockPort)

	// Create app
	app := NewApp()

	err = wails.Run(&options.App{
		Title:         appTitle, // Use constant
		Width:         512,
		Height:        800,
		DisableResize: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Println("FATAL: Error running Wails app: ", err.Error())
		if logFile != nil {
			logFile.Sync()
		} // Sync before exit, only if file exists
		os.Exit(1)
	}
	log.Println("Application exited cleanly.")
	// Sync on clean exit is handled by the defer if logFile != nil
}
