// Package devicepicker provides an interactive Bubbletea-based picker
// for OS audio devices. Examples that need a microphone, speaker, or
// both should call Select rather than hand-rolling enumeration loops —
// that way the example file stays focused on the timeline / filter /
// mixer pattern it exists to teach.
//
// Usage:
//
//	sys, _ := devices.GetSystem()
//	defer sys.Close()
//	sel, err := devicepicker.Select(devicepicker.Options{
//	    System: sys,
//	    Input:  true,
//	    Output: true,
//	})
//	if err != nil { log.Fatal(err) }
//	mic, speaker := *sel.Input, *sel.Output
//
// Cancellation (Ctrl-C / q) returns ErrCancelled with a partial
// Selection (whichever sides the user already confirmed).
package devicepicker

import (
	"errors"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/daniel-sullivan/go-mediatoolkit/devices"
)

// ErrCancelled is returned when the user aborts the picker.
var ErrCancelled = errors.New("devicepicker: user cancelled selection")

// Options configures Select.
type Options struct {
	// System is the device System to enumerate. Required.
	System *devices.System

	// Input requests an input-device step. The returned Selection
	// has Input non-nil only if this is true and the user confirmed.
	Input bool

	// Output requests an output-device step. Same semantics as Input.
	Output bool

	// Title is the heading shown above each list. Empty selects a
	// sensible default ("Select audio devices").
	Title string
}

// Selection is the user's chosen devices. Each pointer is nil if that
// direction was not requested or the user aborted before confirming it.
type Selection struct {
	Input  *devices.Device
	Output *devices.Device
}

// Select runs the interactive picker and blocks until the user confirms
// or aborts. It returns ErrCancelled on abort; the partial Selection is
// still returned so callers can act on whichever sides were already
// confirmed.
func Select(opts Options) (Selection, error) {
	if opts.System == nil {
		return Selection{}, errors.New("devicepicker: Options.System is required")
	}
	if !opts.Input && !opts.Output {
		return Selection{}, errors.New("devicepicker: at least one of Input/Output must be true")
	}
	title := opts.Title
	if title == "" {
		title = "Select audio devices"
	}
	all := opts.System.List()
	ins := filterByDirection(all, devices.Input)
	outs := filterByDirection(all, devices.Output)

	if opts.Input && len(ins) == 0 {
		return Selection{}, errors.New("devicepicker: no input devices available")
	}
	if opts.Output && len(outs) == 0 {
		return Selection{}, errors.New("devicepicker: no output devices available")
	}

	// Bubbletea needs a real TTY to set raw mode and read keys. When
	// stdin isn't a terminal — the typical case for IDE run configs,
	// piped sessions, and CI — fall back to OS defaults so examples
	// still work without manual bypass.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return defaultSelection(opts)
	}

	m := newModel(title, opts.Input, opts.Output, ins, outs)
	prog := tea.NewProgram(m)
	final, err := prog.Run()
	if err != nil {
		return Selection{}, fmt.Errorf("devicepicker: %w", err)
	}
	fm := final.(*model)
	sel := Selection{}
	if fm.inputConfirmed {
		d := ins[fm.inputCursor]
		sel.Input = &d
	}
	if fm.outputConfirmed {
		d := outs[fm.outputCursor]
		sel.Output = &d
	}
	if fm.cancelled {
		return sel, ErrCancelled
	}
	return sel, nil
}

// defaultSelection picks the OS default for each requested direction.
// Used as the no-TTY fallback so the picker degrades gracefully.
func defaultSelection(opts Options) (Selection, error) {
	sel := Selection{}
	if opts.Input {
		d, ok := opts.System.DefaultInput()
		if !ok {
			return sel, errors.New("devicepicker: no default input device (and stdin is not a TTY for picker)")
		}
		sel.Input = &d
	}
	if opts.Output {
		d, ok := opts.System.DefaultOutput()
		if !ok {
			return sel, errors.New("devicepicker: no default output device (and stdin is not a TTY for picker)")
		}
		sel.Output = &d
	}
	log.Printf("devicepicker: stdin is not a TTY, using OS defaults (input=%v, output=%v)",
		nameOrEmpty(sel.Input), nameOrEmpty(sel.Output))
	return sel, nil
}

func nameOrEmpty(d *devices.Device) string {
	if d == nil {
		return "-"
	}
	return d.Name
}

func filterByDirection(devs []devices.Device, dir devices.Direction) []devices.Device {
	out := make([]devices.Device, 0, len(devs))
	for _, d := range devs {
		if d.Direction == dir {
			out = append(out, d)
		}
	}
	sortDevices(out)
	return out
}
