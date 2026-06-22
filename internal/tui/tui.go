package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SalzDevs/agenttop/internal/event"
	"github.com/SalzDevs/agenttop/internal/store"
)

// ── Palette ──────────────────────────────────────────────────────────────
// Matches the logo exactly: 5 colors, cyan as the accent.

var (
	// Logo colors
	cyan   = lipgloss.Color("#56D4DD") // accent — brand, sparkline high, live
	purple = lipgloss.Color("#A78BFA") // sparkline mid, secondary accent
	muted  = lipgloss.Color("#6B7280") // labels, secondary text
	dim    = lipgloss.Color("#3A3F47") // separators, sparkline low

	// Brand — the sparkline logo rendered in cyan/purple/dim blocks
	brandStyle = lipgloss.NewStyle().Foreground(cyan).Bold(true)

	// Stats — borderless colored labels + values
	statLabelStyle = lipgloss.NewStyle().Foreground(muted)
	costValueStyle = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	burnValueStyle = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	liveValueStyle = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	statValueStyle = lipgloss.NewStyle().Foreground(muted).Bold(true)

	// Separator
	sepStyle = lipgloss.NewStyle().Foreground(dim)

	// Detail
	mutedStyle = lipgloss.NewStyle().Foreground(muted)

	// Sparkline
	sparkHigh = lipgloss.NewStyle().Foreground(cyan)
	sparkMid  = lipgloss.NewStyle().Foreground(purple)
	sparkLow  = lipgloss.NewStyle().Foreground(dim)

	// Provider model colors — only cyan and purple from the logo
	anthStyle   = lipgloss.NewStyle().Foreground(purple)
	openaiStyle = lipgloss.NewStyle().Foreground(cyan)
	ocStyle     = lipgloss.NewStyle().Foreground(cyan)

	// Status
	inFlightStyle = lipgloss.NewStyle().Foreground(cyan)
	errStyle      = lipgloss.NewStyle().Foreground(muted)
	okStyle       = lipgloss.NewStyle().Foreground(dim)
)

// logo is the sparkline wordmark from the logo image — 5 bars in the
// palette colors: dim, muted, purple, cyan, cyan.
const logo = "▁▃▅▇█"

var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

type row struct {
	trace     int64
	time      time.Time
	provider  string
	model     string
	status    int
	streaming bool
	inFlight  bool
	inTok     int
	outTok    int
	cost      float64
	duration  time.Duration
	prompt    string
	response  string
	err       string
}

type tickMsg time.Time
type eventMsg struct{ e event.Event }

type Model struct {
	store   *store.Store
	bus     *event.Bus
	sub     chan event.Event
	port    int

	rows     []*row
	byTrace  map[int64]*row
	maxRows  int

	spinner  spinner.Model
	viewport viewport.Model
	ready    bool
	width    int
	height   int
	costWin  []timeDurCost

	sparkData   []float64
	sparkBucket float64
	sparkTick   int
}

type timeDurCost struct {
	t    time.Time
	cost float64
}

func New(s *store.Store, b *event.Bus, port int) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	vp := viewport.New(80, 4)
	return Model{
		store:     s,
		bus:       b,
		sub:       b.Subscribe(),
		port:      port,
		byTrace:   make(map[int64]*row),
		maxRows:   100,
		spinner:   sp,
		viewport:  vp,
		sparkData: make([]float64, 0, 40),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, waitForEvent(m.sub), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func waitForEvent(ch chan event.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return eventMsg{e}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = 4
		m.ready = true
	case tickMsg:
		m.pruneCostWindow()
		m.updateSpark()
		return m, tea.Batch(tickCmd(), m.spinner.Tick)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case eventMsg:
		m.applyEvent(msg.e)
		return m, waitForEvent(m.sub)
	}
	return m, nil
}

func (m *Model) applyEvent(e event.Event) {
	r, ok := m.byTrace[e.TraceID]
	if !ok {
		r = &row{trace: e.TraceID, time: e.Time}
		m.byTrace[e.TraceID] = r
		m.rows = append(m.rows, r)
		if len(m.rows) > m.maxRows {
			old := m.rows[0]
			m.rows = m.rows[1:]
			delete(m.byTrace, old.trace)
		}
	}
	r.time = e.Time
	r.provider = e.Provider
	r.model = e.Model
	r.streaming = e.Streaming
	r.prompt = e.PromptPreview
	if e.InFlight() {
		r.inFlight = true
		r.err = e.Err
		return
	}
	r.inFlight = false
	r.status = e.Status
	r.inTok = e.InputTokens
	r.outTok = e.OutputTokens
	r.cost = e.CostUSD
	r.duration = e.Duration
	r.response = e.ResponsePreview
	r.err = e.Err
	if e.CostUSD > 0 {
		m.costWin = append(m.costWin, timeDurCost{t: e.Time, cost: e.CostUSD})
		m.sparkBucket += e.CostUSD
	}
}

func (m *Model) pruneCostWindow() {
	cutoff := time.Now().Add(-60 * time.Second)
	keep := m.costWin[:0]
	for _, c := range m.costWin {
		if c.t.After(cutoff) {
			keep = append(keep, c)
		}
	}
	m.costWin = keep
}

func (m *Model) updateSpark() {
	m.sparkTick++
	if m.sparkTick >= 4 {
		m.sparkData = append(m.sparkData, m.sparkBucket)
		if len(m.sparkData) > 40 {
			m.sparkData = m.sparkData[1:]
		}
		m.sparkBucket = 0
		m.sparkTick = 0
	}
}

func (m Model) burnPerHour() float64 {
	var sum float64
	for _, c := range m.costWin {
		sum += c.cost
	}
	return sum * 60
}

func (m Model) View() string {
	if !m.ready {
		return "    " + mutedStyle.Render("starting agenttop…")
	}

	cost, in, out, reqs, inFlight := m.store.Stats()
	burn := m.burnPerHour()

	// ── Header: logo + brand + inline stats + sparkline, all on 2 lines ──
	header := m.renderHeader(cost, in, out, reqs, inFlight, burn)

	// ── Separator ──
	sep := "  " + sepStyle.Render(strings.Repeat("─", m.width-4))

	// ── Request list ──
	list := m.renderList()

	// ── Detail (latest request) ──
	detail := m.renderDetail()

	return strings.Join([]string{header, "", sep, list, "", "", detail}, "\n")
}

func (m Model) renderHeader(cost float64, in, out, reqs, inFlight int, burn float64) string {
	// Line 1: logo (5 colored bars) + brand + key stats inline
	logoRendered := sparkLow.Render("▁") + mutedStyle.Render("▃") + sparkMid.Render("▅") + sparkHigh.Render("▇█")
	logoBrand := "  " + logoRendered + "  " + brandStyle.Render("agenttop")

	stat := func(label, value string, valStyle lipgloss.Style) string {
		return statLabelStyle.Render(label+" ") + valStyle.Render(value)
	}

	line1 := "  " + lipgloss.JoinHorizontal(lipgloss.Center,
		logoBrand,
		"   ",
		stat("cost", fmt.Sprintf("$%.4f", cost), costValueStyle),
		"   ",
		stat("burn", fmt.Sprintf("$%.2f/h", burn), burnValueStyle),
		"   ",
		stat("live", fmt.Sprintf("%d", inFlight), liveValueStyle),
		"   ",
		stat("in", fmt.Sprintf("%d", in), statValueStyle),
		"   ",
		stat("out", fmt.Sprintf("%d", out), statValueStyle),
		"   ",
		stat("reqs", fmt.Sprintf("%d", reqs), statValueStyle),
	)

	// Line 2: sparkline
	spark := m.renderSparkline()

	return line1 + "\n\n\n" + spark
}

func (m Model) renderSparkline() string {
	if len(m.sparkData) < 2 {
		return "    " + mutedStyle.Render("collecting data…")
	}

	maxVal := 0.0
	for _, v := range m.sparkData {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	var b strings.Builder
	b.WriteString("    ")
	for _, v := range m.sparkData {
		idx := int(v / maxVal * float64(len(sparkBlocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		c := string(sparkBlocks[idx])
		switch {
		case v > maxVal*0.66:
			b.WriteString(sparkHigh.Render(c))
		case v > maxVal*0.33:
			b.WriteString(sparkMid.Render(c))
		default:
			b.WriteString(sparkLow.Render(c))
		}
	}
	b.WriteString("  ")
	b.WriteString(sparkMid.Render(fmt.Sprintf("$%.2f/h", m.burnPerHour())))
	return b.String()
}

func modelColor(model, provider string) string {
	switch provider {
	case "anthropic":
		return anthStyle.Render(model)
	case "openai":
		return openaiStyle.Render(model)
	case "opencode", "opencode-go":
		return ocStyle.Render(model)
	default:
		return mutedStyle.Render(model)
	}
}

func statusSymbol(r *row) string {
	if r.inFlight {
		return inFlightStyle.Render("●")
	}
	if r.err != "" {
		return errStyle.Render("✗")
	}
	return okStyle.Render("✓")
}

func (m Model) renderList() string {
	if len(m.rows) == 0 {
		return "    " + mutedStyle.Render("waiting for requests…")
	}

	maxVisible := 4
	if m.height < 20 {
		maxVisible = 2
	}
	start := 0
	if len(m.rows) > maxVisible {
		start = len(m.rows) - maxVisible
	}

	var lines []string
	for i := start; i < len(m.rows); i++ {
		r := m.rows[i]
		model := r.model
		if model == "" {
			model = "-"
		}

		costStr := fmt.Sprintf("$%.4f", r.cost)
		if r.inFlight {
			costStr = mutedStyle.Render("…")
		} else if r.cost > 0 {
			costStr = costValueStyle.Render(costStr)
		}

		durStr := fmt.Sprintf("%.2fs", r.duration.Seconds())
		if r.inFlight {
			durStr = inFlightStyle.Render(fmt.Sprintf("%.2fs", time.Since(r.time).Seconds())) + mutedStyle.Render("…")
		} else {
			durStr = mutedStyle.Render(durStr)
		}

		line := fmt.Sprintf("    %s  %s   %s   %s  %s   %s  %s",
			statusSymbol(r),
			modelColor(fmt.Sprintf("%-22s", model), r.provider),
			durStr,
			mutedStyle.Render("in")+fmt.Sprintf(" %d", r.inTok),
			mutedStyle.Render("out")+fmt.Sprintf(" %d", r.outTok),
			mutedStyle.Render("cost"), costStr,
		)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderDetail() string {
	if len(m.rows) == 0 {
		return "    " + mutedStyle.Render("waiting for requests…")
	}
	r := m.rows[len(m.rows)-1]
	var b strings.Builder
	durStr := fmt.Sprintf("%.2fs", r.duration.Seconds())
	if r.inFlight {
		durStr = inFlightStyle.Render(fmt.Sprintf("%.2fs", time.Since(r.time).Seconds())) + mutedStyle.Render("  (live)")
	} else {
		durStr = mutedStyle.Render(durStr)
	}

	b.WriteString("    ")
	fmt.Fprintf(&b, "%s  %s   %s  %s   %s  %s\n",
		mutedStyle.Render("model"), modelColor(r.model, r.provider),
		mutedStyle.Render("provider"), mutedStyle.Render(r.provider),
		mutedStyle.Render("dur"), durStr)
	b.WriteString("    ")
	fmt.Fprintf(&b, "%s  %s   %s  %s   %s  %s\n",
		mutedStyle.Render("tokens"),
		statValueStyle.Render(fmt.Sprintf("%d ↑", r.inTok)),
		statValueStyle.Render(fmt.Sprintf("%d ↓", r.outTok)),
		mutedStyle.Render("cost"),
		costValueStyle.Render(fmt.Sprintf("$%.4f", r.cost)),
		"")
	b.WriteString("\n")
	b.WriteString("    " + mutedStyle.Render("prompt  ") + wrapText(r.prompt, m.width-12) + "\n")
	b.WriteString("    " + mutedStyle.Render("response ") + wrapText(r.response, m.width-12))
	m.viewport.SetContent(b.String())
	return m.viewport.View()
}

func wrapText(s string, width int) string {
	if s == "" {
		return mutedStyle.Render("(empty)")
	}
	if width < 4 {
		width = 4
	}
	if len([]rune(s)) <= width {
		return mutedStyle.Render(s)
	}
	return mutedStyle.Render(string([]rune(s)[:width-1])) + mutedStyle.Render("…")
}
