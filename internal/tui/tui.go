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

var (
	// Brand
	brandStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213")).Padding(0, 1)

	// Stat boxes
	statBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("238"))
	statLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Faint(true)
	statValueStyle  = lipgloss.NewStyle().Bold(true)
	costValueStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	burnValueStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	liveValueStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))

	// Detail
	detailBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
	selStyle          = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255"))
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	dimStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	// Sparkline
	sparkHigh = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	sparkLow  = lipgloss.NewStyle().Foreground(lipgloss.Color("58"))
)

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
	selected int

	spinner  spinner.Model
	viewport viewport.Model
	ready    bool
	width    int
	height   int
	focusList bool
	costWin   []timeDurCost

	// Sparkline data: cost per 2-second bucket, most recent last
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
	vp := viewport.New(80, 8)
	return Model{
		store:     s,
		bus:       b,
		sub:       b.Subscribe(),
		port:      port,
		byTrace:   make(map[int64]*row),
		maxRows:   100,
		spinner:   sp,
		viewport:  vp,
		focusList: true,
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
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = max(5, m.height/4)
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
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.focusList = !m.focusList
		case "up", "k":
			if m.focusList && m.selected > 0 {
				m.selected--
			} else {
				m.viewport.LineUp(1)
			}
		case "down", "j":
			if m.focusList && m.selected < len(m.rows)-1 {
				m.selected++
			} else {
				m.viewport.LineDown(1)
			}
		case "g":
			if m.focusList {
				m.selected = 0
			}
		case "G":
			if m.focusList && len(m.rows) > 0 {
				m.selected = len(m.rows) - 1
			}
		case "enter":
			m.focusList = !m.focusList
		}
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
			if m.selected > 0 {
				m.selected--
			}
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
	if m.selected >= len(m.rows) {
		m.selected = len(m.rows) - 1
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

// updateSpark accumulates cost into a 2-second bucket, then appends it to the
// sparkline data when the bucket is full. Keeps the last 40 buckets (80s).
func (m *Model) updateSpark() {
	m.sparkTick++
	if m.sparkTick >= 4 { // 4 ticks × 500ms = 2s per bucket
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
		return "  starting agenttop..."
	}

	cost, in, out, reqs, inFlight := m.store.Stats()
	burn := m.burnPerHour()

	// ── Header bar ──
	header := m.renderHeader(cost, in, out, reqs, inFlight, burn)

	// ── Sparkline ──
	spark := m.renderSparkline()

	// ── Request selector ──
	selector := m.renderSelector()

	// ── Detail ──
	detail := m.renderDetail()

	// ── Footer ──
	footer := m.renderFooter()

	return strings.Join([]string{header, spark, selector, detail, footer}, "\n")
}

func (m Model) renderHeader(cost float64, in, out, reqs, inFlight int, burn float64) string {
	brand := brandStyle.Render("agenttop")

	stat := func(label, value string, valStyle lipgloss.Style) string {
		return statBoxStyle.Render(
			statLabelStyle.Render(label) + "\n" + valStyle.Render(value),
		)
	}

	costBox := stat("TOTAL COST", fmt.Sprintf("$%.4f", cost), costValueStyle)
	burnBox := stat("BURN/HR", fmt.Sprintf("$%.2f", burn), burnValueStyle)
	liveBox := stat("LIVE", fmt.Sprintf("%d", inFlight), liveValueStyle)
	tokBox := stat("TOKENS", fmt.Sprintf("%d↑ %d↓", in, out), statValueStyle)
	reqBox := stat("REQUESTS", fmt.Sprintf("%d", reqs), statValueStyle)

	statsRow := lipgloss.JoinHorizontal(lipgloss.Top, costBox, burnBox, liveBox, tokBox, reqBox)
	return brand + "\n" + statsRow
}

func (m Model) renderSparkline() string {
	if len(m.sparkData) < 2 {
		return mutedStyle.Render("  burn rate: collecting data...")
	}

	// Render sparkline as block characters
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
	b.WriteString(mutedStyle.Render("  burn  "))
	for _, v := range m.sparkData {
		idx := int(v / maxVal * float64(len(sparkBlocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		c := string(sparkBlocks[idx])
		if v > 0 {
			b.WriteString(sparkHigh.Render(c))
		} else {
			b.WriteString(sparkLow.Render(c))
		}
	}
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  $%.2f/h", m.burnPerHour())))
	return b.String()
}

func (m Model) renderSelector() string {
	if len(m.rows) == 0 {
		return mutedStyle.Render("  waiting for requests...")
	}

	maxVisible := m.height - 18
	if maxVisible < 3 {
		maxVisible = 3
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

		status := ""
		if r.inFlight {
			status = "●"
		} else if r.err != "" {
			status = "✗"
		} else {
			status = "✓"
		}

		costStr := fmt.Sprintf("$%.4f", r.cost)
		if r.inFlight {
			costStr = "..."
		}

		marker := " "
		if i == m.selected {
			marker = "▶"
		}

		line := fmt.Sprintf("%s %s %s  in:%d out:%d  %s", marker, status, model, r.inTok, r.outTok, costStr)
		if i == m.selected {
			line = selStyle.Render(line)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderDetail() string {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return detailBorderStyle.Render(mutedStyle.Render(" select a request to see details"))
	}
	r := m.rows[m.selected]
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s  %s  %s  %s  %s\n",
		statLabelStyle.Render("model:"), r.model,
		statLabelStyle.Render("provider:"), r.provider,
		statLabelStyle.Render("dur:"), r.duration)
	fmt.Fprintf(&b, "%s %d↑  %d↓  %s $%.4f\n",
		statLabelStyle.Render("tokens:"), r.inTok, r.outTok,
		statLabelStyle.Render("cost:"), r.cost)
	b.WriteString(mutedStyle.Render("prompt: ") + wrapText(r.prompt, m.width-6) + "\n")
	b.WriteString(mutedStyle.Render("response: ") + wrapText(r.response, m.width-6))
	m.viewport.SetContent(b.String())
	return detailBorderStyle.Render(m.viewport.View())
}

func (m Model) renderFooter() string {
	footer := dimStyle.Render(" q quit  •  ↑↓/jk select  •  tab toggle detail  •  G bottom ")
	focus := mutedStyle.Render("list")
	if !m.focusList {
		focus = mutedStyle.Render("detail")
	}
	return footer + "  " + focus
}

func wrapText(s string, width int) string {
	if s == "" {
		return mutedStyle.Render("(empty)")
	}
	if width < 4 {
		width = 4
	}
	if len([]rune(s)) <= width {
		return s
	}
	return string([]rune(s)[:width-1]) + "…"
}
