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
	// Fields for storing handles and state
	device         *bluetooth.Device // Correct type
	characteristic *bluetooth.DeviceCharacteristic
	isConnected    bool
	// Add Mutex for thread-safe access
	mutex           sync.RWMutex
	LastStateUpdate time.Time // Track when state was last read
}

// IsConnected returns the current connection status safely.
func (bs *BaseStation) IsConnected() bool {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()
	return bs.isConnected && bs.device != nil
}

// setPowerStateInternal updates the power state and timestamp safely.
// Assumes caller holds the write lock (bs.mutex.Lock()).
func (bs *BaseStation) setPowerStateInternal(state int) {
	bs.PowerState = state
	bs.LastStateUpdate = time.Now()
}

// GetPowerState reads the power state safely.
func (bs *BaseStation) GetPowerState() int {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()
	return bs.PowerState
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

// readPowerStateInternal performs the actual read and update.
// Assumes caller holds the write lock (station.mutex.Lock()).
func readPowerStateInternal(station *BaseStation) error {
	if station.characteristic == nil {
		return fmt.Errorf("power characteristic is nil for %s", station.Name)
	}

	log.Printf("Bluetooth: Reading power state for %s (%s)", station.Name, station.Address)
	buf := make([]byte, 1)
	n, err := station.characteristic.Read(buf)
	if err != nil {
		station.setPowerStateInternal(PowerStateUnknown) // Use helper
		return fmt.Errorf("failed to read power characteristic for %s: %w", station.Name, err)
	}
	if n != 1 {
		station.setPowerStateInternal(PowerStateUnknown) // Use helper
		return fmt.Errorf("unexpected bytes read (%d) for power on %s", n, station.Name)
	}

	newState := int(buf[0])
	// Treat 0 as Off, anything else as On
	if newState != PowerStateOff {
		log.Printf("Bluetooth: Read non-zero state 0x%X for %s. Treating as ON.", buf[0], station.Name)
		newState = PowerStateOn
	}
	// No need to explicitly check for 1 anymore, and remove warning for other values

	if station.PowerState != newState { // Check before logging
		log.Printf("Bluetooth: Power state for %s changed from %d to %d", station.Name, station.PowerState, newState)
	}
	station.setPowerStateInternal(newState) // Use helper

	return nil
}

// ReadPowerState attempts to read the current power state for an already connected station.
func ReadPowerState(station *BaseStation) error {
	if station == nil {
		return fmt.Errorf("station is nil")
	}

	station.mutex.Lock() // Lock for the duration
	defer station.mutex.Unlock()

	if !station.isConnected || station.device == nil {
		return fmt.Errorf("station %s is not connected", station.Name)
	}
	if station.characteristic == nil {
		log.Printf("Bluetooth: Error - Power characteristic not found for connected station %s.", station.Name)
		return fmt.Errorf("power characteristic not cached for %s", station.Name)
	}

	return readPowerStateInternal(station)
}

// connectAndDiscoverInternal handles connection and discovery.
// Assumes caller holds the write lock (station.mutex.Lock()).
func connectAndDiscoverInternal(station *BaseStation) error {
	if station.isConnected && station.device != nil && station.characteristic != nil {
		return nil // Already good
	}

	if !station.isConnected || station.device == nil {
		log.Printf("Bluetooth: Internal connect attempt for %s...", station.Name)
		device, err := adapter.Connect(station.Address, bluetooth.ConnectionParams{})
		if err != nil {
			station.isConnected = false
			station.device = nil
			station.characteristic = nil
			station.setPowerStateInternal(PowerStateUnknown)
			return fmt.Errorf("connection failed internal: %w", err)
		}
		station.device = &device // Assign pointer correctly
		station.isConnected = true
		log.Printf("Bluetooth: Internal connect successful for %s.", station.Name)
		connectedStationsMutex.Lock()
		found := false
		for _, cs := range connectedStations {
			if cs.Address == station.Address {
				found = true
				break
			}
		}
		if !found {
			connectedStations = append(connectedStations, station)
		}
		connectedStationsMutex.Unlock()
	}

	if station.characteristic == nil {
		log.Printf("Bluetooth: Internal discovery attempt for %s...", station.Name)

		var services []bluetooth.DeviceService
		var chars []bluetooth.DeviceCharacteristic
		var err error

		const maxRetries = 3
		for i := 0; i < maxRetries; i++ {
			if i > 0 {
				log.Printf("Bluetooth: Retrying discovery for %s (attempt %d/%d)...", station.Name, i+1, maxRetries)
				time.Sleep(500 * time.Millisecond)
			}

			services, err = station.device.DiscoverServices([]bluetooth.UUID{powerControlServiceUUID})
			if err != nil {
				// Retry if discovery returns error
				continue
			}
			if len(services) == 0 {
				err = fmt.Errorf("no services found")
				continue
			}

			chars, err = services[0].DiscoverCharacteristics([]bluetooth.UUID{powerControlCharacteristicUUID})
			if err != nil {
				// Retry if char discovery returns error
				continue
			}
			if len(chars) == 0 {
				err = fmt.Errorf("no characteristics found")
				continue
			}

			// If we reach here, we found what we needed
			err = nil
			break
		}

		if err != nil {
			disconnectInternal(station)
			return fmt.Errorf("discovery failed internal for %s after %d retries: %w", station.Name, maxRetries, err)
		}

		station.characteristic = &chars[0]
		log.Printf("Bluetooth: Internal discovery successful for %s.", station.Name)
	}
	return nil
}

// FetchInitialPowerState attempts to connect (if necessary) and read the initial power state.
func FetchInitialPowerState(station *BaseStation) error {
	if station == nil {
		return fmt.Errorf("station is nil")
	}

	station.mutex.Lock() // Lock for the whole operation
	defer station.mutex.Unlock()

	err := connectAndDiscoverInternal(station)
	if err != nil {
		log.Printf("Bluetooth: Failed to connect/discover in FetchInitialPowerState for %s: %v", station.Name, err)
		return err
	}

	log.Printf("Bluetooth: FetchInitialPowerState proceeding to read state for %s.", station.Name)
	err = readPowerStateInternal(station)
	if err != nil {
		log.Printf("Bluetooth: Failed to read state in FetchInitialPowerState for %s: %v", station.Name, err)
		return err
	}

	log.Printf("Bluetooth: FetchInitialPowerState successful for %s. State: %d", station.Name, station.PowerState)
	return nil
}

// PowerOn attempts to turn the base station on.
func PowerOn(station *BaseStation) error {
	if station == nil {
		return fmt.Errorf("station is nil")
	}
	station.mutex.Lock()
	defer station.mutex.Unlock()

	const maxRetries = 2
	var err error

	for i := 0; i < maxRetries; i++ {
		if err = connectAndDiscoverInternal(station); err != nil {
			// If connection fails, we can't proceed with this attempt.
			// If it was a retry after a write failure, this will be the final error.
			log.Printf("Bluetooth: connect/discover failed during PowerOn attempt %d/%d for %s: %v", i+1, maxRetries, station.Name, err)
			if i == maxRetries-1 {
				return fmt.Errorf("failed to connect/discover before PowerOn: %w", err)
			}
			// If we failed to connect, wait a bit and try again (force disconnect just in case state is weird)
			disconnectInternal(station)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		log.Printf("Bluetooth: Sending Power ON command to %s using WriteWithoutResponse", station.Name)
		var n int
		n, err = station.characteristic.WriteWithoutResponse([]byte{0x01})
		if err == nil {
			if n != 1 {
				// A successful write should return n=1 for one byte
				log.Printf("Bluetooth: Warning - wrote %d bytes instead of 1 for Power ON on %s", n, station.Name)
			}
			// Success
			break
		}

		log.Printf("Bluetooth: Write Power ON failed for %s: %v. Retrying...", station.Name, err)
		disconnectInternal(station)
		// The next iteration will try to reconnect
		if i < maxRetries-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to write Power ON command after %d retries: %w", maxRetries, err)
	}

	time.Sleep(100 * time.Millisecond)
	err = readPowerStateInternal(station)
	if err != nil {
		log.Printf("Bluetooth: Failed to read back state after PowerOn for %s: %v (state may be stale)", station.Name, err)
	}
	return nil
}

// PowerOff attempts to turn the base station off.
func PowerOff(station *BaseStation) error {
	if station == nil {
		return fmt.Errorf("station is nil")
	}
	station.mutex.Lock()
	defer station.mutex.Unlock()

	const maxRetries = 2
	var err error

	for i := 0; i < maxRetries; i++ {
		if err = connectAndDiscoverInternal(station); err != nil {
			log.Printf("Bluetooth: connect/discover failed during PowerOff attempt %d/%d for %s: %v", i+1, maxRetries, station.Name, err)
			if i == maxRetries-1 {
				return fmt.Errorf("failed to connect/discover before PowerOff: %w", err)
			}
			disconnectInternal(station)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		log.Printf("Bluetooth: Sending Power OFF command to %s using WriteWithoutResponse", station.Name)
		var n int
		n, err = station.characteristic.WriteWithoutResponse([]byte{0x00})
		if err == nil {
			if n != 1 {
				log.Printf("Bluetooth: Warning - wrote %d bytes instead of 1 for Power OFF on %s", n, station.Name)
			}
			// Success
			break
		}

		log.Printf("Bluetooth: Write Power OFF failed for %s: %v. Retrying...", station.Name, err)
		disconnectInternal(station)
		if i < maxRetries-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to write Power OFF command after %d retries: %w", maxRetries, err)
	}

	time.Sleep(100 * time.Millisecond)
	err = readPowerStateInternal(station)
	if err != nil {
		log.Printf("Bluetooth: Failed to read back state after PowerOff for %s: %v (state may be stale)", station.Name, err)
	}
	return nil
}

// disconnectInternal performs disconnection without locking (must be called within locked context).
// Also removes station from the global tracking list.
func disconnectInternal(s *BaseStation) {
	if s.device != nil {
		log.Printf("Bluetooth: Disconnecting internal for %s", s.Name)
		_ = s.device.Disconnect()
	}
	s.isConnected = false
	s.device = nil
	s.characteristic = nil
	s.setPowerStateInternal(PowerStateUnknown)

	connectedStationsMutex.Lock()
	newConnectedStations := make([]*BaseStation, 0, len(connectedStations))
	for _, cs := range connectedStations {
		if cs.Address != s.Address {
			newConnectedStations = append(newConnectedStations, cs)
		}
	}
	connectedStations = newConnectedStations
	connectedStationsMutex.Unlock()
}

// DisconnectStation disconnects from a specific base station.
func DisconnectStation(station *BaseStation) {
	if station == nil {
		return
	}
	station.mutex.Lock() // Lock before calling internal disconnect
	defer station.mutex.Unlock()
	disconnectInternal(station) // Use internal helper
}

// DisconnectAllStations disconnects all tracked stations.
func DisconnectAllStations() {
	connectedStationsMutex.Lock()
	log.Printf("Bluetooth: Disconnecting all %d tracked stations...", len(connectedStations))
	stationsToDisconnect := make([]*BaseStation, len(connectedStations))
	copy(stationsToDisconnect, connectedStations)
	connectedStationsMutex.Unlock()

	for _, station := range stationsToDisconnect {
		DisconnectStation(station)
	}
	log.Println("Bluetooth: Disconnect all stations attempt finished.")
}
