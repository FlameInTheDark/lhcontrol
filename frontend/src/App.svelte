<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import {
    ScanAndFetchStations,  // Use this new method
    GetCurrentStationInfo, // Restore import
    PowerOnStation,  // Restore import
    PowerOffStation, // Restore import
    PowerOnAllStations, // Import new function
    PowerOffAllStations, // Import new function
    RenameStation, // Import the new function
    CheckAllStationStatuses, // Import new function
    IsScanning             // Import new function
  } from '../wailsjs/go/main/App';

  // Interface matching the Go StationInfo struct
  interface StationInfo {
    name: string;
    originalName: string; // Add original name
    address: string;
    powerState: number; // -1: Unknown, 0: Off, 1: On
  }

  let stations: StationInfo[] = [];
  let statusMessage: string = "Click 'Scan' to find base stations.";
  let operationInProgress: { [address: string]: boolean } = {}; // Restore state
  let isLoading: boolean = false; // Track if Scan or Toggle is in progress
  let isBulkLoading: boolean = false; // Track if bulk operation is in progress

  // --- Renaming State --- //
  let editingAddress: string | null = null;
  let editingName: string = '';
  let nameInput: HTMLInputElement;

  let statusCheckInterval: any = null; // Use 'any' to handle Node/browser type differences

  // --- Reactive Sorting --- //
  $: sortedStations = [...stations].sort((a, b) => a.address.localeCompare(b.address));

  // --- Lifecycle --- //
  onMount(() => {
    // Start periodic status check
    statusCheckInterval = setInterval(periodicStatusCheck, 15000); // 15 seconds
    // Trigger initial scan after UI mounts
    handleScanClick();
  });

  onDestroy(() => {
    // Clear interval on component destruction
    if (statusCheckInterval) {
      clearInterval(statusCheckInterval);
    }
  });

  // --- Periodic Status Check --- //
  async function periodicStatusCheck() {
    try {
      const scanning = await IsScanning();
      // Only check if not scanning, not loading, and not bulk loading
      if (!scanning && !isLoading && !isBulkLoading) {
        console.log("Performing periodic status check...");
        const currentList = await CheckAllStationStatuses();
        stations = currentList || [];
        // Update status message gently?
        // statusMessage = "Checked statuses."; // Maybe too noisy
      } else {
        console.log("Skipping periodic status check (scan/operation active).");
      }
    } catch (error) {
      console.error("Error during periodic status check:", error);
      // statusMessage = `Error during status check: ${error}`; // Can be noisy
    }
  }

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
      stations = result || []; // Assign to original `stations` array
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
       // Make sure we cancel any ongoing edit before refreshing
       cancelRename();
       try {
           const currentList = await GetCurrentStationInfo();
           stations = currentList || []; // Assign to original `stations` array
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
    stations = [...stations]; // Trigger reactivity for the list itself

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
       stations = [...stations]; // Trigger reactivity
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

  // --- Renaming Logic --- //
  async function startRename(station: StationInfo) {
    if (isLoading || isBulkLoading || operationInProgress[station.address]) return;
    // Cancel any previous edit
    cancelRename();
    editingAddress = station.address;
    editingName = station.name; // Initialize input with current name
    // Wait for the DOM to update, then focus the input
    await tick();
    nameInput?.focus();
    nameInput?.select(); // Select text for easy replacement
  }

  function cancelRename() {
    editingAddress = null;
    editingName = '';
  }

  async function saveRename(station: StationInfo) {
    const newNameTrimmed = editingName.trim();
    const addressToUpdate = station.address; // Keep track before cancelling
    const originalNameToUpdate = station.originalName;

    if (newNameTrimmed === station.name) {
      // Name hasn't changed
      cancelRename();
      return;
    }

    cancelRename(); // Switch UI back immediately for both empty and non-empty cases
    isLoading = true; // Use global loading

    if (newNameTrimmed === "") {
      // --- Resetting to Original Name --- //
      statusMessage = `Resetting name for ${originalNameToUpdate}...`;
      try {
        await RenameStation(originalNameToUpdate, ""); // Pass empty string to signal reset
        statusMessage = `Successfully reset name for ${originalNameToUpdate}. Refreshing list...`;
        // Update local state immediately
        stations = stations.map(s => {
            if (s.address === addressToUpdate) {
                return { ...s, name: originalNameToUpdate }; // Set name back to original
            }
            return s;
        });
        setTimeout(fetchLatestList, 500);
      } catch (error) {
        statusMessage = `Error resetting name: ${error}`;
        console.error("Error RenameStation (reset):", error);
        // Maybe trigger another fetch to revert local changes?
      } finally {
        isLoading = false;
      }
    } else {
      // --- Saving New Name --- //
      statusMessage = `Renaming ${originalNameToUpdate} to ${newNameTrimmed}...`;
      try {
        await RenameStation(originalNameToUpdate, newNameTrimmed);
        statusMessage = `Successfully renamed to ${newNameTrimmed}. Refreshing list...`;
        // Update local state immediately
        stations = stations.map(s => {
            if (s.address === addressToUpdate) {
                return { ...s, name: newNameTrimmed };
            }
            return s;
        });
        setTimeout(fetchLatestList, 500);
      } catch (error) {
        statusMessage = `Error renaming station: ${error}`;
        console.error("Error RenameStation (save):", error);
        // Maybe trigger another fetch to revert local changes?
      } finally {
        isLoading = false;
      }
    }
  }

  function handleRenameKeydown(event: KeyboardEvent, station: StationInfo) {
    if (event.key === 'Enter') {
      saveRename(station);
    } else if (event.key === 'Escape') {
      cancelRename();
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

  {#if sortedStations.length > 0}
      <h2>Discovered Base Stations</h2>
      <ul class="station-list">
        {#each sortedStations as station (station.address)}
          <li 
            class="station-item"
            class:power-state-on={station.powerState === 1}
            class:power-state-off={station.powerState === 0}
            class:power-state-unknown={station.powerState === -1}
          >
            <div class="station-info">
              {#if editingAddress === station.address}
                <input
                  type="text"
                  bind:this={nameInput} bind:value={editingName}
                  on:keydown={(e) => handleRenameKeydown(e, station)}
                  on:blur={cancelRename}
                  class="rename-input"
                  placeholder="Enter new name"
                />
              {:else}
                <span class="station-name" on:click={() => startRename(station)} title="Click to rename">
                  {station.name}
                </span>
              {/if}
              {#if station.name !== station.originalName && editingAddress !== station.address}
                <span class="station-original-name">({station.originalName})</span>
              {/if}
              <span class="station-address">({station.address})</span>
            </div>
            <div class="station-controls">
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
            </div>
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

<!-- ADDED status bar outside main -->
<div class="status-bar">{statusMessage}</div>

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
    margin: 0 auto;
    padding: 20px;
    padding-bottom: 40px; /* Add padding to prevent overlap with status bar */
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
    background-color: #2a3a52;
    padding: 15px;
    padding-left: 20px; /* Add more left padding to make room for border */
    margin-bottom: 10px;
    border-radius: 5px;
    border: 1px solid #3a4a62;
    border-left-width: 5px; /* Define border width */
    border-left-style: solid;
    border-left-color: #5a6a82; /* Default/Unknown color (greyish) */
    display: flex;
    flex-direction: column;
    text-align: left;
    gap: 10px;
    transition: border-left-color 0.3s ease;
  }

  /* Power state specific border colors */
  .station-item.power-state-on {
      border-left-color: #4CAF50; /* Green */
  }

  .station-item.power-state-off {
      border-left-color: #F44336; /* Red */
  }

  .station-item.power-state-unknown {
      border-left-color: #5a6a82; /* Explicit grey for unknown */
  }

  .station-info {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5em;
    width: 100%;
  }

  .station-name {
    font-weight: bold;
    flex-grow: 1;
    cursor: pointer; /* Indicate clickable */
    padding: 2px 4px; /* Add slight padding for easier clicking */
    border-radius: 3px;
    transition: background-color 0.2s ease;
  }
  .station-name:hover {
      background-color: rgba(255, 255, 255, 0.1);
  }

  .station-original-name {
      font-size: 0.8em;
      color: #aaa;
      font-style: italic;
      margin-left: 5px;
  }

  .station-address {
    font-size: 0.85em;
    color: #aaa;
    white-space: nowrap;
  }

  .station-controls {
      display: flex;
      align-items: center;
      justify-content: flex-end; /* Align button to the right */
      gap: 10px;
      width: 100%;
      margin-top: 5px;
      /* REMOVED: No need for justify-content if state text is gone */
      /* REMOVED: margin-right: auto; from station-state */
  }

  .rename-input {
      font-family: inherit;
      font-size: inherit;
      font-weight: bold;
      padding: 2px 4px;
      border: 1px solid #67b4e3;
      background-color: #3a4a62;
      color: #eee;
      border-radius: 3px;
      flex-grow: 1; /* Allow input to take space */
      min-width: 100px;
  }

  .toggle-btn {
      min-width: 130px;
      flex-shrink: 0;
  }

  /* ADDED status-bar styles */
  .status-bar {
      position: fixed;
      bottom: 0;
      left: 0;
      width: 100%;
      background-color: #2a3a52; /* Slightly different from body for visibility */
      border-top: 1px solid #3a4a62;
      color: #aaa;
      padding: 6px 15px;
      font-size: 0.85em;
      text-align: center;
      z-index: 10;
  }

</style>
