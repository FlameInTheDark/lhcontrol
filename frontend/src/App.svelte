<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    ScanAndFetchStations,  // Use this new method
    GetCurrentStationInfo, // Restore import
    PowerOnStation,  // Restore import
    PowerOffStation, // Restore import
    PowerOnAllStations, // Import new function
    PowerOffAllStations // Import new function
  } from '../wailsjs/go/main/App';

  // Interface matching the Go StationInfo struct
  interface StationInfo {
    name: string;
    address: string;
    powerState: number; // -1: Unknown, 0: Off, 1: On
  }

  let stations: StationInfo[] = [];
  let statusMessage: string = "Click 'Scan' to find base stations.";
  let operationInProgress: { [address: string]: boolean } = {}; // Restore state
  let isLoading: boolean = false; // Track if Scan or Toggle is in progress
  let isBulkLoading: boolean = false; // Track if bulk operation is in progress

  // Handles the Scan button click
  async function handleScanClick() {
    if (isLoading || isBulkLoading) return;
    isLoading = true;
    // stations = []; // REMOVED: Don't clear the list visually
    statusMessage = "Scanning and updating list..."; // Update message
    // operationInProgress = {}; // Keep this? Or allow toggles during scan update?
                                  // Let's keep it cleared for simplicity for now.
    operationInProgress = {};

    try {
      // Call the backend method (which now returns the full persistent list)
      const result = await ScanAndFetchStations();
      stations = result || []; // Assign the full updated list from the backend
      if (stations.length > 0) {
        statusMessage = `Scan complete. ${stations.length} station(s) known.`;
      } else {
        statusMessage = "Scan complete. No stations known.";
      }
    } catch (error) {
      statusMessage = `Error during scan/update: ${error}`;
      console.error("Error scan/update:", error);
      // Don't clear stations on error, keep the last known good list?
      // Or potentially fetch the list again?
      // For now, keep the possibly stale list and show error.
    } finally {
      isLoading = false;
    }
  }

  // Restore fetchLatestList function (used by toggle)
  async function fetchLatestList() {
       try {
           const currentList = await GetCurrentStationInfo();
           stations = currentList || [];
           console.log("Refreshed list after operation.");
       } catch (error) {
           console.error("Error fetching list after operation:", error);
           statusMessage = `Error refreshing list: ${error}`;
       }
  }

  // Restore togglePower function
  async function togglePower(station: StationInfo) {
    // Restore check for powerState === -1
    if (station.powerState === -1 || operationInProgress[station.address] || isLoading || isBulkLoading) {
      console.log(`Operation already in progress, state unknown, or scan active for ${station.address}`);
      return;
    }

    // Restore original logic for determining target state
    const targetState = station.powerState === 0 ? 'ON' : 'OFF';

    isLoading = true; // Prevent scanning during toggle
    statusMessage = `Attempting to turn ${station.name} ${targetState}...`; // Restore original message
    operationInProgress = { ...operationInProgress, [station.address]: true };
    stations = [...stations];

    try {
      // Restore original logic for calling PowerOn/PowerOff
      if (station.powerState === 0) {
        await PowerOnStation(station.address);
      } else {
        await PowerOffStation(station.address);
      }
      statusMessage = `Successfully turned ${station.name} ${targetState}. Refreshing list...`; // Restore original message
      setTimeout(fetchLatestList, 1500);
    } catch (error) {
      statusMessage = `Error toggling power for ${station.name}: ${error}`; // Restore original message
      console.error(`Error toggling power for ${station.name}:`, error);
    } finally {
       operationInProgress = { ...operationInProgress, [station.address]: false };
       stations = [...stations];
       isLoading = false; // Reset loading after toggle attempt completes
    }
  }

  // Handles Power On All button click
  async function handlePowerOnAll() {
    if (isLoading || isBulkLoading) return;
    isBulkLoading = true;
    statusMessage = "Attempting to power ON all stations...";
    try {
      await PowerOnAllStations();
      statusMessage = "Sent Power ON command to all stations. Refreshing list...";
    } catch (error) {
      statusMessage = `Error powering on all stations: ${error}`;
      console.error("Error PowerOnAll:", error);
    } finally {
      isBulkLoading = false;
      setTimeout(fetchLatestList, 1500); // Refresh list after attempt
    }
  }

  // Handles Power Off All button click
  async function handlePowerOffAll() {
    if (isLoading || isBulkLoading) return;
    isBulkLoading = true;
    statusMessage = "Attempting to power OFF all stations...";
    try {
      await PowerOffAllStations();
      statusMessage = "Sent Power OFF command to all stations. Refreshing list...";
    } catch (error) {
      statusMessage = `Error powering off all stations: ${error}`;
      console.error("Error PowerOffAll:", error);
    } finally {
      isBulkLoading = false;
      setTimeout(fetchLatestList, 1500); // Refresh list after attempt
    }
  }

  function getPowerStateText(state: number): string {
    // Restore original logic
    switch (state) {
      case 0: return 'Off';
      case 1: return 'On';
      default: return 'Unknown';
    }
  }

  // No polling timeout to clear in onDestroy

</script>

<main>
  <h1>Lighthouse Control</h1>

  <div class="controls">
     <button class="btn" on:click={handleScanClick} disabled={isLoading || isBulkLoading}>
       {#if isLoading}Scanning...{:else}Scan{/if}
     </button>
     <button class="btn" on:click={handlePowerOnAll} disabled={isLoading || isBulkLoading || stations.length === 0}>
       {#if isBulkLoading}Working...{:else}Power On All{/if}
     </button>
     <button class="btn" on:click={handlePowerOffAll} disabled={isLoading || isBulkLoading || stations.length === 0}>
       {#if isBulkLoading}Working...{:else}Power Off All{/if}
     </button>
  </div>

  <p class="status">{statusMessage}</p>

  {#if stations.length > 0}
      <h2>Discovered Base Stations</h2>
      <ul class="station-list">
        {#each stations as station (station.address)}
          <li class="station-item">
            <span class="station-name">{station.name}</span>
            <span class="station-address">({station.address})</span>
            <span class="station-state">
              State: <strong>{getPowerStateText(station.powerState)}</strong>
            </span>
            <!-- Restore Toggle Button functionality -->
            <button
              class="btn toggle-btn"
              on:click={() => togglePower(station)}
              disabled={station.powerState === -1 || operationInProgress[station.address] || isLoading || isBulkLoading}
              title={station.powerState === -1 ? "Power state unknown" : `Turn ${station.powerState === 0 ? 'On' : 'Off'}`}
            >
              {#if operationInProgress[station.address]}
                  Working...
              {:else}
                  Toggle Power ({station.powerState === 0 ? 'On' : 'Off'})
              {/if}
            </button>
          </li>
        {/each}
      </ul>
  {:else if !isLoading && !isBulkLoading}
      <p>List is empty. Click 'Scan' to find base stations.</p>
   {:else if isLoading}
       <p>Scanning...</p>
   {:else if isBulkLoading}
       <p>Performing bulk operation...</p>
  {/if}

</main>

<style>
  /* Global styles */
  * {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
  }

  :global(html) {
      height: 100%; /* Define viewport height */
      /* Hide scrollbar attempts on html */
      scrollbar-width: none; /* Firefox */
      -ms-overflow-style: none;  /* IE and Edge */
  }
   :global(html::-webkit-scrollbar) {
      display: none; /* Chrome, Safari, Opera */
  }

  :global(body) {
      height: 100%; /* Ensure body takes full height */
      overflow-y: auto; /* Allow scrolling if content exceeds viewport */
      background-color: #1b2636;
      color: #eee;
      font-family: sans-serif;

      /* Reinforce scrollbar hiding on body */
      scrollbar-width: none; /* Firefox */
      -ms-overflow-style: none;  /* IE and Edge */
  }

 :global(body::-webkit-scrollbar) {
    display: none; /* Chrome, Safari, Opera */
 }

  main {
    max-width: 600px;
    margin: 0 auto; /* Remove top/bottom margin, keep horizontal auto centering */
    padding: 20px;
    text-align: center;
  }

  h1 {
    color: #67b4e3; /* Wails-like blue */
    margin-bottom: 1.5rem; /* Keep existing */
  }

  .controls {
    margin-bottom: 1.5rem;
    display: flex;
    justify-content: center;
    gap: 1rem;
  }

  .status {
    margin-bottom: 1.5rem;
    font-style: italic;
    color: #aaa;
  }

  .btn {
    padding: 8px 15px;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    background-color: #4a5a72; /* Darker button */
    color: white;
    transition: background-color 0.2s ease;
    font-size: 0.9rem;
  }

  .btn:hover:not(:disabled) {
    background-color: #5a6a82; /* Lighter on hover */
  }

  .btn:disabled {
    background-color: #3a4a62; /* Even darker when disabled */
    color: #888;
    cursor: not-allowed;
  }

  h2 {
      color: #67b4e3;
      margin-top: 2rem;
      margin-bottom: 1rem;
      border-bottom: 1px solid #4a5a72;
      padding-bottom: 0.5rem;
  }

  .station-list {
    list-style: none;
    padding: 0;
    margin: 0;
    text-align: left;
  }

  .station-item {
    background-color: #2a3a52; /* Slightly lighter than main background */
    padding: 15px;
    margin-bottom: 10px;
    border-radius: 5px;
    border: 1px solid #3a4a62;
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap; /* Allow wrapping on smaller screens */
    gap: 10px;
  }

  .station-name {
    font-weight: bold;
    flex-basis: 150px; /* Give name some space */
    flex-grow: 1;
  }

  .station-address {
    font-size: 0.85em;
    color: #aaa;
    flex-basis: 180px; /* Give address space */
    flex-grow: 1;
  }

 .station-state {
     font-size: 0.9em;
     min-width: 100px; /* Ensure state text doesn't wrap too easily */
     text-align: right;
     flex-grow: 1;
 }

  .station-state strong {
      color: #67b4e3;
  }

  /* Restore toggle-btn style */
  .toggle-btn {
      min-width: 130px; /* Ensure button text fits */
      flex-shrink: 0; /* Prevent button from shrinking too much */
  }

</style>
