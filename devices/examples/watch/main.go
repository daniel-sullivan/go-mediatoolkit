// watch prints the initial device list, then hangs and prints every
// device-state change reported by the platform backend. Exits on
// Ctrl-C. Run with `go run ./devices/examples/watch`.
//
// On macOS with CGO_ENABLED=1 (the default) events come from real
// CoreAudio property listeners; with CGO_ENABLED=0 the System falls
// back to polling List every 2s and emits synthetic diff events.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-mediatoolkit/devices"
)

func main() {
	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()

	snap, sub := sys.Snapshot(func(ev devices.Event) {
		d := ev.Device
		fmt.Printf("[%s] %-15s  %-6s  %-40s  default=%-5v rate=%d ch=%d  id=%s\n",
			time.Now().Format("15:04:05.000"),
			ev.Kind, d.Direction, d.Name, d.IsDefault, d.SampleRate, d.Channels, d.ID)
	})
	defer sub.Cancel()

	fmt.Printf("initial: %d devices\n", len(snap))
	for _, d := range snap {
		marker := " "
		if d.IsDefault {
			marker = "*"
		}
		fmt.Printf("  %s %-6s %-40s  rate=%d ch=%d\n", marker, d.Direction, d.Name, d.SampleRate, d.Channels)
	}
	fmt.Println("watching for changes — plug/unplug a device, or switch the system default… (Ctrl-C to exit)")

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs
	fmt.Println()
}
