package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lhcontrol/internal/bluetooth"

	"github.com/gofiber/fiber/v2"
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
	api           *fiber.App
	isScanning    bool // Flag to indicate if ScanAndFetchStations is running
}

// NewApp creates a new App application struct
func NewApp() *App {
	// Initialize the map
	return &App{
		stations: make(map[string]*bluetooth.BaseStation),
		api:      fiber.New(),
	}
}

// Helper function to get the full path to the config file
func getConfigPathForConfig() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config dir: %w", err)
	}
	appConfigDir := filepath.Join(configDir, "lhcontrol")
	err = os.MkdirAll(appConfigDir, 0755) // Ensure the directory exists
	if err != nil {
		return "", fmt.Errorf("failed to create app config dir '%s': %w", appConfigDir, err)
	}
	return filepath.Join(appConfigDir, "config.json"), nil
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Use standard logger (already configured in main)
	log.Println("-----------------------------------------")
	log.Println("Application startup initiated.")
	log.Println("-----------------------------------------")

	err := bluetooth.Initialize()
	if err != nil {
		log.Printf("Error initializing Bluetooth: %v", err)
	}

	a.config.RenamedStations = make(map[string]string)

	// Load renamed stations from file in config directory
	configFilePath, errCfgPath := getConfigPathForConfig()
	if errCfgPath != nil {
		log.Printf("Error getting config file path: %v", errCfgPath)
	} else {
		log.Printf("Loading config from: %s", configFilePath)
		configFile, err := os.ReadFile(configFilePath)
		if err != nil {
			if !os.IsNotExist(err) { // Log error only if it's not 'file not found'
				log.Printf("Error reading config file '%s': %v", configFilePath, err)
			}
		} else {
			err = json.Unmarshal(configFile, &a.config)
			if err != nil {
				log.Printf("Error unmarshalling config: %v", err)
			}
		}
	}

	// Setup API routes
	a.api.Post("/allon", func(c *fiber.Ctx) error {
		// Use goroutine to avoid blocking API response while BT operation runs
		go a.PowerOnAllStations()
		return c.SendStatus(fiber.StatusOK)
	})
	a.api.Post("/alloff", func(c *fiber.Ctx) error {
		// Use goroutine to avoid blocking API response while BT operation runs
		go a.PowerOffAllStations()
		return c.SendStatus(fiber.StatusOK)
	})
	// Start API server in a goroutine
	go func() {
		if err := a.api.Listen("127.0.0.1:7575"); err != nil {
			log.Printf("Error starting API server: %v", err)
		}
	}()

	log.Println("Startup sequence complete.")
}

// --- Bluetooth Methods --- //

// ScanAndFetchStations performs a scan, updates the persistent station map,
// fetches initial states for newly discovered or disconnected stations, and returns the full list.
func (a *App) ScanAndFetchStations() ([]StationInfo, error) {
	a.stationsMutex.Lock() // Lock to modify isScanning
	if a.isScanning {
		a.stationsMutex.Unlock()
		log.Println("Scan already in progress. Ignoring request.")
		return a.GetCurrentStationInfo(), fmt.Errorf("scan already in progress")
	}
	a.isScanning = true
	a.stationsMutex.Unlock()

	// Ensure isScanning is set back to false when function exits
	defer func() {
		a.stationsMutex.Lock()
		a.isScanning = false
		log.Println("ScanAndFetchStations completed, isScanning set to false.")
		a.stationsMutex.Unlock()
	}()

	scanDuration := 5 * time.Second
	fetchWaitDuration := 7 * time.Second // Time to wait for FetchInitialPowerState goroutines

	log.Println("Adding 1s delay before starting scan...")
	time.Sleep(1 * time.Second)

	log.Printf("Starting blocking scan for %v...", scanDuration)
	discoveredValues, err := bluetooth.ScanForDuration(scanDuration)
	if err != nil {
		log.Printf("Error during blocking scan: %v", err)
		// Don't clear the map on scan error
		return a.GetCurrentStationInfo(), fmt.Errorf("bluetooth scan failed: %w", err)
	}
	log.Printf("Blocking scan finished, found %d devices in this scan.", len(discoveredValues))

	// --- Update persistent map and identify stations to fetch state for --- //
	stationsToFetch := make([]*bluetooth.BaseStation, 0, len(discoveredValues))
	a.stationsMutex.Lock() // Lock map for updates
	log.Println("Updating persistent station map...")
	for _, currentScanStation := range discoveredValues {
		addrStr := currentScanStation.Address.String()
		if existingStation, found := a.stations[addrStr]; found {
			// Station already exists, update name if needed
			if existingStation.Name != currentScanStation.Name {
				log.Printf("Updating name for %s from %s to %s", addrStr, existingStation.Name, currentScanStation.Name)
				existingStation.Name = currentScanStation.Name
			}
			// Check if we need to fetch state (if not currently connected)
			if !existingStation.IsConnected() {
				log.Printf("Existing station %s found, but not connected. Queuing for state fetch.", addrStr)
				stationsToFetch = append(stationsToFetch, existingStation)
			}
		} else {
			// New station found
			log.Printf("Adding new station %s (%s) to map.", currentScanStation.Name, addrStr)
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
		log.Printf("Launching state fetch routines for %d stations...", len(stationsToFetch))
		for _, stationToFetch := range stationsToFetch {
			wg.Add(1)
			go func(ptr *bluetooth.BaseStation) {
				defer wg.Done()
				err := bluetooth.FetchInitialPowerState(stationToFetch)
				if err != nil {
					log.Printf("App Goroutine: Error fetching state for %s: %v", stationToFetch.Name, err)
				}
			}(stationToFetch)
		}

		// Wait for fetch routines to complete (with timeout)
		waitChan := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitChan)
		}()

		log.Printf("Waiting up to %v for state fetch routines...", fetchWaitDuration)
		select {
		case <-waitChan:
			log.Println("All state fetch routines completed.")
		case <-time.After(fetchWaitDuration):
			log.Printf("Warning: Timed out waiting for state fetch routines after %v.", fetchWaitDuration)
		}
	}

	// --- Generate result for frontend from the full map --- //
	log.Println("Scan and update process complete. Returning full station info map.")
	return a.GetCurrentStationInfo(), nil
}

// IsScanning returns true if ScanAndFetchStations is currently running.
func (a *App) IsScanning() bool {
	a.stationsMutex.RLock()
	defer a.stationsMutex.RUnlock()
	return a.isScanning
}

// CheckAllStationStatuses attempts to fetch the current power state for disconnected stations
// and reads the state for already connected stations.
func (a *App) CheckAllStationStatuses() ([]StationInfo, error) {
	statusCheckTimeout := 4 * time.Second // Max time to wait for status checks

	log.Println("Starting periodic status check...")
	stationsToRead := make([]*bluetooth.BaseStation, 0)
	stationsToFetch := make([]*bluetooth.BaseStation, 0)

	a.stationsMutex.RLock() // Read lock to check connection status
	log.Printf("Checking status of %d known stations.", len(a.stations))
	for _, stationPtr := range a.stations {
		if stationPtr == nil {
			continue
		}
		if stationPtr.IsConnected() {
			stationsToRead = append(stationsToRead, stationPtr)
		} else {
			stationsToFetch = append(stationsToFetch, stationPtr)
		}
	}
	a.stationsMutex.RUnlock()

	if len(stationsToRead) == 0 && len(stationsToFetch) == 0 {
		log.Println("No stations known or needing check.")
		return a.GetCurrentStationInfo(), nil
	}

	// --- Launch fetch/read routines --- //
	var wg sync.WaitGroup

	// Launch routines to *read* already connected stations
	if len(stationsToRead) > 0 {
		log.Printf("Launching state read routines for %d connected stations...", len(stationsToRead))
		for _, stationToRead := range stationsToRead {
			wg.Add(1)
			go func(ptr *bluetooth.BaseStation) {
				defer wg.Done()
				err := bluetooth.ReadPowerState(stationToRead)
				if err != nil {
					// Log error, but don't necessarily fail the whole check
					log.Printf("App Goroutine: Error reading state for connected station %s: %v", stationToRead.Name, err)
				}
			}(stationToRead)
		}
	}

	// Launch routines to *fetch* (connect & read) disconnected stations
	if len(stationsToFetch) > 0 {
		log.Printf("Launching state fetch routines for %d disconnected stations...", len(stationsToFetch))
		for _, stationToFetch := range stationsToFetch {
			wg.Add(1)
			go func(ptr *bluetooth.BaseStation) {
				defer wg.Done()
				// We only *attempt* to fetch, don't worry about errors here
				err := bluetooth.FetchInitialPowerState(stationToFetch)
				if err != nil {
					log.Printf("App Goroutine: Error fetching state for disconnected station %s: %v", stationToFetch.Name, err)
				}
			}(stationToFetch)
		}
	}

	// Wait for ALL routines to complete (with timeout)
	waitChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitChan)
	}()

	log.Printf("Waiting up to %v for status check routines...", statusCheckTimeout)
	select {
	case <-waitChan:
		log.Println("All status check routines completed.")
	case <-time.After(statusCheckTimeout):
		log.Printf("Warning: Timed out waiting for status check routines after %v.", statusCheckTimeout)
	}

	// Return the updated list
	log.Println("Periodic status check complete. Returning current station info.")
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
				PowerState:   stationPtr.GetPowerState(),
			})
		} else {
			log.Println("Warning: Nil pointer found in stations map during GetCurrentStationInfo")
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
	log.Printf("Requesting Power ON for %s (%s)", stationPtr.Name, address)
	err := bluetooth.PowerOn(stationPtr)
	if err != nil {
		log.Printf("Error in PowerOn for %s: %v", stationPtr.Name, err)
		return err
	}
	return nil
}

// PowerOffStation turns a specific base station off.
func (a *App) PowerOffStation(address string) error {
	stationPtr := a.findStationPtrByAddress(address)
	if stationPtr == nil {
		return fmt.Errorf("station with address %s not found in App map", address)
	}
	log.Printf("Requesting Power OFF for %s (%s)", stationPtr.Name, address)
	err := bluetooth.PowerOff(stationPtr)
	if err != nil {
		log.Printf("Error in PowerOff for %s: %v", stationPtr.Name, err)
		return err
	}
	return nil
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

	log.Printf("Attempting to power ON %d stations...", len(stationsToToggle))
	var wg sync.WaitGroup
	errors := make(map[string]error)
	var errorMutex sync.Mutex

	for _, stationPtr := range stationsToToggle {
		wg.Add(1)
		go func(s *bluetooth.BaseStation) {
			defer wg.Done()
			err := bluetooth.PowerOn(s) // Call the existing PowerOn function
			if err != nil {
				log.Printf("App Goroutine: Error powering ON %s: %v", s.Name, err)
				errorMutex.Lock()
				errors[s.Address.String()] = err
				errorMutex.Unlock()
			}
		}(stationPtr)
	}

	wg.Wait()
	log.Println("Finished power ON attempts for all stations.")

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

	log.Printf("Attempting to power OFF %d stations...", len(stationsToToggle))
	var wg sync.WaitGroup
	errors := make(map[string]error)
	var errorMutex sync.Mutex

	for _, stationPtr := range stationsToToggle {
		wg.Add(1)
		go func(s *bluetooth.BaseStation) {
			defer wg.Done()
			err := bluetooth.PowerOff(s) // Call the existing PowerOff function
			if err != nil {
				log.Printf("App Goroutine: Error powering OFF %s: %v", s.Name, err)
				errorMutex.Lock()
				errors[s.Address.String()] = err
				errorMutex.Unlock()
			}
		}(stationPtr)
	}

	wg.Wait()
	log.Println("Finished power OFF attempts for all stations.")

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
		log.Printf("Resetting custom name for %s", originalName)
	} else {
		// Otherwise, save the new name
		a.config.RenamedStations[originalName] = newName
		log.Printf("Renaming %s to %s", originalName, newName)
	}
	return a.SaveConfig()
}

func (a *App) SaveConfig() error {
	configFilePath, err := getConfigPathForConfig()
	if err != nil {
		log.Printf("Failed to get config path for saving: %v", err)
		return fmt.Errorf("failed to get config path for saving: %w", err)
	}

	configFile, err := json.MarshalIndent(a.config, "", "  ") // Use MarshalIndent for readability
	if err != nil {
		log.Printf("Error marshalling config: %v", err)
		return fmt.Errorf("error marshalling config: %w", err)
	}
	log.Printf("Saving config to: %s", configFilePath)
	err = os.WriteFile(configFilePath, configFile, 0644)
	if err != nil {
		log.Printf("Failed to write config file '%s': %v", configFilePath, err)
		return err
	}
	return nil
}

// shutdown is called when the app terminates.
// It disconnects any active Bluetooth connections by iterating the map.
func (a *App) shutdown(ctx context.Context) {
	log.Println("App shutdown requested. Cleaning up...")
	if a.api != nil {
		log.Println("Shutting down API server...")
		err := a.api.Shutdown()
		if err != nil {
			log.Printf("Error shutting down API server: %v", err)
		}
	}
	log.Println("Requesting disconnect for all stations...")
	bluetooth.DisconnectAllStations() // Assumes this logs internally
	log.Println("App shutdown sequence complete.")
}

// Greet (Example method - can be kept or removed)
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
