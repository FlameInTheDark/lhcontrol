# lhcontrol

[**⬇️ Get the latest Windows Installer from Releases**](https://github.com/FlameInTheDark/lhcontrol/releases/latest)

![Application Screenshot](<./screenshot.png>)

A simple application to control Valve Lighthouse (SteamVR) base stations v2.0 power state via Bluetooth LE.

## Features

*   Scan for nearby Lighthouse base stations.
*   Display discovered stations and their current power state (On/Off/Unknown).
*   Toggle the power state of individual base stations.
*   Power On/Off all known base stations simultaneously.
*   Persistent list of discovered stations across scans (within a single app session).

## Technology Stack

*   **Framework:** [Wails v2](https://wails.io/)
*   **Backend:** Go
*   **Frontend:** Svelte
*   **Bluetooth:** [TinyGo Bluetooth Library](https://github.com/tinygo-org/bluetooth)

## Prerequisites

*   **Bluetooth Adapter:** You MUST have a working Bluetooth adapter compatible with your OS that supports **Bluetooth Low Energy (BLE)**. Many built-in adapters work, but dedicated USB adapters can sometimes offer better performance/compatibility.
*   **Go:** Version 1.18 or higher.
*   **Node.js & npm:** Required by Wails for frontend dependencies.
*   **Wails CLI:** Install via `go install github.com/wailsapp/wails/v2/cmd/wails@latest`.
*   **TinyGo:** While the main build uses the standard Go compiler, the `tinygo/bluetooth` library is used. Ensure required system dependencies for BLE development are met (e.g., build-essential, libbluetooth-dev on Debian/Ubuntu).

## Setup

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/FlameInTheDark/lhcontrol
    cd lhcontrol
    ```
2.  **Install frontend dependencies:**
    Wails typically handles this automatically during the build, but you can run it manually if needed:
    ```bash
    cd frontend
    npm install
    cd ..
    ```

## Running the Application

*   **Development Mode:** (Live reload)
    ```bash
    wails dev
    ```
*   **Production Build:**
    ```bash
    wails build
    ```
    This will create an executable in the `build/bin` directory.
    Alternatively, for Windows users, a pre-built installer (`lhcontrol-amd64-installer.exe`) may be available in the project's releases.

## Usage

1.  Launch the application.
2.  Click **Scan** to discover nearby base stations.
3.  The application will attempt to connect to discovered stations to determine their power state.
4.  Use the **Toggle Power** button next to each station to turn it On or Off.
5.  Use the **Power On All** or **Power Off All** buttons to control all known stations simultaneously.

## Troubleshooting

*   **Scanning Issues:** If scans fail after the first time, or interactions fail with errors like "characteristic not found", try removing the base station(s) from your operating system's Bluetooth device list and restarting your computer. Do *not* re-pair them in the OS settings; the application will find them via scanning.
*   **Bluetooth Drivers:** Ensure you have the latest drivers for your Bluetooth adapter.
*   **Permissions:** The application might require specific permissions to access Bluetooth hardware.

## License

(Specify your license here, e.g., MIT License)
