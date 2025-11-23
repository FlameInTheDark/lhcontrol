package station

import (
	"fmt"
	"log"
	"sync"
	"time"

	"lhcontrol/internal/bluetooth"
	"lhcontrol/internal/config"
)

// StationInfo is a simplified representation of a BaseStation for the frontend.
type StationInfo struct {
	Name         string `json:"name"`
	OriginalName string `json:"originalName"`
	Address      string `json:"address"`
	PowerState   int    `json:"powerState"`
}

type Manager struct {
	stations      map[string]*bluetooth.BaseStation
	stationsMutex sync.RWMutex
	config        *config.Config
	isScanning    bool
}

func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		stations: make(map[string]*bluetooth.BaseStation),
		config:   cfg,
	}
}

// Initialize should be called at app startup
func (m *Manager) Initialize() error {
	return bluetooth.Initialize()
}

// GetStationInfo returns the current state of the stations map.
func (m *Manager) GetStationInfo() []StationInfo {
	m.stationsMutex.RLock()
	defer m.stationsMutex.RUnlock()

	stationInfos := make([]StationInfo, 0, len(m.stations))
	for _, stationPtr := range m.stations {
		if stationPtr != nil {
			var name string
			if renamedName, ok := m.config.RenamedStations[stationPtr.Name]; ok {
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
		}
	}
	return stationInfos
}

func (m *Manager) ScanAndFetchStations() ([]StationInfo, error) {
	m.stationsMutex.Lock()
	if m.isScanning {
		m.stationsMutex.Unlock()
		return m.GetStationInfo(), fmt.Errorf("scan already in progress")
	}
	m.isScanning = true
	m.stationsMutex.Unlock()

	defer func() {
		m.stationsMutex.Lock()
		m.isScanning = false
		m.stationsMutex.Unlock()
	}()

	scanDuration := 5 * time.Second
	fetchWaitDuration := 7 * time.Second

	// Using time.Sleep inside a method is generally not ideal for testing,
	// but preserving original logic for now.
	time.Sleep(1 * time.Second)

	discoveredValues, err := bluetooth.ScanForDuration(scanDuration)
	if err != nil {
		return m.GetStationInfo(), fmt.Errorf("bluetooth scan failed: %w", err)
	}

	stationsToFetch := make([]*bluetooth.BaseStation, 0)
	m.stationsMutex.Lock()
	for _, currentScanStation := range discoveredValues {
		addrStr := currentScanStation.Address.String()
		if existingStation, found := m.stations[addrStr]; found {
			if existingStation.Name != currentScanStation.Name {
				existingStation.Name = currentScanStation.Name
			}
			if !existingStation.IsConnected() {
				stationsToFetch = append(stationsToFetch, existingStation)
			}
		} else {
			newStationPtr := new(bluetooth.BaseStation)
			*newStationPtr = currentScanStation
			m.stations[addrStr] = newStationPtr
			stationsToFetch = append(stationsToFetch, newStationPtr)
		}
	}
	m.stationsMutex.Unlock()

	if len(stationsToFetch) > 0 {
		var wg sync.WaitGroup
		for _, stationToFetch := range stationsToFetch {
			wg.Add(1)
			go func(ptr *bluetooth.BaseStation) {
				defer wg.Done()
				_ = bluetooth.FetchInitialPowerState(ptr)
			}(stationToFetch)
		}

		waitChan := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitChan)
		}()

		select {
		case <-waitChan:
		case <-time.After(fetchWaitDuration):
			log.Println("Warning: Timed out waiting for state fetch routines.")
		}
	}

	return m.GetStationInfo(), nil
}

func (m *Manager) IsScanning() bool {
	m.stationsMutex.RLock()
	defer m.stationsMutex.RUnlock()
	return m.isScanning
}

func (m *Manager) CheckAllStationStatuses() ([]StationInfo, error) {
	statusCheckTimeout := 4 * time.Second

	stationsToRead := make([]*bluetooth.BaseStation, 0)
	stationsToFetch := make([]*bluetooth.BaseStation, 0)

	m.stationsMutex.RLock()
	for _, stationPtr := range m.stations {
		if stationPtr == nil {
			continue
		}
		if stationPtr.IsConnected() {
			stationsToRead = append(stationsToRead, stationPtr)
		} else {
			stationsToFetch = append(stationsToFetch, stationPtr)
		}
	}
	m.stationsMutex.RUnlock()

	if len(stationsToRead) == 0 && len(stationsToFetch) == 0 {
		return m.GetStationInfo(), nil
	}

	var wg sync.WaitGroup

	for _, stationToRead := range stationsToRead {
		wg.Add(1)
		go func(ptr *bluetooth.BaseStation) {
			defer wg.Done()
			_ = bluetooth.ReadPowerState(ptr)
		}(stationToRead)
	}

	for _, stationToFetch := range stationsToFetch {
		wg.Add(1)
		go func(ptr *bluetooth.BaseStation) {
			defer wg.Done()
			_ = bluetooth.FetchInitialPowerState(ptr)
		}(stationToFetch)
	}

	waitChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
	case <-time.After(statusCheckTimeout):
		log.Println("Warning: Timed out waiting for status check routines.")
	}

	return m.GetStationInfo(), nil
}

func (m *Manager) PowerOnStation(address string) error {
	m.stationsMutex.RLock()
	stationPtr, ok := m.stations[address]
	m.stationsMutex.RUnlock()

	if !ok || stationPtr == nil {
		return fmt.Errorf("station with address %s not found", address)
	}
	return bluetooth.PowerOn(stationPtr)
}

func (m *Manager) PowerOffStation(address string) error {
	m.stationsMutex.RLock()
	stationPtr, ok := m.stations[address]
	m.stationsMutex.RUnlock()

	if !ok || stationPtr == nil {
		return fmt.Errorf("station with address %s not found", address)
	}
	return bluetooth.PowerOff(stationPtr)
}

func (m *Manager) PowerOnAllStations() error {
	m.stationsMutex.RLock()
	stationsToToggle := make([]*bluetooth.BaseStation, 0, len(m.stations))
	for _, stationPtr := range m.stations {
		if stationPtr != nil {
			stationsToToggle = append(stationsToToggle, stationPtr)
		}
	}
	m.stationsMutex.RUnlock()

	var wg sync.WaitGroup
	errors := make(map[string]error)
	var errorMutex sync.Mutex

	for _, stationPtr := range stationsToToggle {
		wg.Add(1)
		go func(s *bluetooth.BaseStation) {
			defer wg.Done()
			err := bluetooth.PowerOn(s)
			if err != nil {
				errorMutex.Lock()
				errors[s.Address.String()] = err
				errorMutex.Unlock()
			}
		}(stationPtr)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d error(s) during PowerOnAllStations", len(errors))
	}
	return nil
}

func (m *Manager) PowerOffAllStations() error {
	m.stationsMutex.RLock()
	stationsToToggle := make([]*bluetooth.BaseStation, 0, len(m.stations))
	for _, stationPtr := range m.stations {
		if stationPtr != nil {
			stationsToToggle = append(stationsToToggle, stationPtr)
		}
	}
	m.stationsMutex.RUnlock()

	var wg sync.WaitGroup
	errors := make(map[string]error)
	var errorMutex sync.Mutex

	for _, stationPtr := range stationsToToggle {
		wg.Add(1)
		go func(s *bluetooth.BaseStation) {
			defer wg.Done()
			err := bluetooth.PowerOff(s)
			if err != nil {
				errorMutex.Lock()
				errors[s.Address.String()] = err
				errorMutex.Unlock()
			}
		}(stationPtr)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d error(s) during PowerOffAllStations", len(errors))
	}
	return nil
}

func (m *Manager) RenameStation(originalName string, newName string) error {
	if newName == "" {
		delete(m.config.RenamedStations, originalName)
	} else {
		m.config.RenamedStations[originalName] = newName
	}
	return m.config.Save()
}

func (m *Manager) Shutdown() {
	bluetooth.DisconnectAllStations()
}
