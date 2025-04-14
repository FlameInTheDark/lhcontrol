package bluetooth

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

var (
	adapter = bluetooth.DefaultAdapter

	// UUIDs
	powerControlServiceUUIDString        = "00001523-1212-efde-1523-785feabcd124"
	powerControlCharacteristicUUIDString = "00001525-1212-efde-1523-785feabcd124"
	powerControlServiceUUID              bluetooth.UUID
	powerControlCharacteristicUUID       bluetooth.UUID

	// Track connected stations for cleanup
	connectedStations      []*BaseStation
	connectedStationsMutex sync.Mutex
)

// PowerState constants
const (
	PowerStateUnknown = -1
	PowerStateOff     = 0
	PowerStateOn      = 1
)

// BaseStation represents a discovered SteamVR Base Station.
type BaseStation struct {
	Name       string
	Address    bluetooth.Address
	PowerState int
	// Re-add fields for storing handles
	device         *bluetooth.Device
	characteristic *bluetooth.DeviceCharacteristic
	isConnected    bool
}

// IsConnected returns the current connection status of the base station.
// Re-add this getter method.
func (bs *BaseStation) IsConnected() bool {
	return bs.isConnected
}

// Initialize sets up the Bluetooth adapter and parses UUIDs.
func Initialize() error {
	// Re-initialize the tracking slice
	connectedStations = make([]*BaseStation, 0)

	err := adapter.Enable()
	if err != nil {
		return fmt.Errorf("could not enable Bluetooth adapter: %w", err)
	}

	var parseErr error
	powerControlServiceUUID, parseErr = bluetooth.ParseUUID(powerControlServiceUUIDString)
	if parseErr != nil {
		return fmt.Errorf("could not parse power control service UUID: %w", parseErr)
	}
	powerControlCharacteristicUUID, parseErr = bluetooth.ParseUUID(powerControlCharacteristicUUIDString)
	if parseErr != nil {
		return fmt.Errorf("could not parse power control characteristic UUID: %w", parseErr)
	}
	return nil
}

// ScanForDuration performs a blocking BLE scan for the specified duration
// and returns a list of discovered base stations.
// Uses time.AfterFunc to stop the scan.
func ScanForDuration(duration time.Duration) ([]BaseStation, error) {
	// log.Printf("[BT] ScanForDuration: Starting scan for %v...", duration)
	localStations := make(map[string]BaseStation)
	var localMutex sync.Mutex
	var scanErr error

	scanCallback := func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		if result.LocalName() == "" || !strings.HasPrefix(result.LocalName(), "LHB-") {
			return
		}
		addressString := result.Address.String()
		if addressString == "" || addressString == "00:00:00:00:00:00" {
			return
		}
		localMutex.Lock()
		if _, found := localStations[addressString]; !found {
			// log.Printf("[BT] Scan: Discovered %s (%s)", result.LocalName(), result.Address.String())
		}
		localStations[addressString] = BaseStation{
			Name:       result.LocalName(),
			Address:    result.Address,
			PowerState: PowerStateUnknown,
			// device, characteristic, and isConnected are initially nil/false
		}
		localMutex.Unlock()
	}

	// Schedule StopScan using time.AfterFunc
	stopTimer := time.AfterFunc(duration, func() {
		log.Printf("[BT] ScanForDuration (AfterFunc): Duration %v elapsed. Calling StopScan...", duration)
		err := adapter.StopScan()
		if err != nil {
			log.Printf("[BT] ScanForDuration (AfterFunc): adapter.StopScan() error: %v", err)
		}
	})

	// Start the blocking scan directly
	log.Println("[BT] ScanForDuration (AfterFunc): Calling adapter.Scan()...")
	scanErr = adapter.Scan(scanCallback) // This blocks until StopScan is called (by timer) or an error occurs
	stopTimer.Stop()                     // Prevent StopScan if Scan returned early (e.g., error)

	if scanErr != nil {
		log.Printf("[BT] ScanForDuration (AfterFunc): adapter.Scan() finished with error: %v", scanErr)
	} else {
		log.Println("[BT] ScanForDuration (AfterFunc): adapter.Scan() finished gracefully (likely due to StopScan timer).)")
	}

	// Collect results
	localMutex.Lock()
	results := make([]BaseStation, 0, len(localStations))
	for _, station := range localStations {
		results = append(results, station)
	}
	localMutex.Unlock()

	log.Printf("[BT] ScanForDuration (AfterFunc): Finished. Found %d stations.", len(results))

	if len(results) == 0 && scanErr != nil {
		return nil, fmt.Errorf("scan failed with no results: %w", scanErr)
	}
	return results, nil
}

// FetchInitialPowerState attempts to retrieve the current power state of a station ONCE.
// It calls tryFetchPowerStateAttempt and updates the station pointer.
func FetchInitialPowerState(station *BaseStation) {
	log.Printf("[BT] FetchState: Starting single attempt for %s (%s)...", station.Name, station.Address.String())

	// Update to handle error return
	success, newState, err := tryFetchPowerStateAttempt(station)
	if success {
		log.Printf("[BT] FetchState: Success for %s. State: %d", station.Name, newState)
		station.PowerState = newState
	} else {
		if err != nil {
			log.Printf("[BT] FetchState: Failed single attempt for %s: %v. State remains Unknown.", station.Name, err)
		} else {
			log.Printf("[BT] FetchState: Failed single attempt for %s (no specific error). State remains Unknown.", station.Name)
		}
		station.PowerState = PowerStateUnknown
	}
}

// tryFetchPowerStateAttempt contains the logic for a single attempt to fetch the power state.
// On success, it stores the device and characteristic handles in the station struct,
// adds station to tracking list, and LEAVES THE CONNECTION OPEN.
// Returns success (bool), read state (int), and error.
func tryFetchPowerStateAttempt(station *BaseStation) (bool, int, error) {
	// --- Disconnect first if already connected (safety check) ---
	if station.isConnected && station.device != nil {
		log.Printf("[BT] tryFetchAttempt: WARN - Station %s already connected? Disconnecting first.", station.Name)
		DisconnectStation(station)
		time.Sleep(500 * time.Millisecond)
	}

	address := station.Address
	addressString := station.Address.String()
	if addressString == "" || addressString == "00:00:00:00:00:00" {
		return false, PowerStateUnknown, fmt.Errorf("invalid address '%s' for %s", addressString, station.Name)
	}

	// --- Delay Before Connecting ---
	log.Printf("[BT] tryFetchAttempt: Waiting 500ms before connecting to %s...", station.Name)
	time.Sleep(500 * time.Millisecond)

	// --- Connection ---
	log.Printf("[BT] tryFetchAttempt: Connecting to %s...", station.Name)
	device, connectErr := adapter.Connect(address, bluetooth.ConnectionParams{})
	if connectErr != nil {
		return false, PowerStateUnknown, fmt.Errorf("failed to connect: %w", connectErr)
	}
	log.Printf("[BT] tryFetchAttempt: Connected to %s.", station.Name)

	// *** REMOVED defer device.Disconnect() AGAIN - Keep connection open on success ***

	// --- Service Discovery (Targeted) ---
	log.Printf("[BT] tryFetchAttempt: Discovering service %s for %s...", powerControlServiceUUIDString, station.Name)
	services, err := device.DiscoverServices([]bluetooth.UUID{powerControlServiceUUID})
	if err != nil {
		log.Printf("[BT] tryFetchAttempt: Failed discover target service for %s: %v. Disconnecting.", station.Name, err)
		device.Disconnect() // Disconnect on error
		return false, PowerStateUnknown, fmt.Errorf("failed discover target service: %w", err)
	}
	if len(services) == 0 {
		log.Printf("[BT] tryFetchAttempt: Target service %s not found on %s. Disconnecting.", powerControlServiceUUIDString, station.Name)
		device.Disconnect() // Disconnect on error
		return false, PowerStateUnknown, fmt.Errorf("target service %s not found", powerControlServiceUUIDString)
	}
	powerService := services[0]
	log.Printf("[BT] tryFetchAttempt: Found power service for %s.", station.Name)

	// --- Characteristic Discovery (Targeted) ---
	log.Printf("[BT] tryFetchAttempt: Discovering characteristic %s for %s...", powerControlCharacteristicUUIDString, station.Name)
	chars, err := powerService.DiscoverCharacteristics([]bluetooth.UUID{powerControlCharacteristicUUID})
	if err != nil {
		log.Printf("[BT] tryFetchAttempt: Failed discover target characteristic for %s: %v. Disconnecting.", station.Name, err)
		device.Disconnect() // Disconnect on error
		return false, PowerStateUnknown, fmt.Errorf("failed discover target characteristic: %w", err)
	}
	if len(chars) == 0 {
		log.Printf("[BT] tryFetchAttempt: Target characteristic %s not found in service for %s. Disconnecting.", powerControlCharacteristicUUIDString, station.Name)
		device.Disconnect() // Disconnect on error
		return false, PowerStateUnknown, fmt.Errorf("target characteristic %s not found in service", powerControlCharacteristicUUIDString)
	}
	powerChar := chars[0]
	log.Printf("[BT] tryFetchAttempt: Found power characteristic for %s.", station.Name)

	// --- Read Value ---
	log.Printf("[BT] tryFetchAttempt: Reading characteristic for %s...", station.Name)
	readValue := make([]byte, 1)
	nRead, errRead := powerChar.Read(readValue)
	if errRead != nil {
		log.Printf("[BT] tryFetchAttempt: Failed read characteristic for %s: %v. Disconnecting.", station.Name, errRead)
		device.Disconnect() // Disconnect on error
		return false, PowerStateUnknown, fmt.Errorf("failed read characteristic: %w", errRead)
	}
	if nRead > 0 {
		currentState := int(readValue[0])
		newState := PowerStateUnknown
		isOk := false
		if currentState == PowerStateOn || currentState == 0x0B {
			newState = PowerStateOn
			isOk = true
		} else if currentState == PowerStateOff {
			newState = PowerStateOff
			isOk = true
		}

		if isOk {
			log.Printf("[BT] tryFetchAttempt: Successfully read state 0x%X (mapped to ON) for %s.", readValue[0], station.Name)
			// STORE HANDLES & TRACK
			station.device = &device
			station.characteristic = &powerChar
			station.isConnected = true
			connectedStationsMutex.Lock()
			// Avoid double-adding
			found := false
			for _, s := range connectedStations {
				if s.Address.String() == station.Address.String() {
					found = true
					break
				}
			}
			if !found {
				connectedStations = append(connectedStations, station)
			}
			connectedStationsMutex.Unlock()
			return true, newState, nil // Return standard ON state
		} else {
			errUnexpected := fmt.Errorf("read unexpected state 0x%X", currentState)
			log.Printf("[BT] tryFetchAttempt: %v for %s. Treating as Unknown. Disconnecting.", errUnexpected, station.Name)
			device.Disconnect() // Disconnect on error
			return false, PowerStateUnknown, errUnexpected
		}
	} else {
		errReadZero := fmt.Errorf("read 0 bytes from characteristic")
		log.Printf("[BT] tryFetchAttempt: %v for %s. Disconnecting.", errReadZero, station.Name)
		device.Disconnect() // Disconnect on error
		return false, PowerStateUnknown, errReadZero
	}
}

// SetPowerState uses the stored characteristic handle (if available) to write the target command.
func SetPowerState(station *BaseStation, targetCommand byte) error {
	targetStateStr := "UNKNOWN"
	targetStateInt := PowerStateUnknown
	if targetCommand == 0x00 {
		targetStateStr = "OFF (0x00)"
		targetStateInt = PowerStateOff
	} else if targetCommand == 0x01 {
		targetStateStr = "ON (0x01)"
		targetStateInt = PowerStateOn
	} else {
		return fmt.Errorf("[BT] SetPowerState: Invalid target command byte 0x%X for %s", targetCommand, station.Name)
	}
	log.Printf("[BT] SetPowerState: Attempting %s for %s (%s) using stored handles...", targetStateStr, station.Name, station.Address.String())

	// --- Check if handles are valid --- //
	if !station.isConnected || station.characteristic == nil || station.device == nil {
		log.Printf("[BT] SetPowerState: Station %s is not connected or handles are missing. Cannot set state.", station.Name)
		return fmt.Errorf("station %s not initialized or connection lost", station.Name)
	}

	// --- Use stored characteristic directly --- //
	powerChar := station.characteristic // Use the stored pointer
	log.Printf("[BT] SetPowerState: Writing target state (%s) to stored characteristic for %s...", targetStateStr, station.Name)
	n, err := powerChar.Write([]byte{targetCommand})

	// --- Handle Write Result --- //
	if err != nil {
		// Assume connection is lost on write error
		log.Printf("[BT] SetPowerState: FAILED Write command (%s) to %s: %v. Assuming connection lost.", targetStateStr, station.Name, err)

		// Perform disconnect and cleanup
		log.Printf("[BT] SetPowerState: Cleaning up connection state for %s due to write error...", station.Name)
		// Attempt disconnect (ignore error, best effort)
		if station.device != nil {
			_ = station.device.Disconnect()
		}
		// Clear handles and state
		station.isConnected = false
		station.device = nil
		station.characteristic = nil
		station.PowerState = PowerStateUnknown // Set state to Unknown on error

		// Remove from tracking list
		connectedStationsMutex.Lock()
		newConnectedList := make([]*BaseStation, 0, len(connectedStations))
		for _, s := range connectedStations {
			if s.Address.String() != station.Address.String() {
				newConnectedList = append(newConnectedList, s)
			}
		}
		connectedStations = newConnectedList
		log.Printf("[BT] SetPowerState: Removed %s from connected list after write error.", station.Name)
		connectedStationsMutex.Unlock()

		return fmt.Errorf("failed Write command (%s) to %s (connection likely lost): %w", targetStateStr, station.Name, err)
	}

	if n != 1 {
		log.Printf("[BT] SetPowerState: Incorrect byte count on write (%d != 1) for %s.", n, station.Name)
		// Consider if this also implies connection loss or just a bad write?
		// For now, just return error, don't necessarily disconnect.
		return fmt.Errorf("failed Write command (%s) to %s: wrote %d bytes, expected 1", targetStateStr, station.Name, n)
	}

	// --- Success --- //
	log.Printf("[BT] SetPowerState: Write successful (%s) for %s.", targetStateStr, station.Name)
	station.PowerState = targetStateInt // Update state in memory only on success
	return nil
}

// PowerOn ensures the base station is powered on.
func PowerOn(station *BaseStation) error {
	return SetPowerState(station, byte(PowerStateOn))
}

// PowerOff ensures the base station is powered off.
func PowerOff(station *BaseStation) error {
	return SetPowerState(station, byte(PowerStateOff))
}

// DisconnectStation explicitly disconnects a single station and removes it from tracking.
func DisconnectStation(station *BaseStation) {
	if !station.isConnected || station.device == nil {
		// log.Printf("[BT] DisconnectStation: Station %s is not connected or device is nil.", station.Name)
		return // Already disconnected or handles are invalid
	}
	log.Printf("[BT] DisconnectStation: Disconnecting from %s...", station.Name)
	err := station.device.Disconnect()
	if err != nil {
		log.Printf("[BT] DisconnectStation: Error disconnecting from %s: %v", station.Name, err)
	}

	// Clear handles and state regardless of disconnect error
	station.isConnected = false
	station.device = nil
	station.characteristic = nil
	// Don't reset PowerState here, keep last known state

	// Remove from tracked list
	connectedStationsMutex.Lock()
	newConnectedList := make([]*BaseStation, 0, len(connectedStations))
	found := false // To log if it was actually removed
	for _, s := range connectedStations {
		if s.Address.String() != station.Address.String() {
			newConnectedList = append(newConnectedList, s)
		} else {
			found = true
		}
	}
	connectedStations = newConnectedList
	if found {
		log.Printf("[BT] DisconnectStation: Removed %s from connected list (new size: %d).", station.Name, len(connectedStations))
	}
	connectedStationsMutex.Unlock()
}

// DisconnectAllStations iterates through the tracked list and disconnects each station.
// Intended for application shutdown.
func DisconnectAllStations() {
	connectedStationsMutex.Lock()
	// Create a copy of the list to iterate over, as DisconnectStation modifies the original
	stationsToDisconnect := make([]*BaseStation, len(connectedStations))
	copy(stationsToDisconnect, connectedStations)
	log.Printf("[BT] DisconnectAllStations: Disconnecting %d tracked stations...", len(stationsToDisconnect))
	connectedStationsMutex.Unlock() // Unlock before potentially long disconnect loop

	for _, station := range stationsToDisconnect {
		DisconnectStation(station) // This will lock/unlock the mutex internally
	}
	log.Println("[BT] DisconnectAllStations: Finished.")
}
