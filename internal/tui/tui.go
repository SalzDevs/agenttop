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
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1)
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	costStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	goodStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	badStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	selStyle     = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255"))
	hdrStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250")).Background(lipgloss.Color("236"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	provAnth     = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	provOAI      = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
)

type row struct {
	trace       int64
	time        time.Time
	provider    string
	model       string
	status      int
	streaming   bool
	inFlight    bool
	inTok       int
	outTok      int
	cost        float64
	duration    time.Duration
	prompt      string
	response    string
	err         string
}

type tickMsg time.Time
type eventMsg struct{ e event.Event }

type Model struct {
	store    *store.Store
	bus      *event.Bus
	sub      chan event.Event
	port     int

	rows     []*row
	byTrace  map[int64]*row
	maxRows  int
	selected int

	spinner   spinner.Model
	viewport  viewport.Model
	ready     bool
	width     int
	height    int
	focusList bool
	lastTick  time.Time
	costWin   []timeDurCost
	quit      bool
}

type timeDurCost struct {
	t    time.Time
	cost float64
}

func New(s *store.Store, b *event.Bus, port int) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	vp := viewport.New(80, 10)
	return Model{
		store:    s,
		bus:      b,
		sub:      b.Subscribe(),
		port:     port,
		byTrace:  make(map[int64]*row),
		maxRows:  100,
		spinner:  sp,
		viewport: vp,
		focusList: true,
		lastTick: time.Now(),
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
		m.viewport.Height = max(6, msg.Height/3)
		m.ready = true
	case tickMsg:
		m.lastTick = time.Now()
		m.pruneCostWindow()
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
			m.quit = true
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

func (m Model) burnPerHour() float64 {
	var sum float64
	for _, c := range m.costWin {
		sum += c.cost
	}
	return sum * 60
}

func (m Model) View() string {
	if !m.ready {
		return "starting agenttop..."
	}

	cost, in, out, reqs, inFlight := m.store.Stats()
	burn := m.burnPerHour()

	header := titleStyle.Render("agenttop") + mutedStyle.Render(fmt.Sprintf("  :%d", m.port)) +
		"   " + costStyle.Render(fmt.Sprintf("$%.4f", cost)) +
		mutedStyle.Render(fmt.Sprintf("  in %d  out %d  reqs %d", in, out, reqs)) +
		"   " + goodStyle.Render(fmt.Sprintf("%d live", inFlight)) +
		mutedStyle.Render(fmt.Sprintf("  burn $%.2f/h", burn))
	headerLine := lipgloss.NewStyle().Width(m.width).Render(header)

	// table
	cols := []int{min(22, m.width/6), 6, 10, 8, 9, 9}
	head := padRow([]string{"model", "prov", "status", "in", "out", "cost"}, cols)
	table := hdrStyle.Render(head)
	var lines []string
	start := 0
	if len(m.rows) > m.height-12 {
		start = len(m.rows) - (m.height - 12)
	}
	for i := start; i < len(m.rows); i++ {
		r := m.rows[i]
		status := ""
		switch {
		case r.inFlight:
			status = m.spinner.View() + "stream"
		case r.err != "":
			status = "ERR"
		default:
			status = fmt.Sprintf("%d", r.status)
		}
		prov := r.provider
		if r.provider == "anthropic" {
			prov = "anth"
		} else if r.provider == "openai" {
			prov = "oai"
		}
		costStr := fmt.Sprintf("$%.4f", r.cost)
		if r.inFlight {
			costStr = "..."
		}
		model := r.model
		if model == "" {
			model = "-"
		}
		rowStr := padRow([]string{model, prov, status, fmt.Sprintf("%d", r.inTok), fmt.Sprintf("%d", r.outTok), costStr}, cols)
		if i == m.selected && m.focusList {
			rowStr = selStyle.Render(rowStr)
		} else if r.inFlight {
			rowStr = goodStyle.Render(rowStr)
		} else if r.err != "" {
			rowStr = badStyle.Render(rowStr)
		}
		lines = append(lines, rowStr)
	}
	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render("  waiting for requests... run your agent with the base URL shown above"))
	}
	body := table + "\n" + strings.Join(lines, "\n")

	// detail
	detail := m.detailView()

	footer := dimStyle.Render("q quit  •  ↑↓/jk select  •  tab/enter toggle detail  •  G bottom")
	focus := mutedStyle.Render("focus: list")
	if !m.focusList {
		focus = mutedStyle.Render("focus: detail")
	}

	return strings.Join([]string{
		headerLine,
		body,
		"",
		hdrStyle.Render(" detail"),
		detail,
		footer + "   " + focus,
	}, "\n")
}

func (m Model) detailView() string {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return mutedStyle.Render("  (no request selected)")
	}
	r := m.rows[m.selected]
	var b strings.Builder
	fmt.Fprintf(&b, "model: %s   provider: %s   status: %s   dur: %s\n",
		r.model, r.provider, statusLabel(r), r.duration)
	fmt.Fprintf(&b, "tokens: in %d  out %d   cost: $%.4f\n", r.inTok, r.outTok, r.cost)
	b.WriteString(mutedStyle.Render("prompt:\n") + wrap(r.prompt) + "\n")
	b.WriteString(mutedStyle.Render("response:\n") + wrap(r.response))
	m.viewport.SetContent(b.String())
	return m.viewport.View()
}

func statusLabel(r *row) string {
	if r.inFlight {
		return "streaming"
	}
	if r.err != "" {
		return "error: " + r.err
	}
	return fmt.Sprintf("%d", r.status)
}

func wrap(s string) string {
	if s == "" {
		return mutedStyle.Render("(empty)")
	}
	return s
}

func padRow(vals []string, cols []int) string {
	var b strings.Builder
	for i, v := range vals {
		w := 0
		if i < len(cols) {
			w = cols[i]
		}
		cell := v
		if visibleLen(v) > w {
			cell = truncate(v, w-1) + "…"
		}
		b.WriteString(cell + strings.Repeat(" ", max(1, w-visibleLen(cell))))
	}
	return b.String()
}

func truncate(s string, n int) string {
	if visibleLen(s) <= n {
		return s
	}
	r := []rune(s)
	if n <= 0 {
		return ""
	}
	return string(r[:n])
}

func visibleLen(s string) int {
	return len([]rune(s))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
