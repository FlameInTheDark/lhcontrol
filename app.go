package main

import (
	"context"
	"fmt"
	"log"

	"lhcontrol/internal/bluetooth"
	"lhcontrol/internal/config"
	"lhcontrol/internal/station"

	"github.com/gofiber/fiber/v2"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	config         *config.Config
	stationManager *station.Manager
	api            *fiber.App
}

// NewApp creates a new App application struct
func NewApp() *App {
	cfg := config.NewConfig()
	mgr := station.NewManager(cfg)
	return &App{
		config:         cfg,
		stationManager: mgr,
		api:            fiber.New(),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Use standard logger (already configured in main)
	log.Println("-----------------------------------------")
	log.Println("Application startup initiated.")
	log.Println("-----------------------------------------")

	if err := a.stationManager.Initialize(); err != nil {
		log.Printf("Error initializing Bluetooth: %v", err)
	}

	if err := a.config.Load(); err != nil {
		log.Printf("Error loading config: %v", err)
	}

	// Setup API routes
	a.api.Post("/allon", func(c *fiber.Ctx) error {
		// Use goroutine to avoid blocking API response while BT operation runs
		go func() {
			if err := a.stationManager.PowerOnAllStations(); err != nil {
				log.Printf("API PowerOnAllStations error: %v", err)
			}
		}()
		return c.SendStatus(fiber.StatusOK)
	})
	a.api.Post("/alloff", func(c *fiber.Ctx) error {
		// Use goroutine to avoid blocking API response while BT operation runs
		go func() {
			if err := a.stationManager.PowerOffAllStations(); err != nil {
				log.Printf("API PowerOffAllStations error: %v", err)
			}
		}()
		return c.SendStatus(fiber.StatusOK)
	})
	// Add new GET /status endpoint
	a.api.Get("/status", func(c *fiber.Ctx) error {
		log.Println("API: Received GET /status request")
		currentStations := a.GetCurrentStationInfo() // Get current data
		log.Printf("API: Returning status for %d stations", len(currentStations))
		return c.JSON(currentStations)
	})
	// Add new POST /scan endpoint
	a.api.Post("/scan", func(c *fiber.Ctx) error {
		log.Println("API: Received POST /scan request")
		// Run scan in background to avoid blocking API response
		go func() {
			stations, err := a.ScanAndFetchStations()
			if err != nil {
				// Log error using standard logger (API goroutine might not have Wails context)
				log.Printf("API: Error during background scan triggered by API: %v", err)
			} else {
				log.Println("API: Background scan triggered by API completed.")
				// Emit an event to notify the frontend that a scan has completed
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "external-scan-completed", stations)
					log.Println("API: Emitted external-scan-completed event")
				}
			}
		}()
		// Return 202 Accepted immediately
		return c.SendStatus(fiber.StatusAccepted)
	})
	// Start API server in a goroutine
	go func() {
		if err := a.api.Listen("127.0.0.1:7575"); err != nil {
			log.Printf("Error starting API server: %v", err)
		}
	}()

	log.Println("Startup sequence complete.")
}

// --- Bluetooth Methods exposed to Wails --- //

func (a *App) ScanAndFetchStations() ([]station.StationInfo, error) {
	return a.stationManager.ScanAndFetchStations()
}

func (a *App) IsScanning() bool {
	return a.stationManager.IsScanning()
}

func (a *App) CheckAllStationStatuses() ([]station.StationInfo, error) {
	return a.stationManager.CheckAllStationStatuses()
}

func (a *App) GetCurrentStationInfo() []station.StationInfo {
	return a.stationManager.GetStationInfo()
}

func (a *App) PowerOnStation(address string) error {
	log.Printf("Requesting Power ON for address %s", address)
	return a.stationManager.PowerOnStation(address)
}

func (a *App) PowerOffStation(address string) error {
	log.Printf("Requesting Power OFF for address %s", address)
	return a.stationManager.PowerOffStation(address)
}

func (a *App) PowerOnAllStations() error {
	return a.stationManager.PowerOnAllStations()
}

func (a *App) PowerOffAllStations() error {
	return a.stationManager.PowerOffAllStations()
}

func (a *App) RenameStation(originalName string, newName string) error {
	log.Printf("Renaming %s to %s", originalName, newName)
	return a.stationManager.RenameStation(originalName, newName)
}

func (a *App) SaveConfig() error {
	return a.config.Save()
}

// shutdown is called when the app terminates.
func (a *App) shutdown(ctx context.Context) {
	log.Println("App shutdown requested. Cleaning up...")
	if a.api != nil {
		log.Println("Shutting down API server...")
		if err := a.api.Shutdown(); err != nil {
			log.Printf("Error shutting down API server: %v", err)
		}
	}
	log.Println("Requesting disconnect for all stations...")
	bluetooth.DisconnectAllStations()
	log.Println("App shutdown sequence complete.")
}

// Greet (Example method - can be kept or removed)
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
