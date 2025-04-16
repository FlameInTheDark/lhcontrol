package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"lhcontrol/internal/bluetooth"

	"github.com/gofiber/fiber/v2"
	// "github.com/wailsapp/wails/v2/pkg/runtime"
)

// StationInfo is a simplified representation of a BaseStation for the frontend.
type StationInfo struct {
	Name         string `json:"name"`
	OriginalName string `json:"originalName"`
	Address      string `json:"address"`
	PowerState   int    `json:"powerState"`
}

type Config struct {
	RenamedStations map[string]string `json:"renamedStations"`
}

// App struct
type App struct {
	ctx context.Context
	// Use a map again to store stations keyed by address string for persistence
	stations      map[string]*bluetooth.BaseStation
	stationsMutex sync.RWMutex
	config        Config

	api *fiber.App
}

// NewApp creates a new App application struct
func NewApp() *App {
	// Initialize the map
	return &App{
		stations: make(map[string]*bluetooth.BaseStation),
		api:      fiber.New(),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	err := bluetooth.Initialize()
	if err != nil {
		log.Printf("Error initializing Bluetooth: %v", err)
	}

	a.config.RenamedStations = make(map[string]string)

	// Load renamed stations from file
	configFile, err := os.ReadFile("config.json")
	if err != nil {
		log.Printf("Error reading config file: %v", err)
	}

	err = json.Unmarshal(configFile, &a.config)
	if err != nil {
		log.Printf("Error unmarshalling config: %v", err)
	}

	a.api.Post("/allon", func(c *fiber.Ctx) error {
		a.PowerOnAllStations()
		return c.SendStatus(fiber.StatusOK)
	})

	a.api.Post("/alloff", func(c *fiber.Ctx) error {
		a.PowerOffAllStations()
		return c.SendStatus(fiber.StatusOK)
	})

	go log.Fatal(a.api.Listen("127.0.0.1:7575"))
	// No explicit shutdown needed for bluetooth package anymore
}

// --- Bluetooth Methods --- //

// ScanAndFetchStations performs a scan, updates the persistent station map,
// fetches initial states for newly discovered or disconnected stations, and returns the full list.
func (a *App) ScanAndFetchStations() ([]StationInfo, error) {
	scanDuration := 5 * time.Second
	fetchWaitDuration := 7 * time.Second // Time to wait for FetchInitialPowerState goroutines

	log.Println("App: Adding 1s delay before starting scan...")
	time.Sleep(1 * time.Second)

	log.Printf("App: Starting blocking scan for %v...", scanDuration)
	discoveredValues, err := bluetooth.ScanForDuration(scanDuration)
	if err != nil {
		log.Printf("App: Error during blocking scan: %v", err)
		// Don't clear the map on scan error
		return a.GetCurrentStationInfo(), fmt.Errorf("bluetooth scan failed: %w", err)
	}
	log.Printf("App: Blocking scan finished, found %d devices in this scan.", len(discoveredValues))

	// --- Update persistent map and identify stations to fetch state for --- //
	stationsToFetch := make([]*bluetooth.BaseStation, 0, len(discoveredValues))
	a.stationsMutex.Lock() // Lock map for updates
	log.Printf("App: Updating persistent station map...")
	for _, currentScanStation := range discoveredValues {
		addrStr := currentScanStation.Address.String()
		if existingStation, found := a.stations[addrStr]; found {
			// Station already exists, update name if needed
			if existingStation.Name != currentScanStation.Name {
				log.Printf("App: Updating name for %s from %s to %s", addrStr, existingStation.Name, currentScanStation.Name)
				existingStation.Name = currentScanStation.Name
			}
			// Check if we need to fetch state (if not currently connected)
			if !existingStation.IsConnected() {
				log.Printf("App: Existing station %s found, but not connected. Queuing for state fetch.", addrStr)
				stationsToFetch = append(stationsToFetch, existingStation)
			}
		} else {
			// New station found
			log.Printf("App: Adding new station %s (%s) to map.", currentScanStation.Name, addrStr)
			newStationPtr := new(bluetooth.BaseStation)
			*newStationPtr = currentScanStation // Copy data
			a.stations[addrStr] = newStationPtr
			stationsToFetch = append(stationsToFetch, newStationPtr) // Fetch state for new stations
		}
	}
	a.stationsMutex.Unlock() // Unlock map

	// --- Launch fetch routines for stations needing state check --- //
	var wg sync.WaitGroup
	if len(stationsToFetch) > 0 {
		log.Printf("App: Launching state fetch routines for %d stations...", len(stationsToFetch))
		for _, stationToFetch := range stationsToFetch {
			wg.Add(1)
			go func(ptr *bluetooth.BaseStation) {
				defer wg.Done()
				bluetooth.FetchInitialPowerState(ptr) // This attempts connection and state read
			}(stationToFetch)
		}

		// Wait for fetch routines to complete (with timeout)
		waitChan := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitChan)
		}()

		log.Printf("App: Waiting up to %v for state fetch routines...", fetchWaitDuration)
		select {
		case <-waitChan:
			log.Println("App: All state fetch routines completed.")
		case <-time.After(fetchWaitDuration):
			log.Printf("App: Warning - Timed out waiting for state fetch routines after %v.", fetchWaitDuration)
		}
	}

	// --- Generate result for frontend from the full map --- //
	log.Println("App: Scan and update process complete. Returning full station info map.")
	return a.GetCurrentStationInfo(), nil
}

// GetCurrentStationInfo returns the current state of the stations map held by the app.
func (a *App) GetCurrentStationInfo() []StationInfo {
	a.stationsMutex.RLock()
	defer a.stationsMutex.RUnlock()

	stationInfos := make([]StationInfo, 0, len(a.stations))
	// Iterate through the map
	for _, stationPtr := range a.stations {
		if stationPtr != nil {
			var name string
			if renamedName, ok := a.config.RenamedStations[stationPtr.Name]; ok {
				name = renamedName
			} else {
				name = stationPtr.Name
			}
			stationInfos = append(stationInfos, StationInfo{
				Name:         name,
				OriginalName: stationPtr.Name,
				Address:      stationPtr.Address.String(),
				PowerState:   stationPtr.PowerState,
			})
		} else {
			log.Printf("App: Warning - Nil pointer found in stations map during GetCurrentStationInfo")
		}
	}
	return stationInfos
}

// Helper to find station pointer by address in the App's map
func (a *App) findStationPtrByAddress(address string) *bluetooth.BaseStation {
	a.stationsMutex.RLock() // Use read lock for searching
	defer a.stationsMutex.RUnlock()
	stationPtr, _ := a.stations[address] // Returns nil if not found
	return stationPtr
}

// PowerOnStation turns a specific base station on.
func (a *App) PowerOnStation(address string) error {
	stationPtr := a.findStationPtrByAddress(address)
	if stationPtr == nil {
		return fmt.Errorf("station with address %s not found in App map", address)
	}
	// Pass the POINTER to the bluetooth function
	return bluetooth.PowerOn(stationPtr)
}

// PowerOffStation turns a specific base station off.
func (a *App) PowerOffStation(address string) error {
	stationPtr := a.findStationPtrByAddress(address)
	if stationPtr == nil {
		return fmt.Errorf("station with address %s not found in App map", address)
	}
	// Pass the POINTER to the bluetooth function
	return bluetooth.PowerOff(stationPtr)
}

// PowerOnAllStations attempts to turn ON all known base stations concurrently.
func (a *App) PowerOnAllStations() error {
	a.stationsMutex.RLock() // Read lock to get the list of stations
	stationsToToggle := make([]*bluetooth.BaseStation, 0, len(a.stations))
	for _, stationPtr := range a.stations {
		if stationPtr != nil {
			stationsToToggle = append(stationsToToggle, stationPtr)
		}
	}
	a.stationsMutex.RUnlock()

	log.Printf("App: Attempting to power ON %d stations...", len(stationsToToggle))
	var wg sync.WaitGroup
	errors := make(map[string]error)
	var errorMutex sync.Mutex

	for _, stationPtr := range stationsToToggle {
		wg.Add(1)
		go func(s *bluetooth.BaseStation) {
			defer wg.Done()
			err := bluetooth.PowerOn(s) // Call the existing PowerOn function
			if err != nil {
				log.Printf("App: Error powering ON %s: %v", s.Name, err)
				errorMutex.Lock()
				errors[s.Address.String()] = err
				errorMutex.Unlock()
			}
		}(stationPtr)
	}

	wg.Wait()
	log.Printf("App: Finished power ON attempts for all stations.")

	if len(errors) > 0 {
		// Combine errors? Or just indicate failure?
		// For now, just log and return a generic error if any failed.
		return fmt.Errorf("encountered %d error(s) during PowerOnAllStations", len(errors))
	}
	return nil // Success if no errors recorded
}

// PowerOffAllStations attempts to turn OFF all known base stations concurrently.
func (a *App) PowerOffAllStations() error {
	a.stationsMutex.RLock()
	stationsToToggle := make([]*bluetooth.BaseStation, 0, len(a.stations))
	for _, stationPtr := range a.stations {
		if stationPtr != nil {
			stationsToToggle = append(stationsToToggle, stationPtr)
		}
	}
	a.stationsMutex.RUnlock()

	log.Printf("App: Attempting to power OFF %d stations...", len(stationsToToggle))
	var wg sync.WaitGroup
	errors := make(map[string]error)
	var errorMutex sync.Mutex

	for _, stationPtr := range stationsToToggle {
		wg.Add(1)
		go func(s *bluetooth.BaseStation) {
			defer wg.Done()
			err := bluetooth.PowerOff(s) // Call the existing PowerOff function
			if err != nil {
				log.Printf("App: Error powering OFF %s: %v", s.Name, err)
				errorMutex.Lock()
				errors[s.Address.String()] = err
				errorMutex.Unlock()
			}
		}(stationPtr)
	}

	wg.Wait()
	log.Printf("App: Finished power OFF attempts for all stations.")

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d error(s) during PowerOffAllStations", len(errors))
	}
	return nil
}

// rename function with saving to config
func (a *App) RenameStation(originalName string, newName string) error {
	if newName == "" {
		// If newName is empty, remove the entry from the map to reset
		delete(a.config.RenamedStations, originalName)
		log.Printf("App: Resetting custom name for %s", originalName)
	} else {
		// Otherwise, save the new name
		a.config.RenamedStations[originalName] = newName
		log.Printf("App: Renaming %s to %s", originalName, newName)
	}
	return a.SaveConfig()
}

func (a *App) SaveConfig() error {
	configFile, err := json.Marshal(a.config)
	if err != nil {
		return fmt.Errorf("error marshalling config: %w", err)
	}
	return os.WriteFile("config.json", configFile, 0644)
}

// shutdown is called when the app terminates.
// It disconnects any active Bluetooth connections by iterating the map.
func (a *App) shutdown(ctx context.Context) {
	log.Println("App: Shutdown requested. Disconnecting all stations...")
	a.api.Shutdown()
	a.stationsMutex.Lock() // Lock for iterating
	stationsToDisconnect := make([]*bluetooth.BaseStation, 0, len(a.stations))
	for _, stationPtr := range a.stations {
		if stationPtr != nil && stationPtr.IsConnected() { // Check connection status
			stationsToDisconnect = append(stationsToDisconnect, stationPtr)
		}
	}
	a.stationsMutex.Unlock() // Unlock before calling disconnect functions

	log.Printf("App: Found %d connected stations to disconnect.", len(stationsToDisconnect))
	for _, stationPtr := range stationsToDisconnect {
		log.Printf("App: Requesting disconnect for %s", stationPtr.Name)
		bluetooth.DisconnectStation(stationPtr)
	}

	log.Println("App: Disconnect all stations completed.")
}

// Greet (Example method - can be kept or removed)
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
