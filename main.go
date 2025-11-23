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

	"lhcontrol/internal/platform"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const lockPort = "34115"     // Port used for single instance check
const appTitle = "lhcontrol" // Define app title constant

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
			platform.BringWindowToFront(appTitle)
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
