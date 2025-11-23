//go:build !windows

package platform

import "log"

// BringWindowToFront is a no-op on non-Windows platforms for now.
func BringWindowToFront(appTitle string) {
	log.Println("BringWindowToFront not implemented for this platform.")
}
