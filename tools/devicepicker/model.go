package devicepicker

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"go-mediatoolkit/devices"
)

type stage int

const (
	stageInput stage = iota
	stageOutput
	stageDone
)

type model struct {
	title string

	wantInput  bool
	wantOutput bool

	inputs  []devices.Device
	outputs []devices.Device

	stage           stage
	inputCursor     int
	outputCursor    int
	inputConfirmed  bool
	outputConfirmed bool
	cancelled       bool
}

func newModel(title string, wantIn, wantOut bool, ins, outs []devices.Device) *model {
	m := &model{
		title:      title,
		wantInput:  wantIn,
		wantOutput: wantOut,
		inputs:     ins,
		outputs:    outs,
	}
	if wantIn {
		m.stage = stageInput
		m.inputCursor = defaultIndex(ins)
	} else {
		m.stage = stageOutput
		m.outputCursor = defaultIndex(outs)
	}
	if !wantOut && !wantIn {
		m.stage = stageDone
	}
	if wantOut {
		m.outputCursor = defaultIndex(outs)
	}
	return m
}

func defaultIndex(devs []devices.Device) int {
	for i, d := range devs {
		if d.IsDefault {
			return i
		}
	}
	return 0
}

func sortDevices(devs []devices.Device) {
	sort.SliceStable(devs, func(i, j int) bool {
		// Defaults first, then alphabetical by name.
		if devs[i].IsDefault != devs[j].IsDefault {
			return devs[i].IsDefault
		}
		return devs[i].Name < devs[j].Name
	})
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.cancelled = true
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "d":
		m.jumpToDefault()
	case "enter", " ":
		return m.confirm()
	}
	return m, nil
}

func (m *model) moveCursor(delta int) {
	switch m.stage {
	case stageInput:
		m.inputCursor = clamp(m.inputCursor+delta, 0, len(m.inputs)-1)
	case stageOutput:
		m.outputCursor = clamp(m.outputCursor+delta, 0, len(m.outputs)-1)
	}
}

func (m *model) jumpToDefault() {
	switch m.stage {
	case stageInput:
		m.inputCursor = defaultIndex(m.inputs)
	case stageOutput:
		m.outputCursor = defaultIndex(m.outputs)
	}
}

func (m *model) confirm() (tea.Model, tea.Cmd) {
	switch m.stage {
	case stageInput:
		m.inputConfirmed = true
		if m.wantOutput {
			m.stage = stageOutput
			return m, nil
		}
		m.stage = stageDone
		return m, tea.Quit
	case stageOutput:
		m.outputConfirmed = true
		m.stage = stageDone
		return m, tea.Quit
	}
	return m, nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	defaultMark   = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	confirmedMark = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func (m *model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")

	if m.wantInput {
		m.renderList(&b, "Input", m.inputs, m.inputCursor, m.stage == stageInput, m.inputConfirmed)
		b.WriteString("\n")
	}
	if m.wantOutput {
		m.renderList(&b, "Output", m.outputs, m.outputCursor, m.stage == stageOutput, m.outputConfirmed)
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("↑/↓ move · enter confirm · d default · q cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m *model) renderList(b *strings.Builder, label string, devs []devices.Device, cursor int, active, confirmed bool) {
	header := label
	switch {
	case confirmed:
		header += " " + confirmedMark.Render("✓ "+devs[cursor].Name)
	case active:
		header = headerStyle.Render(header + " ▸")
	default:
		header = helpStyle.Render(header)
	}
	b.WriteString(header)
	b.WriteString("\n")

	if confirmed {
		return
	}
	for i, d := range devs {
		prefix := "  "
		name := d.Name
		if active && i == cursor {
			prefix = cursorStyle.Render("▸ ")
			name = cursorStyle.Render(name)
		}
		if d.IsDefault {
			name += " " + defaultMark.Render("(default)")
		}
		b.WriteString(prefix)
		b.WriteString(name)
		b.WriteString("\n")
	}
}
