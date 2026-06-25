// list prints every audio device the OS reports, flags the default for
// each direction, and exits. Run with `go run ./devices/examples/list`.
package main

import (
	"fmt"
	"log"
	"sort"

	"github.com/daniel-sullivan/go-mediatoolkit/devices"
)

func main() {
	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()

	list := sys.List()
	sort.Slice(list, func(i, j int) bool {
		if list[i].Direction != list[j].Direction {
			return list[i].Direction < list[j].Direction
		}
		return list[i].Name < list[j].Name
	})

	fmt.Printf("%d devices:\n", len(list))
	for _, d := range list {
		marker := " "
		if d.IsDefault {
			marker = "*"
		}
		fmt.Printf("  %s [%-6s] %-40s  rate=%d ch=%d  id=%s\n",
			marker, d.Direction, d.Name, d.SampleRate, d.Channels, d.ID)
	}
}
