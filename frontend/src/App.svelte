<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import {
    ScanAndFetchStations,
    GetCurrentStationInfo,
    PowerOnStation,
    PowerOffStation,
    PowerOnAllStations,
    PowerOffAllStations,
    RenameStation,
    CheckAllStationStatuses,
    IsScanning
  } from '../wailsjs/go/main/App';
  import {
    RefreshCw,
    Power,
    Edit2,
    Check,
    X,
    Zap,
    Activity,
    Loader2,
    Bluetooth
  } from 'lucide-svelte';

  interface StationInfo {
    name: string;
    originalName: string;
    address: string;
    powerState: number; // -1: Unknown, 0: Off, 1: On
  }

  let stations: StationInfo[] = [];
  let statusMessage: string = "Ready to scan.";
  let operationInProgress: { [address: string]: boolean } = {};
  let isLoading: boolean = false;
  let isBulkLoading: boolean = false;

  // --- Renaming State --- //
  let editingAddress: string | null = null;
  let editingName: string = '';
  let nameInput: HTMLInputElement;

  let statusCheckInterval: any = null;

  // --- Reactive Sorting --- //
  $: sortedStations = [...stations].sort((a, b) => a.address.localeCompare(b.address));

  // --- Lifecycle --- //
  onMount(() => {
    statusCheckInterval = setInterval(periodicStatusCheck, 15000);
    handleScanClick();
  });

  onDestroy(() => {
    if (statusCheckInterval) {
      clearInterval(statusCheckInterval);
    }
  });

  // --- Periodic Status Check --- //
  async function periodicStatusCheck() {
    try {
      const scanning = await IsScanning();
      if (!scanning && !isLoading && !isBulkLoading) {
        const currentList = await CheckAllStationStatuses();
        stations = currentList || [];
      }
    } catch (error) {
      console.error("Error during periodic status check:", error);
    }
  }

  // Handles the Scan button click
  async function handleScanClick() {
    if (isLoading || isBulkLoading) return;
    isLoading = true;
    statusMessage = "Scanning for base stations...";
    operationInProgress = {};

    try {
      const result = await ScanAndFetchStations();
      stations = result || [];
      if (stations.length > 0) {
        statusMessage = `Found ${stations.length} station(s).`;
      } else {
        statusMessage = "No stations found.";
      }
    } catch (error) {
      statusMessage = `Scan failed: ${error}`;
      console.error("Error scan/update:", error);
    } finally {
      isLoading = false;
    }
  }

  async function fetchLatestList() {
       cancelRename();
       try {
           const currentList = await GetCurrentStationInfo();
           stations = currentList || [];
       } catch (error) {
           console.error("Error fetching list:", error);
           statusMessage = `Error refreshing list: ${error}`;
       }
  }

  async function togglePower(station: StationInfo) {
    if (station.powerState === -1 || operationInProgress[station.address] || isLoading || isBulkLoading) {
      return;
    }

    const targetState = station.powerState === 0 ? 'ON' : 'OFF';

    // Optimistic UI update could be done here, but we wait for confirmation for reliability
    statusMessage = `Turning ${station.name} ${targetState}...`;
    operationInProgress = { ...operationInProgress, [station.address]: true };
    stations = [...stations];

    try {
      if (station.powerState === 0) {
        await PowerOnStation(station.address);
      } else {
        await PowerOffStation(station.address);
      }
      statusMessage = `Turned ${station.name} ${targetState}.`;
      setTimeout(fetchLatestList, 1500);
    } catch (error) {
      statusMessage = `Failed to toggle ${station.name}: ${error}`;
      console.error(`Error toggling power for ${station.name}:`, error);
    } finally {
       operationInProgress = { ...operationInProgress, [station.address]: false };
       stations = [...stations];
    }
  }

  async function handlePowerOnAll() {
    if (isLoading || isBulkLoading) return;
    isBulkLoading = true;
    statusMessage = "Powering ON all stations...";
    try {
      await PowerOnAllStations();
      statusMessage = "Power ON command sent.";
    } catch (error) {
      statusMessage = `Error powering on all: ${error}`;
    } finally {
      isBulkLoading = false;
      setTimeout(fetchLatestList, 1500);
    }
  }

  async function handlePowerOffAll() {
    if (isLoading || isBulkLoading) return;
    isBulkLoading = true;
    statusMessage = "Powering OFF all stations...";
    try {
      await PowerOffAllStations();
      statusMessage = "Power OFF command sent.";
    } catch (error) {
      statusMessage = `Error powering off all: ${error}`;
    } finally {
      isBulkLoading = false;
      setTimeout(fetchLatestList, 1500);
    }
  }

  // --- Renaming Logic --- //
  async function startRename(station: StationInfo) {
    if (isLoading || isBulkLoading || operationInProgress[station.address]) return;
    cancelRename();
    editingAddress = station.address;
    editingName = station.name;
    await tick();
    nameInput?.focus();
    nameInput?.select();
  }

  function cancelRename() {
    editingAddress = null;
    editingName = '';
  }

  async function saveRename(station: StationInfo) {
    const newNameTrimmed = editingName.trim();
    const originalNameToUpdate = station.originalName;

    if (newNameTrimmed === station.name) {
      cancelRename();
      return;
    }

    cancelRename();
    // Don't set global isLoading to avoid blocking everything, just show status
    statusMessage = "Updating name...";

    try {
      if (newNameTrimmed === "") {
        await RenameStation(originalNameToUpdate, "");
        statusMessage = `Reset name for ${originalNameToUpdate}.`;
      } else {
        await RenameStation(originalNameToUpdate, newNameTrimmed);
        statusMessage = `Renamed to ${newNameTrimmed}.`;
      }
      // Fetching for consistency after a short delay to allow backend to update
      setTimeout(fetchLatestList, 500);
    } catch (error) {
      statusMessage = `Error renaming: ${error}`;
    }
  }

  function handleRenameKeydown(event: KeyboardEvent, station: StationInfo) {
    if (event.key === 'Enter') {
      saveRename(station);
    } else if (event.key === 'Escape') {
      cancelRename();
    }
  }
</script>

<div class="app-container">
  <header>
    <div class="title-group">
      <div class="logo-icon">
        <Activity size={24} color="var(--color-primary)" />
      </div>
      <h1>Lighthouse Control</h1>
    </div>

    <div class="global-controls">
       <button class="btn btn-primary" on:click={handleScanClick} disabled={isLoading || isBulkLoading}>
         {#if isLoading}
           <Loader2 class="spin" size={16} />
           <span>Scanning...</span>
         {:else}
           <RefreshCw size={16} />
           <span>Scan</span>
         {/if}
       </button>

       <div class="button-group">
         <button class="btn btn-surface" on:click={handlePowerOnAll} disabled={isLoading || isBulkLoading || stations.length === 0}>
            {#if isBulkLoading}
              <Loader2 class="spin" size={16} />
            {:else}
              <Zap size={16} />
            {/if}
            <span>All On</span>
         </button>
         <button class="btn btn-surface" on:click={handlePowerOffAll} disabled={isLoading || isBulkLoading || stations.length === 0}>
            {#if isBulkLoading}
              <Loader2 class="spin" size={16} />
            {:else}
              <Power size={16} />
            {/if}
            <span>All Off</span>
         </button>
       </div>
    </div>
  </header>

  <main>
    {#if sortedStations.length > 0}
        <div class="station-grid">
          {#each sortedStations as station (station.address)}
            <div
              class="station-card"
              class:is-on={station.powerState === 1}
              class:is-off={station.powerState === 0}
              class:is-unknown={station.powerState === -1}
            >
              <div class="card-content">
                <div class="station-identity">
                  {#if editingAddress === station.address}
                    <div class="rename-container">
                      <input
                        type="text"
                        bind:this={nameInput}
                        bind:value={editingName}
                        on:keydown={(e) => handleRenameKeydown(e, station)}
                        on:blur={cancelRename}
                        class="rename-input"
                        placeholder="Station Name"
                      />
                      <button class="icon-btn success" on:mousedown|preventDefault={() => saveRename(station)}>
                        <Check size={16} />
                      </button>
                      <button class="icon-btn danger" on:mousedown|preventDefault={cancelRename}>
                        <X size={16} />
                      </button>
                    </div>
                  {:else}
                    <div class="name-row">
                      <h3 title={station.name}>{station.name}</h3>
                      <button class="icon-btn ghost" on:click={() => startRename(station)} title="Rename">
                        <Edit2 size={12} />
                      </button>
                    </div>
                    <div class="info-row">
                      <Bluetooth size={12} class="text-muted" />
                      <span class="address">{station.address}</span>
                      {#if station.name !== station.originalName}
                        <span class="original-name">({station.originalName})</span>
                      {/if}
                    </div>
                  {/if}
                </div>
              </div>

              <div class="card-action">
                <button
                  class="btn btn-sm toggle-btn"
                  class:btn-success={station.powerState === 0}
                  class:btn-danger={station.powerState === 1}
                  on:click={() => togglePower(station)}
                  disabled={station.powerState === -1 || operationInProgress[station.address] || isLoading || isBulkLoading}
                >
                  {#if operationInProgress[station.address]}
                      <Loader2 class="spin" size={16} />
                  {:else}
                      <Power size={16} />
                      <span>{station.powerState === 0 ? 'Turn On' : 'Turn Off'}</span>
                  {/if}
                </button>
              </div>
            </div>
          {/each}
        </div>
    {:else if !isLoading && !isBulkLoading}
        <div class="empty-state">
          <Activity size={48} color="var(--text-muted)" />
          <p>No base stations found.</p>
          <button class="btn btn-primary" on:click={handleScanClick}>Scan Now</button>
        </div>
     {:else if isLoading}
         <div class="loading-state">
            <Loader2 class="spin" size={32} color="var(--color-primary)" />
            <p>Scanning...</p>
         </div>
    {/if}
  </main>

  <div class="status-bar">
    <div class="status-content">
      <Activity size={12} />
      <span>{statusMessage}</span>
    </div>
  </div>
</div>

<style>
  .app-container {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background-color: var(--bg-app);
  }

  header {
    padding: var(--spacing-md);
    background-color: var(--bg-surface);
    border-bottom: 1px solid var(--color-border);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-sm);
    box-shadow: var(--shadow-sm);
  }

  .title-group {
    display: flex;
    align-items: center;
    gap: var(--spacing-sm);
    justify-content: center;
  }

  .logo-icon {
    display: flex;
    align-items: center;
    justify-content: center;
  }

  h1 {
    font-size: 1.1rem;
    font-weight: 700;
    margin: 0;
    color: var(--text-primary);
  }

  .global-controls {
    display: flex;
    gap: var(--spacing-sm);
    justify-content: center;
  }

  .button-group {
    display: flex;
    gap: var(--spacing-xs);
    background-color: var(--bg-app);
    padding: 2px;
    border-radius: var(--radius-md);
  }

  main {
    flex: 1;
    padding: var(--spacing-md);
    overflow-y: auto;
    position: relative;
    max-width: 100%;
    margin: 0 auto;
    width: 100%;
    box-sizing: border-box;
  }

  /* Buttons */
  .btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-sm);
    padding: 0.4rem 0.8rem;
    border: none;
    border-radius: var(--radius-md);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: var(--transition);
    color: white;
  }

  .btn-sm {
      padding: 0.3rem 0.6rem;
      font-size: 0.8rem;
  }

  .btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .btn-primary {
    background-color: var(--color-primary);
  }
  .btn-primary:hover:not(:disabled) {
    background-color: var(--color-primary-hover);
  }

  .btn-surface {
    background-color: var(--bg-surface);
    color: var(--text-secondary);
  }
  .btn-surface:hover:not(:disabled) {
    background-color: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .btn-success { background-color: var(--color-success); }
  .btn-success:hover:not(:disabled) { filter: brightness(1.1); }

  .btn-danger { background-color: var(--color-danger); }
  .btn-danger:hover:not(:disabled) { filter: brightness(1.1); }

  .icon-btn {
    background: none;
    border: none;
    cursor: pointer;
    padding: 2px;
    border-radius: var(--radius-sm);
    display: flex;
    align-items: center;
    justify-content: center;
    transition: var(--transition);
  }

  .icon-btn.ghost { color: var(--text-muted); }
  .icon-btn.ghost:hover { color: var(--text-primary); background-color: rgba(255,255,255,0.1); }

  .icon-btn.success { color: var(--color-success); }
  .icon-btn.success:hover { background-color: rgba(34, 197, 94, 0.1); }

  .icon-btn.danger { color: var(--color-danger); }
  .icon-btn.danger:hover { background-color: rgba(239, 68, 68, 0.1); }

  /* Station Grid */
  .station-grid {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-sm);
  }

  .station-card {
    background-color: var(--bg-surface);
    border-radius: var(--radius-md);
    border: 1px solid var(--color-border);
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--spacing-sm) var(--spacing-md);
    overflow: hidden;
    transition: var(--transition);
    position: relative;
    min-height: 60px;
  }

  .station-card:hover {
    transform: translateY(-1px);
    box-shadow: var(--shadow-sm);
    border-color: var(--text-muted);
  }

  /* Status indicators on card border */
  .station-card.is-on { border-left: 3px solid var(--color-success); }
  .station-card.is-off { border-left: 3px solid var(--color-danger); }
  .station-card.is-unknown { border-left: 3px solid var(--text-muted); }

  .card-content {
      flex: 1;
      display: flex;
      flex-direction: column;
      justify-content: center;
      padding-right: var(--spacing-md);
      overflow: hidden;
  }

  .station-identity {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 100%;
  }

  .name-row {
    display: flex;
    align-items: center;
    gap: var(--spacing-xs);
  }

  .name-row h3 {
    margin: 0;
    font-size: 0.95rem;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .original-name {
    font-size: 0.75rem;
    color: var(--text-muted);
    font-style: italic;
    margin-left: 4px;
  }

  .info-row {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--text-secondary);
    font-size: 0.8rem;
  }

  .card-action {
      flex-shrink: 0;
  }

  /* Renaming */
  .rename-container {
    display: flex;
    align-items: center;
    gap: var(--spacing-xs);
    width: 100%;
  }

  .rename-input {
    background-color: var(--bg-input);
    border: 1px solid var(--color-primary);
    color: white;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    font-family: inherit;
    font-size: 0.9rem;
    width: 100%;
    outline: none;
  }

  /* States */
  .empty-state, .loading-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 150px;
    color: var(--text-muted);
    gap: var(--spacing-md);
  }

  /* Status Bar */
  .status-bar {
    background-color: var(--bg-surface);
    border-top: 1px solid var(--color-border);
    padding: 2px var(--spacing-md);
    font-size: 0.75rem;
    color: var(--text-secondary);
    display: flex;
    justify-content: center;
    min-height: 24px;
  }

  .status-content {
    display: flex;
    align-items: center;
    gap: var(--spacing-sm);
  }

  /* Utilities */
  :global(.spin) {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
