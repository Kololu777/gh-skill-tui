package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type focusArea int

const (
	focusTree focusArea = iota
	focusSkills
	focusAgents
	focusStatus
	focusMain
	focusAreaCount
)

type operationKind int

const (
	opNone operationKind = iota
	opInstall
	opPR
	opDelete
)

func (k operationKind) String() string {
	switch k {
	case opInstall:
		return "INSTALL / UPDATE"
	case opPR:
		return "PROPOSE PR"
	case opDelete:
		return "DELETE"
	default:
		return ""
	}
}

var (
	headerStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	mutedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	activeStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4"))
	borderStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	focusBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	searchStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	previewStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	markStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	warnStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dirStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	headingStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	codeStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	quoteStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	scopeUserStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10")).Bold(true)
	scopeProjStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("13")).Bold(true)
	updateStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // orange
	newStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	modifiedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
)

func scopeBadge(scope string) string {
	if scope == "user" {
		return scopeUserStyle.Render(" scope:user ")
	}
	return scopeProjStyle.Render(" scope:project ")
}

func otherScope(scope string) string {
	if scope == "user" {
		return "project"
	}
	return "user"
}

func (c skillClass) style() lipgloss.Style {
	switch c {
	case classManaged:
		return markStyle
	case classForeign:
		return errorStyle
	default:
		return warnStyle
	}
}

func styledMark(mark string) string {
	switch mark {
	case "x", "✓":
		return markStyle.Render(mark)
	case "~", "?":
		return warnStyle.Render(mark)
	case "!":
		return errorStyle.Render(mark)
	case "↓":
		return updateStyle.Render(mark)
	case "m":
		return modifiedStyle.Render(mark)
	case "O":
		return warnStyle.Render(mark)
	}
	return mark
}

type model struct {
	cfg            config
	installTargets []installTarget
	agentSelected  map[string]bool
	agentForce     map[string]bool // per-agent force install (any state)
	cliOverride    bool
	projectRoot    string
	allowed        []string
	localRoots     []string
	sourceLocal    bool
	sourceRoot     string
	approved       bool

	skills        []skill
	dirs          []dirEntry
	visibleSkills []int
	localOnly     []localOnlySkill
	visibleLocal  []int           // outside entries appended after source skills in panel 1
	localMarked   map[string]bool // neutral selections for outside entries, by stable Key
	treeShas      map[string]string
	blobShas      map[string]string
	targets       []scanTarget
	lookup        installIndex
	diffCache     map[string][]string // rendered diffs; avoids re-running diff_command per frame

	selected map[string]bool
	scope    string
	force    bool

	focus       focusArea
	detail      focusArea // left panel whose detail the main panel shows
	cursors     [focusAreaCount]int
	dirFilter   string
	query       string
	searchMode  bool
	confirmMode bool
	planKind    operationKind
	planBlocks  []string
	planWarns   []string
	plan        []planEntry
	planSkipped []string
	deletePlan  []deleteEntry
	prPlan      *prPlan
	running     bool // an operation owns the terminal through tea.Exec
	result      *executionResult
	addPick     *addPick // destination picker for outside skills; nil when inactive
	mainScroll  int

	width    int
	height   int
	ref      string
	loading  bool
	scanning bool
	loadErr  string
	status   string

	accepted  bool
	cancelled bool
}

func newModel(cfg config) model {
	installTargets := allInstallTargets()
	marked := defaultSelectedAgents()
	cliOverride := cfg.AgentArg != "" || cfg.DirArg != ""
	if cliOverride {
		marked = make(map[string]bool)
		for _, p := range installTargets {
			if cfg.AgentArg != "" && p.GHAgent == cfg.AgentArg {
				marked[p.Name] = true
			}
		}
	}
	allowed := allowedSources()
	sourceLocal := isLocalSource(cfg.Source)
	sourceRoot := ""
	if sourceLocal {
		sourceRoot, _ = localRoot(cfg.Source)
	}
	return model{
		cfg:            cfg,
		installTargets: installTargets,
		agentSelected:  marked,
		agentForce:     make(map[string]bool),
		cliOverride:    cliOverride,
		projectRoot:    findProjectRoot(),
		allowed:        allowed,
		localRoots:     allowedLocalRoots(cfg.Source),
		sourceLocal:    sourceLocal,
		sourceRoot:     sourceRoot,
		approved:       sourceApproved(cfg.Source, allowed),
		selected:       make(map[string]bool),
		localMarked:    make(map[string]bool),
		diffCache:      make(map[string][]string),
		scope:          cfg.Scope,
		force:          cfg.Force,
		focus:          focusSkills,
		detail:         focusSkills,
		loading:        true,
		scanning:       true,
		status:         "loading skills",
		width:          100,
		height:         30,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		loadSkillsCmd(m.cfg),
		scanInstalledCmd(m.installTargets, m.projectRoot, m.allowed, m.localRoots),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case skillsLoadedMsg:
		m.loading = false
		if msg.Err != nil {
			m.loadErr = msg.Err.Error()
			m.status = "failed to load skills"
			return m, nil
		}
		m.skills = msg.Skills
		m.dirs = buildDirEntries(m.skills)
		m.treeShas = msg.TreeShas
		m.blobShas = msg.BlobShas
		m.ref = msg.Ref
		m.status = fmt.Sprintf("%d skills", len(m.skills))
		m.refreshVisible()
		m.rebuildLocalOnly()
		return m, m.ensurePreviewCmd()
	case skillContentMsg:
		for i := range m.skills {
			if m.skills[i].Path != msg.Path {
				continue
			}
			m.skills[i].Loading = false
			if msg.Err != nil {
				m.skills[i].LoadErr = msg.Err.Error()
			} else {
				m.skills[i].Content = msg.Content
			}
			break
		}
		return m, nil
	case installedScannedMsg:
		m.targets = msg.Targets
		m.lookup = buildInstallIndex(m.targets, m.copyFromConfiguredSource)
		m.scanning = false
		m.cursors[focusStatus] = min(m.cursors[focusStatus], max(0, len(m.targets)-1))
		m.rebuildLocalOnly()
		return m, nil
	case operationDoneMsg:
		m.running = false
		m.resetPlan()
		m.result = &msg.Result
		m.focus = focusMain
		m.mainScroll = 0
		if len(msg.Result.Failed) > 0 {
			m.status = fmt.Sprintf("%s finished with %d failure(s)", strings.ToLower(msg.Result.Kind.String()), len(msg.Result.Failed))
		} else {
			m.status = msg.Result.Summary
			m.selected = make(map[string]bool)
			m.localMarked = make(map[string]bool)
		}
		m.scanning = true
		return m, scanInstalledCmd(m.installTargets, m.projectRoot, m.allowed, m.localRoots)
	case tea.KeyMsg:
		return m.handleKey(msg)
	default:
		return m, nil
	}
}

type executionResult struct {
	Kind      operationKind
	Summary   string
	Succeeded []string
	Failed    []string
	Skipped   []string
}

type operationDoneMsg struct {
	Result executionResult
}

// commandLog keeps a bounded, concurrency-safe tail while the same bytes are
// streamed to the real terminal. os/exec may copy stdout and stderr from
// separate goroutines, so a plain strings.Builder is not safe here.
type commandLog struct {
	mu   sync.Mutex
	data []byte
}

func (l *commandLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data = append(l.data, p...)
	const maxLog = 64 << 10
	if len(l.data) > maxLog {
		l.data = append([]byte(nil), l.data[len(l.data)-maxLog:]...)
	}
	return len(p), nil
}

func (l *commandLog) tail(maxLines int) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(l.data)), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

// prExec runs a PR plan through tea.Exec: bubbletea leaves the alt screen
// and hands the terminal over, so the user watches the gh/git run directly.
// Completion resumes into the shared panel-4 result screen automatically.
type prExec struct {
	plan   prPlan
	result executionResult
	stdin  io.Reader
	stdout io.Writer
}

func (e *prExec) SetStdin(r io.Reader)  { e.stdin = r }
func (e *prExec) SetStdout(w io.Writer) { e.stdout = w }
func (e *prExec) SetStderr(io.Writer)   {}

func (e *prExec) Run() error {
	out := e.stdout
	if out == nil {
		out = os.Stdout
	}
	write := func(format string, args ...any) error {
		_, err := fmt.Fprintf(out, format, args...)
		return err
	}
	target := e.plan.Repo
	if !e.plan.Remote {
		target = homeShorten(e.plan.SourceRoot)
	}
	if err := write("→ creating branch %s on %s (%d file(s))\n", e.plan.Branch, target, len(e.plan.Files)); err != nil {
		return err
	}
	summary, err := runPR(e.plan)
	e.result = executionResult{Kind: opPR, Summary: summary}
	if err != nil {
		e.result.Summary = "PR failed"
		e.result.Failed = append(e.result.Failed, err.Error())
		if writeErr := write("✗ %v\n", err); writeErr != nil {
			return writeErr
		}
	} else {
		e.result.Succeeded = append(e.result.Succeeded, e.plan.Title)
		if writeErr := write("✓ %s\n", summary); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// runPRCmd hands the terminal to the PR execution and resumes the TUI with
// the outcome.
func runPRCmd(plan prPlan) tea.Cmd {
	ex := &prExec{plan: plan}
	return tea.Exec(ex, func(execErr error) tea.Msg {
		result := ex.result
		if execErr != nil && len(result.Failed) == 0 {
			result.Failed = append(result.Failed, execErr.Error())
		}
		return operationDoneMsg{Result: result}
	})
}

// planExec runs Install/Update/Adopt or Delete through the same terminal
// handoff used by PR execution, then returns a structured panel-4 result.
type planExec struct {
	kind     operationKind
	installs []planEntry
	deletes  []deleteEntry
	skipped  []string
	result   executionResult
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
}

func (e *planExec) SetStdin(r io.Reader)  { e.stdin = r }
func (e *planExec) SetStdout(w io.Writer) { e.stdout = w }
func (e *planExec) SetStderr(w io.Writer) { e.stderr = w }

func (e *planExec) Run() error {
	out := e.stdout
	if out == nil {
		out = os.Stdout
	}
	errOut := e.stderr
	if errOut == nil {
		errOut = out
	}
	writeOut := func(format string, args ...any) error {
		_, err := fmt.Fprintf(out, format, args...)
		return err
	}
	writeErr := func(format string, args ...any) error {
		_, err := fmt.Fprintf(errOut, format, args...)
		return err
	}
	e.result = executionResult{Kind: e.kind, Skipped: append([]string(nil), e.skipped...)}
	for _, note := range e.skipped {
		if err := writeOut("skip: %s\n", note); err != nil {
			return err
		}
	}
	switch e.kind {
	case opInstall:
		for _, entry := range e.installs {
			if err := writeOut("→ %s %s → %s (%s)\n", strings.ToUpper(firstNonEmpty(entry.Action, "install")), entry.Skill, entry.Agent, entry.Dest); err != nil {
				return err
			}
			var log commandLog
			cmd := exec.Command(entry.Args[0], entry.Args[1:]...)
			cmd.Stdin = e.stdin
			cmd.Stdout = io.MultiWriter(out, &log)
			cmd.Stderr = io.MultiWriter(errOut, &log)
			if err := cmd.Run(); err != nil {
				failure := fmt.Sprintf("%s → %s: %v", entry.Skill, entry.Agent, err)
				if detail := log.tail(12); detail != "" {
					failure += "\n" + detail
				}
				e.result.Failed = append(e.result.Failed, failure)
				if writeErrErr := writeErr("✗ %s\n", failure); writeErrErr != nil {
					return writeErrErr
				}
				continue
			}
			if entry.TreeSha != "" && entry.DestAbs != "" && entry.SourceSkillAbs != "" {
				if err := injectLocalTracking(entry.DestAbs, entry.SourceSkillAbs, entry.TreeSha); err != nil {
					e.result.Skipped = append(e.result.Skipped, "tracking metadata: "+err.Error())
					if writeErrErr := writeErr("warning: %s\n", err); writeErrErr != nil {
						return writeErrErr
					}
				}
			}
			e.result.Succeeded = append(e.result.Succeeded, entry.Skill+" → "+entry.Agent)
		}
	case opDelete:
		for _, entry := range e.deletes {
			if err := writeOut("→ DELETE %s (%s)\n", entry.Dir, entry.Agents); err != nil {
				return err
			}
			if errs := deleteInstalled([]deleteEntry{entry}); len(errs) > 0 {
				e.result.Failed = append(e.result.Failed, errs...)
				for _, failure := range errs {
					if writeErrErr := writeErr("✗ %s\n", failure); writeErrErr != nil {
						return writeErrErr
					}
				}
				continue
			}
			e.result.Succeeded = append(e.result.Succeeded, entry.Skill+" @"+entry.Agents)
		}
	}
	e.result.Summary = fmt.Sprintf("%s: %d succeeded, %d failed", strings.ToLower(e.kind.String()), len(e.result.Succeeded), len(e.result.Failed))
	return nil
}

func runOperationCmd(kind operationKind, installs []planEntry, deletes []deleteEntry, skipped []string) tea.Cmd {
	ex := &planExec{kind: kind, installs: installs, deletes: deletes, skipped: skipped}
	return tea.Exec(ex, func(execErr error) tea.Msg {
		result := ex.result
		if execErr != nil {
			result.Failed = append(result.Failed, execErr.Error())
		}
		return operationDoneMsg{Result: result}
	})
}

func (m *model) refreshVisible() {
	m.visibleSkills = filterSkills(m.skills, m.dirFilter, m.query)
	m.visibleLocal = filterLocalOnly(m.localOnly, m.dirFilter, m.query, m.scope)
	if m.cursors[focusSkills] >= m.skillsListLen() {
		m.cursors[focusSkills] = max(0, m.skillsListLen()-1)
	}
	m.clampAgentsCursor()
	m.mainScroll = 0
}

// rebuildLocalOnly recomputes outside entries and their virtual tree
// node. Needs both skillsLoadedMsg and installedScannedMsg to have arrived.
func (m *model) rebuildLocalOnly() {
	if m.loading || m.loadErr != "" || m.targets == nil {
		return
	}
	sourceKeys := make(map[string]bool, len(m.skills))
	for _, s := range m.skills {
		sourceKeys[m.skillKey(s)] = true
	}
	m.localOnly = buildLocalOnly(m.targets, m.skills, sourceKeys, m.copyFromConfiguredSource)
	keys := make(map[string]bool, len(m.localOnly))
	for _, lo := range m.localOnly {
		keys[lo.Key] = true
	}
	for key := range m.localMarked {
		if !keys[key] {
			delete(m.localMarked, key)
		}
	}
	m.dirs = buildDirEntries(m.skills)
	if len(m.localOnly) > 0 {
		m.dirs = append(m.dirs, dirEntry{Path: localOnlyDir, Depth: 0})
	}
	if m.cursors[focusTree] >= len(m.dirs) {
		m.cursors[focusTree] = max(0, len(m.dirs)-1)
	}
	if m.dirFilter == localOnlyDir && len(m.localOnly) == 0 {
		m.dirFilter = ""
	}
	m.refreshVisible()
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		m.cancelled = true
		return m, tea.Quit
	}
	if m.loading || m.loadErr != "" {
		switch key {
		case "q", "esc", "ctrl+[":
			m.cancelled = true
			return m, tea.Quit
		}
		return m, nil
	}
	if m.result != nil {
		return m.handleResultKey(key)
	}
	if m.confirmMode {
		return m.handleConfirmKey(key)
	}
	if m.addPick != nil {
		return m.handleAddPickKey(msg)
	}
	if m.searchMode {
		return m.handleSearchKey(msg)
	}

	switch key {
	case "0", "1", "2", "3", "4":
		m.setFocus(focusArea(int(key[0] - '0')))
		return m, m.ensurePreviewCmd()
	case "l", "right":
		m.setFocus((m.focus + 1) % focusAreaCount)
		return m, m.ensurePreviewCmd()
	case "h", "left":
		m.setFocus((m.focus + focusAreaCount - 1) % focusAreaCount)
		return m, m.ensurePreviewCmd()
	case "j", "down":
		return m.moveCursor(1)
	case "k", "up":
		return m.moveCursor(-1)
	case "g", "home":
		return m.moveCursor(-1 << 20)
	case "G", "end":
		return m.moveCursor(1 << 20)
	case "ctrl+d", "pagedown", "ctrl+f":
		m.mainScroll += max(1, (m.height-4)/2)
		return m, nil
	case "ctrl+u", "pageup", "ctrl+b":
		m.mainScroll = max(0, m.mainScroll-max(1, (m.height-4)/2))
		return m, nil
	case " ":
		m.toggleCurrent()
		return m, nil
	case "enter":
		return m.handleEnter()
	case "i":
		return m.openInstallPlan()
	case "u":
		if m.scope == "user" {
			m.scope = "project"
		} else {
			m.scope = "user"
		}
		m.status = "scope: " + m.scope
		// the outside section is scoped, so the visible set changes
		m.refreshVisible()
		return m, m.ensurePreviewCmd()
	case "f":
		if m.focus == focusAgents {
			m.toggleForceMark()
			return m, nil
		}
		m.force = !m.force
		if m.force {
			m.status = "force: on (up-to-date copies get reinstalled too)"
		} else {
			m.status = "force: off"
		}
		return m, nil
	case "r":
		m.scanning = true
		m.status = "rescanning installed skills"
		return m, scanInstalledCmd(m.installTargets, m.projectRoot, m.allowed, m.localRoots)
	case "p":
		if m.running {
			m.status = "an operation is already running"
			return m, nil
		}
		return m.openPRPlan()
	case "P":
		if m.running {
			m.status = "an operation is already running"
			return m, nil
		}
		if lo, ok := m.currentLocalOnly(); ok {
			cp, errMsg := m.resolveOutsideCopy(lo)
			if errMsg != "" {
				m.status = errMsg
				return m, nil
			}
			m.addPick = newAddPick(lo, cp, m.skills)
			m.mainScroll = 0
			m.status = "choose destination directory in the source"
			return m, nil
		}
		return m.openPRPlan()
	case "d":
		return m.openDeletePlan()
	case "s", "/":
		m.searchMode = true
		m.setFocus(focusSkills)
		m.status = "search mode"
		return m, nil
	case "q", "esc", "ctrl+[":
		if m.focus == focusMain {
			m.focus = m.detail
			return m, nil
		}
		if m.query != "" {
			m.query = ""
			m.status = "search cleared"
			m.refreshVisible()
			return m, m.ensurePreviewCmd()
		}
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleResultKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter", "y":
		m.result = nil
		m.focus = m.detail
		m.mainScroll = 0
		m.status = "result closed"
	case "esc", "q", "ctrl+[":
		m.cancelled = true
		return m, tea.Quit
	case "j", "down":
		m.mainScroll++
	case "k", "up":
		m.mainScroll = max(0, m.mainScroll-1)
	}
	return m, nil
}

// setFocus keeps m.detail pointing at the last focused left panel so the
// main panel keeps showing its detail while focused for scrolling.
func (m *model) setFocus(area focusArea) {
	m.focus = area
	if area != focusMain {
		m.detail = area
		m.mainScroll = 0
	}
}

func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+[":
		m.query = ""
		m.searchMode = false
		m.status = "search cleared"
		m.refreshVisible()
		return m, m.ensurePreviewCmd()
	case "enter":
		m.searchMode = false
		m.status = fmt.Sprintf("%d skills match", m.skillsListLen())
		return m, m.ensurePreviewCmd()
	case " ":
		// Search is a transient picker: space commits the current match as a
		// normal neutral selection, then clears the filter so the remaining
		// skills become visible again. This avoids treating the selection key as
		// a literal space in the query.
		m.searchMode = false
		m.toggleCurrent()
		m.query = ""
		m.refreshVisible()
		m.status = "search selection committed"
		return m, m.ensurePreviewCmd()
	case "backspace", "ctrl+h":
		if m.query != "" {
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m.refreshVisible()
		}
		return m, m.ensurePreviewCmd()
	case "ctrl+u":
		m.query = ""
		m.refreshVisible()
		return m, m.ensurePreviewCmd()
	}
	if msg.Type == tea.KeyRunes {
		m.query += string(msg.Runes)
		m.refreshVisible()
		return m, m.ensurePreviewCmd()
	}
	return m, nil
}

// handleAddPickKey drives the destination picker for importing an outside
// skill to the source: stage 0 chooses the parent dir (list or direct
// input), stage 1 edits the skill name, enter builds the add-PR plan.
func (m model) handleAddPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ap := m.addPick
	key := msg.String()
	if ap.Stage == 0 && !ap.Direct {
		last := len(ap.Dirs) // index of the "(direct input…)" row
		switch key {
		case "esc", "q", "ctrl+[":
			m.addPick = nil
			m.status = "add cancelled"
		case "j", "down":
			ap.Cursor = min(last, ap.Cursor+1)
		case "k", "up":
			ap.Cursor = max(0, ap.Cursor-1)
		case "g", "home":
			ap.Cursor = 0
		case "G", "end":
			ap.Cursor = last
		case "enter":
			if ap.Cursor == last {
				ap.Direct = true
				ap.DirInput = ""
				return m, nil
			}
			ap.Dir = ap.Dirs[ap.Cursor]
			ap.Stage = 1
			ap.NameInput = path.Base(ap.Local.Name)
		}
		return m, nil
	}
	// text stages: stage-0 direct dir input, stage-1 name edit
	input := &ap.NameInput
	if ap.Stage == 0 {
		input = &ap.DirInput
	}
	switch key {
	case "esc", "ctrl+[":
		if ap.Stage == 0 {
			ap.Direct = false
		} else {
			ap.Stage = 0
		}
		return m, nil
	case "backspace", "ctrl+h":
		if *input != "" {
			runes := []rune(*input)
			*input = string(runes[:len(runes)-1])
		}
		return m, nil
	case "ctrl+u":
		*input = ""
		return m, nil
	case "enter":
		if ap.Stage == 0 {
			dir, err := cleanDestDirInput(ap.DirInput)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			ap.Dir = dir
			ap.Stage = 1
			ap.NameInput = path.Base(ap.Local.Name)
			return m, nil
		}
		name := strings.TrimSpace(ap.NameInput)
		if !validSkillName(name) {
			m.status = "invalid skill name (letters, digits, . _ - only)"
			return m, nil
		}
		destDir := path.Join(ap.Dir, name)
		if m.sourceHasSkillDir(destDir) {
			m.status = "already exists in source: " + destDir
			return m, nil
		}
		plan, errMsg := m.buildAddPRPlan(ap.Copy, destDir)
		if plan == nil {
			m.status = errMsg
			return m, nil
		}
		m.prPlan = plan
		m.addPick = nil
		m.beginPlan(opPR, nil, nil)
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		*input += string(msg.Runes)
	}
	return m, nil
}

func (m model) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter", "y":
		if len(m.planBlocks) > 0 {
			m.status = fmt.Sprintf("%s plan blocked by %d selected target(s)", strings.ToLower(m.planKind.String()), len(m.planBlocks))
			return m, nil
		}
		// Every operation uses the same plan -> terminal -> result flow.
		if m.prPlan != nil {
			plan := *m.prPlan
			m.prPlan = nil
			m.confirmMode = false
			if m.cfg.DryRun {
				m.resetPlan()
				m.result = &executionResult{Kind: opPR, Summary: "dry-run", Skipped: []string{"would create branch " + plan.Branch}}
				m.focus = focusMain
				return m, nil
			}
			m.running = true
			m.status = "creating PR: " + plan.Branch
			return m, runPRCmd(plan)
		}
		kind := m.planKind
		installs := append([]planEntry(nil), m.plan...)
		deletes := append([]deleteEntry(nil), m.deletePlan...)
		skipped := append([]string(nil), m.planSkipped...)
		if m.cfg.DryRun {
			var notes []string
			for _, entry := range installs {
				notes = append(notes, "would "+firstNonEmpty(entry.Action, "install")+" "+entry.Skill+" → "+entry.Agent)
			}
			for _, entry := range deletes {
				notes = append(notes, "would delete "+entry.Dir)
			}
			m.resetPlan()
			m.result = &executionResult{Kind: kind, Summary: "dry-run", Skipped: notes}
			m.focus = focusMain
			return m, nil
		}
		m.confirmMode = false
		m.running = true
		m.status = "running " + strings.ToLower(kind.String())
		return m, runOperationCmd(kind, installs, deletes, skipped)
	case "esc", "q", "ctrl+[", "n":
		kind := m.planKind
		m.resetPlan()
		m.status = strings.ToLower(kind.String()) + " plan cancelled"
	case "j", "down":
		m.mainScroll++
	case "k", "up":
		m.mainScroll = max(0, m.mainScroll-1)
	}
	return m, nil
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	// On normal screens Enter has one meaning: open the focused panel's
	// detail in panel 4. It executes only while a validated plan is visible.
	if m.focus != focusMain {
		m.detail = m.focus
		m.focus = focusMain
		m.mainScroll = 0
		return m, m.ensurePreviewCmd()
	}
	return m, nil
}

func (m model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if m.focus == focusMain {
		switch {
		case delta <= -(1 << 20):
			m.mainScroll = 0
		case delta >= 1<<20:
			m.mainScroll = 1 << 30 // clamped to content length at render time
		default:
			m.mainScroll = max(0, m.mainScroll+delta)
		}
		return m, nil
	}
	n := m.listLen(m.focus)
	if n == 0 {
		return m, nil
	}
	c := m.cursors[m.focus] + delta
	c = max(0, min(n-1, c))
	if c == m.cursors[m.focus] {
		return m, nil
	}
	m.cursors[m.focus] = c
	m.mainScroll = 0
	if m.focus == focusTree {
		m.dirFilter = m.dirs[c].Path
		m.cursors[focusSkills] = 0
		m.refreshVisible()
	}
	if m.focus == focusSkills {
		// panel 2 flips between agents and an outside row's copies
		m.clampAgentsCursor()
	}
	return m, m.ensurePreviewCmd()
}

func (m model) listLen(area focusArea) int {
	switch area {
	case focusTree:
		return len(m.dirs)
	case focusSkills:
		return m.skillsListLen()
	case focusAgents:
		return len(m.installTargets)
	case focusStatus:
		return len(m.targets)
	}
	return 0
}

func (m *model) toggleCurrent() {
	switch m.focus {
	case focusTree:
		if len(m.dirs) == 0 {
			return
		}
		dir := m.dirs[m.cursors[focusTree]].Path
		if dir == localOnlyDir {
			// Select/unselect every visible outside entry. Selection is
			// neutral; i/d will block it and p will resolve it as an import.
			allMarked := len(m.visibleLocal) > 0
			for _, idx := range m.visibleLocal {
				if !m.localMarked[m.localOnly[idx].Key] {
					allMarked = false
				}
			}
			for _, idx := range m.visibleLocal {
				if allMarked {
					delete(m.localMarked, m.localOnly[idx].Key)
				} else {
					m.localMarked[m.localOnly[idx].Key] = true
				}
			}
			if allMarked {
				m.status = "unselected outside source"
			} else {
				m.status = fmt.Sprintf("selected %d outside skill(s) — p proposes an import PR", len(m.visibleLocal))
			}
			return
		}
		var under []string
		var outsideKeys []string
		allMarked := true
		for _, s := range m.skills {
			if underDir(s.Path, dir) {
				under = append(under, s.Path)
				if !m.selected[s.Path] {
					allMarked = false
				}
			}
		}
		if dir == "" {
			for _, lo := range m.localOnly {
				if len(lo.copiesFor(m.scope)) == 0 {
					continue
				}
				outsideKeys = append(outsideKeys, lo.Key)
				if !m.localMarked[lo.Key] {
					allMarked = false
				}
			}
		}
		label := dir + "/"
		if dir == "" {
			label = "(all)"
		}
		if allMarked && len(under)+len(outsideKeys) > 0 {
			for _, p := range under {
				delete(m.selected, p)
			}
			for _, key := range outsideKeys {
				delete(m.localMarked, key)
			}
			m.status = "unmarked " + label
		} else {
			for _, p := range under {
				m.selected[p] = true
			}
			for _, key := range outsideKeys {
				m.localMarked[key] = true
			}
			m.status = "marked " + label
		}
	case focusSkills:
		if lo, ok := m.currentLocalOnly(); ok {
			if m.localMarked[lo.Key] {
				delete(m.localMarked, lo.Key)
				m.status = "unselected " + lo.Name
			} else {
				m.localMarked[lo.Key] = true
				m.status = "selected " + lo.Name + " — p proposes selected outside skills"
			}
			return
		}
		s, ok := m.currentSkill()
		if !ok {
			return
		}
		if m.selected[s.Path] {
			delete(m.selected, s.Path)
			m.status = "unmarked " + s.Dir()
		} else {
			m.selected[s.Path] = true
			m.status = "marked " + s.Dir()
		}
	case focusAgents:
		if _, ok := m.currentLocalOnly(); ok {
			m.status = "outside rows show copy locations; p proposes the selected copy"
			return
		}
		if m.cliOverride {
			m.status = "agents fixed by --agent/--dir flags"
			return
		}
		p := m.installTargets[m.cursors[focusAgents]]
		if m.agentSelected[p.Name] {
			delete(m.agentSelected, p.Name)
			delete(m.agentForce, p.Name)
			m.status = "unselected agent " + p.Name
		} else {
			m.agentSelected[p.Name] = true
			m.status = "selected agent " + p.Name
		}
	}
}

func (m model) currentSkill() (skill, bool) {
	if len(m.visibleSkills) == 0 {
		return skill{}, false
	}
	c := m.cursors[focusSkills]
	if c < 0 || c >= len(m.visibleSkills) {
		return skill{}, false
	}
	return m.skills[m.visibleSkills[c]], true
}

// skillsListLen is the panel-1 row count: source skills plus outside entries.
func (m model) skillsListLen() int {
	return len(m.visibleSkills) + len(m.visibleLocal)
}

// currentLocalOnly returns the outside entry under the panel-1 cursor.
func (m model) currentLocalOnly() (localOnlySkill, bool) {
	c := m.cursors[focusSkills] - len(m.visibleSkills)
	if c < 0 || c >= len(m.visibleLocal) {
		return localOnlySkill{}, false
	}
	return m.localOnly[m.visibleLocal[c]], true
}

// resolveOutsideCopy picks the PR-source copy of an outside entry. A single
// physical copy is unambiguous even when several agents share it. If
// multiple copies exist, the panel-2 cursor must point at one of them; never
// silently fall back to an unrelated agent copy.
func (m model) resolveOutsideCopy(lo localOnlySkill) (localCopy, string) {
	copies := lo.copiesFor(m.scope)
	if len(copies) == 0 {
		return localCopy{}, "no copy of " + lo.Name + " in " + m.scope + " scope"
	}
	if len(m.installTargets) > 0 {
		p := m.installTargets[max(0, min(m.cursors[focusAgents], len(m.installTargets)-1))]
		for _, cp := range copies {
			if containsStr(cp.Agents, p.Short) {
				return cp, ""
			}
		}
	}
	if len(copies) == 1 {
		return copies[0], ""
	}
	return localCopy{}, lo.Name + ": multiple copies; choose an agent holding the desired copy in panel 2"
}

// currentLocalCopy is the PR-source copy for the outside row under the
// panel-1 cursor.
func (m model) currentLocalCopy() (localOnlySkill, localCopy, bool) {
	lo, ok := m.currentLocalOnly()
	if !ok {
		return localOnlySkill{}, localCopy{}, false
	}
	cp, errMsg := m.resolveOutsideCopy(lo)
	if errMsg != "" {
		return localOnlySkill{}, localCopy{}, false
	}
	return lo, cp, true
}

func (m *model) clampAgentsCursor() {
	m.cursors[focusAgents] = max(0, min(m.cursors[focusAgents], m.listLen(focusAgents)-1))
}

func (m model) selectedPaths() []string {
	var paths []string
	for _, s := range m.skills {
		if m.selected[s.Path] {
			paths = append(paths, s.Path)
		}
	}
	return paths
}

func (m model) selectedSkills() []skill {
	var out []skill
	for _, s := range m.skills {
		if m.selected[s.Path] {
			out = append(out, s)
		}
	}
	return out
}

func (m model) selectedOutside() []localOnlySkill {
	var out []localOnlySkill
	for _, lo := range m.localOnly {
		if m.localMarked[lo.Key] && len(lo.copiesFor(m.scope)) > 0 {
			out = append(out, lo)
		}
	}
	return out
}

func (m *model) resetPlan() {
	m.planKind = opNone
	m.planBlocks = nil
	m.planWarns = nil
	m.plan = nil
	m.planSkipped = nil
	m.deletePlan = nil
	m.prPlan = nil
	m.confirmMode = false
}

func (m *model) beginPlan(kind operationKind, blocks, warns []string) {
	m.planKind = kind
	m.planBlocks = blocks
	m.planWarns = warns
	m.confirmMode = true
	m.mainScroll = 0
}

func appendForceArg(args []string) []string {
	for _, arg := range args {
		if arg == "--force" {
			return args
		}
	}
	return append(args, "--force")
}

func (m model) agentNamed(name string) (installTarget, bool) {
	for _, p := range m.installTargets {
		if p.Name == name || p.GHAgent == name {
			return p, true
		}
	}
	return installTarget{}, false
}

// destinationForced treats agent aliases sharing one physical directory as
// one target. An override set on kimi must still apply when codex is the
// canonical target chosen to execute their shared .agents/skills destination.
func (m model) destinationForced(p installTarget) bool {
	if m.force {
		return true
	}
	dest := p.dirFor(m.scope, m.projectRoot)
	for _, candidate := range m.installTargets {
		if candidate.dirFor(m.scope, m.projectRoot) == dest && m.agentSelected[candidate.Name] && m.agentForce[candidate.Name] {
			return true
		}
	}
	return false
}

// openInstallPlan resolves the neutral selection into install/update/adopt
// work. Outside selections are blockers; they are never silently skipped.
func (m model) openInstallPlan() (tea.Model, tea.Cmd) {
	m.resetPlan()
	selected := m.selectedSkills()
	outside := m.selectedOutside()
	if len(selected) == 0 && len(outside) == 0 {
		m.status = "nothing selected — space selects skills"
		return m, nil
	}
	if m.cfg.SelectOnly {
		m.accepted = true
		return m, tea.Quit
	}
	var blocks, warns []string
	for _, lo := range outside {
		blocks = append(blocks, lo.Name+": outside source; use p to import, or select its source counterpart to ADOPT")
	}
	if !m.cliOverride && len(m.agentSelected) == 0 {
		blocks = append(blocks, "no agents selected")
	}
	paths := make([]string, 0, len(selected))
	blockedDest := make(map[string]bool)
	for _, s := range selected {
		paths = append(paths, s.Path)
		for _, p := range m.installTargets {
			if !m.agentSelected[p.Name] && !m.agentForce[p.Name] {
				continue
			}
			blockKey := s.Path + "|" + p.dirFor(m.scope, m.projectRoot)
			if m.badgeState(s, p) == badgeModified && !m.destinationForced(p) && !blockedDest[blockKey] {
				blockedDest[blockKey] = true
				blocks = append(blocks, fmt.Sprintf("%s @%s: edited copy requires force after reviewing the diff", s.Dir(), p.Name))
			}
		}
	}
	gh, _ := commandPath("gh")
	skillByPath := make(map[string]skill, len(m.skills))
	for _, s := range m.skills {
		skillByPath[s.Path] = s
	}
	// Skip files an agent destination already has in current form.
	needed := func(path string, p installTarget) bool {
		s, ok := skillByPath[path]
		if !ok {
			return true
		}
		return m.badgeState(s, p) != badgeManaged
	}
	entries, skipped := buildPlan(m.cfg, gh, paths, m.installTargets, m.agentSelected, m.scope, m.force, m.projectRoot, m.sourceRoot, m.treeShas, needed, m.agentForce)
	for i := range entries {
		entry := &entries[i]
		entry.Action = "install"
		s, ok := skillByPath[entry.Skill]
		p, hasAgent := m.agentNamed(entry.Agent)
		if !ok || !hasAgent {
			continue
		}
		switch m.badgeState(s, p) {
		case badgeManaged:
			entry.Action = "reinstall"
		case badgeOutdated:
			entry.Action = "update"
			entry.Args = appendForceArg(entry.Args)
		case badgeModified:
			entry.Action = "overwrite"
			if m.destinationForced(p) {
				warns = append(warns, fmt.Sprintf("%s @%s: local edits will be overwritten", s.Dir(), p.Name))
			}
		case badgeForeign, badgeUntracked:
			entry.Action = "adopt"
			entry.Args = appendForceArg(entry.Args)
			warns = append(warns, fmt.Sprintf("%s @%s: outside copy will be replaced by the configured source", s.Dir(), p.Name))
		}
	}
	if len(entries) == 0 && len(blocks) == 0 {
		m.status = "no install work: selected source skills are current"
		return m, nil
	}
	m.plan = entries
	m.planSkipped = skipped
	m.beginPlan(opInstall, blocks, warns)
	return m, nil
}

// openDeletePlan resolves only managed tracked copies. Any outside or absent
// selected target blocks the entire plan so Delete never partially succeeds.
func (m model) openDeletePlan() (tea.Model, tea.Cmd) {
	m.resetPlan()
	selected := m.selectedSkills()
	outside := m.selectedOutside()
	if len(selected) == 0 && len(outside) == 0 {
		m.status = "nothing selected — space selects skills"
		return m, nil
	}
	var blocks, warns []string
	for _, lo := range outside {
		blocks = append(blocks, lo.Name+": outside source is protected from Delete")
	}
	if !m.cliOverride && len(m.agentSelected) == 0 {
		blocks = append(blocks, "no agents selected")
	}
	entries := buildDeletePlan(selected, m.targets, m.installTargets, m.agentSelected, m.scope, m.sourceRoot, m.copyFromConfiguredSource)
	warnedDest := make(map[string]bool)
	for _, s := range selected {
		found := false
		for _, e := range entries {
			if e.Skill == s.Dir() {
				found = true
				break
			}
		}
		if !found {
			blocks = append(blocks, s.Dir()+": no managed copy in the selected agents")
		}
		for _, p := range m.installTargets {
			warnKey := s.Path + "|" + p.dirFor(m.scope, m.projectRoot)
			if m.agentSelected[p.Name] && m.badgeState(s, p) == badgeModified && !warnedDest[warnKey] {
				warnedDest[warnKey] = true
				warns = append(warns, fmt.Sprintf("%s @%s: local edits will be deleted", s.Dir(), p.Name))
			}
		}
	}
	m.deletePlan = entries
	m.beginPlan(opDelete, blocks, warns)
	return m, nil
}

func (m model) openPRPlan() (tea.Model, tea.Cmd) {
	m.resetPlan()
	selected := m.selectedSkills()
	outside := m.selectedOutside()
	if len(selected) == 0 && len(outside) == 0 {
		m.status = "nothing selected — select edited or outside skills first"
		return m, nil
	}
	plan, blocks := m.buildSelectedPRPlan(selected, outside)
	m.prPlan = plan
	m.beginPlan(opPR, blocks, nil)
	return m, nil
}

func (m *model) ensurePreviewCmd() tea.Cmd {
	// Agent detail also needs the source SKILL.md for the local-edit diff.
	if area := m.detailArea(); area != focusSkills && area != focusAgents {
		return nil
	}
	s, ok := m.currentSkill()
	if !ok || s.Content != "" || s.Loading || s.LoadErr != "" {
		return nil
	}
	m.skills[m.visibleSkills[m.cursors[focusSkills]]].Loading = true
	return loadSkillContentCmd(m.cfg, m.ref, s.Path)
}

type badgeState int

const (
	badgeAbsent badgeState = iota
	badgeManaged
	badgeOutdated // installed, but the source has newer content
	badgeModified // installed, but the copy was edited locally
	badgeUntracked
	badgeForeign
)

// skillKey identifies a source skill the same way installed metadata does:
// repo-relative dir for GitHub sources, absolute dir for local clones.
func (m model) skillKey(s skill) string {
	if m.sourceLocal {
		return filepath.Join(m.sourceRoot, filepath.FromSlash(s.Dir()))
	}
	return s.Dir()
}

// copyFromConfiguredSource is stricter than the supply-chain allowlist.
// Another allowed repository is not foreign by policy, but it is still
// outside the authoritative source currently open in this TUI.
func (m model) copyFromConfiguredSource(inst installedSkill) bool {
	if inst.Class != classManaged {
		return false
	}
	if m.sourceLocal {
		if inst.LocalPath == "" || m.sourceRoot == "" {
			return false
		}
		rel, err := filepath.Rel(filepath.Clean(m.sourceRoot), filepath.Clean(inst.LocalPath))
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if inst.RepoSlug == "" {
		// Synthetic/legacy records classified as managed may omit the slug;
		// real scan results cannot reach classManaged without source metadata.
		return inst.GhPath != ""
	}
	return repoSlug(inst.RepoSlug) == repoSlug(m.cfg.Source)
}

func (m model) badgeState(s skill, p installTarget) badgeState {
	key := p.Short + "|" + m.scope
	if inst, ok := m.lookup.byKey[m.skillKey(s)][key]; ok {
		current := m.treeShas[m.skillKey(s)]
		if inst.TreeSha != "" && current != "" && inst.TreeSha != current {
			return badgeOutdated
		}
		if m.copyModified(inst) {
			return badgeModified
		}
		return badgeManaged
	}
	if cls, ok := m.lookup.byName[path.Base(s.Dir())][key]; ok {
		if cls == classForeign {
			return badgeForeign
		}
		return badgeUntracked
	}
	return badgeAbsent
}

// sourceFilesFor returns the source's files under a skill key as
// rel path -> blob sha.
func (m model) sourceFilesFor(key string) map[string]string {
	src := make(map[string]string)
	prefix := key + "/"
	for p, sha := range m.blobShas {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rel := strings.TrimPrefix(p, prefix)
		if junkRel(rel) {
			continue
		}
		src[rel] = sha
	}
	return src
}

// sourceHasSkillDir reports whether destDir collides with the source tree:
// an existing skill dir, a dir nested in/around one, or any tracked file.
func (m model) sourceHasSkillDir(destDir string) bool {
	if destDir == "" {
		return true
	}
	for _, s := range m.skills {
		d := s.Dir()
		if d == destDir || strings.HasPrefix(d, destDir+"/") || strings.HasPrefix(destDir, d+"/") {
			return true
		}
	}
	// blobShas keys are repo-relative for remote sources, absolute for local
	exact := destDir
	if m.sourceLocal {
		exact = filepath.Join(m.sourceRoot, filepath.FromSlash(destDir))
	}
	for p := range m.blobShas {
		if p == exact || strings.HasPrefix(p, exact+"/") {
			return true
		}
	}
	return false
}

// sourceContentFor returns the source SKILL.md content for a skill key, if
// it has been loaded (always true for local sources, lazy for remote).
func (m model) sourceContentFor(key string) string {
	for _, s := range m.skills {
		if m.skillKey(s) == key {
			return s.Content
		}
	}
	return ""
}

// copyModified reports whether an installed copy differs from the current
// source. Companion files are compared by git blob sha; the root SKILL.md is
// compared semantically because gh re-serializes its frontmatter on install.
// Only meaningful when the copy is not outdated: outdated copies are
// expected to differ.
func (m model) copyModified(inst installedSkill) bool {
	if len(m.blobShas) == 0 || inst.Key() == "" || len(inst.FileShas) == 0 {
		return false
	}
	src := m.sourceFilesFor(inst.Key())
	if len(src) == 0 {
		return false
	}
	for rel, sha := range src {
		got, ok := inst.FileShas[rel]
		if !ok {
			return true
		}
		if rel == "SKILL.md" {
			continue // compared semantically below
		}
		if got != sha {
			return true
		}
	}
	for rel := range inst.FileShas {
		if _, ok := src[rel]; !ok {
			return true
		}
	}
	if srcContent := m.sourceContentFor(inst.Key()); srcContent != "" && inst.SkillMD != "" {
		return !skillMDEquivalent(srcContent, inst.SkillMD)
	}
	return false
}

// copyChanges lists per-file differences of an installed copy vs the source.
func (m model) copyChanges(inst installedSkill) []string {
	src := m.sourceFilesFor(inst.Key())
	var out []string
	var rels []string
	for rel := range inst.FileShas {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		srcSha, inSrc := src[rel]
		switch {
		case !inSrc:
			out = append(out, "A "+rel)
		case rel == "SKILL.md":
			if srcContent := m.sourceContentFor(inst.Key()); srcContent != "" && inst.SkillMD != "" &&
				!skillMDEquivalent(srcContent, inst.SkillMD) {
				out = append(out, "M "+rel)
			}
		case srcSha != inst.FileShas[rel]:
			out = append(out, "M "+rel)
		}
	}
	rels = rels[:0]
	for rel := range src {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if _, ok := inst.FileShas[rel]; !ok {
			out = append(out, "D "+rel)
		}
	}
	return out
}

// skillIndicator reports health independently from coverage. Partial install
// state is rendered as an explicit N/M count rather than another symbol.
func (m model) skillIndicator(s skill) string {
	installed, outdated, modified := 0, 0, 0
	outside := false
	for _, p := range m.installTargets {
		if !m.agentSelected[p.Name] {
			continue
		}
		switch m.badgeState(s, p) {
		case badgeManaged:
			installed++
		case badgeOutdated:
			installed++
			outdated++
		case badgeModified:
			installed++
			modified++
		case badgeForeign:
			outside = true
		case badgeUntracked:
			outside = true
		}
	}
	switch {
	case modified > 0:
		return "m"
	case outdated > 0:
		return "↓"
	case outside:
		return "O"
	case installed > 0:
		return "✓"
	}
	return " "
}

func (m model) skillCoverage(s skill) (installed, total int) {
	seen := make(map[string]bool)
	for _, p := range m.installTargets {
		if !m.agentSelected[p.Name] {
			continue
		}
		dest := p.dirFor(m.scope, m.projectRoot)
		if seen[dest] {
			continue
		}
		seen[dest] = true
		total++
		switch m.badgeState(s, p) {
		case badgeManaged, badgeOutdated, badgeModified:
			installed++
		}
	}
	return installed, total
}

// contextSkills is the set used for agent action previews: explicitly
// selected source skills, or the cursor skill when nothing is selected.
func (m model) contextSkills() []skill {
	var out []skill
	for _, s := range m.skills {
		if m.selected[s.Path] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		if s, ok := m.currentSkill(); ok {
			out = append(out, s)
		}
	}
	return out
}

// --- rendering ---

func (m model) View() string {
	if m.loading {
		return simpleView(m.width, m.height, []string{
			headerStyle.Render("gh-skill-tui"),
			"",
			m.status,
		})
	}
	if m.loadErr != "" {
		return simpleView(m.width, m.height, []string{
			headerStyle.Render("gh-skill-tui"),
			"",
			errorStyle.Render(m.loadErr),
			"",
			"q: quit",
		})
	}
	width := max(70, m.width)
	height := max(20, m.height)

	leftW := max(30, min(46, width/3))
	mainW := width - leftW
	bodyH := height - 2

	left := m.renderLeftColumn(leftW, bodyH)
	main := m.renderMainPanel(mainW, bodyH)
	rows := make([]string, bodyH)
	for i := 0; i < bodyH; i++ {
		rows[i] = left[i] + main[i]
	}

	return strings.Join([]string{
		m.headerLine(width),
		strings.Join(rows, "\n"),
		m.statusLine(width),
	}, "\n")
}

func (m model) headerLine(width int) string {
	badge := scopeBadge(m.scope)
	title := "gh-skill-tui  source: " + m.cfg.Source
	if m.ref != "" {
		title += "  ref: " + m.ref
	}
	title += "  dir: " + homeShorten(m.projectRoot)
	unapproved := ""
	if !m.approved {
		unapproved = " " + errorStyle.Render("[UNAPPROVED SOURCE]")
	}
	avail := width - lipgloss.Width(badge) - lipgloss.Width(unapproved) - 1
	left := headerStyle.Render(truncate(title, max(1, avail))) + unapproved
	gap := width - lipgloss.Width(left) - lipgloss.Width(badge)
	if gap < 1 {
		return padRendered(left, width)
	}
	return left + strings.Repeat(" ", gap) + badge
}

func (m model) statusLine(width int) string {
	if m.searchMode {
		return padRendered(searchStyle.Render(fitText("search: "+m.query+"_  (space: select+clear, enter: keep, esc: clear)", width)), width)
	}
	if m.result != nil {
		return padRendered(searchStyle.Render(fitText("enter: return to TUI  esc/q: exit", width)), width)
	}
	if m.confirmMode {
		msg := m.planKind.String() + " plan"
		if len(m.planBlocks) > 0 {
			msg += fmt.Sprintf(" BLOCKED (%d) — enter disabled  esc: back", len(m.planBlocks))
		} else {
			msg += " — enter/y: run  esc: cancel"
		}
		return padRendered(searchStyle.Render(fitText(msg, width)), width)
	}
	if m.addPick != nil {
		ap := m.addPick
		var msg string
		switch {
		case ap.Stage == 0 && !ap.Direct:
			msg = "add to source: j/k choose dir, enter: next, esc: cancel"
		case ap.Stage == 0:
			msg = "dir: " + ap.DirInput + "_  (enter: next, esc: back to list)"
		default:
			msg = "name: " + ap.NameInput + "_  (enter: build PR, esc: back)"
		}
		return padRendered(searchStyle.Render(fitText(msg, width)), width)
	}
	state := fmt.Sprintf("scope:%s force:%s  selected: source %d / outside %d", m.scope, onOff(m.force), len(m.selectedPaths()), len(m.selectedOutside()))
	if m.query != "" {
		state += "  search:" + m.query
	}
	if m.status != "" {
		state += "  " + m.status
	}
	hints := "0-4/h/l panel  j/k move  g/G top/bottom  ctrl-f/b page  ctrl-d/u half-page  space select  i install  p PR  d delete  s search  ctrl-[ esc  q quit"
	return padRendered(mutedStyle.Render(fitText(state+"  |  "+hints, width)), width)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m model) renderLeftColumn(w, h int) []string {
	// 4 title rows + 1 shared bottom border
	contentTotal := h - 5
	agentH := max(2, min(len(m.installTargets), 8))
	statH := max(2, min(max(len(m.targets), 1), 6))
	remaining := contentTotal - agentH - statH
	for remaining < 6 && (agentH > 2 || statH > 2) {
		if agentH > 2 {
			agentH--
		} else {
			statH--
		}
		remaining = contentTotal - agentH - statH
	}
	treeH := max(3, remaining*2/5)
	skillsH := max(3, remaining-treeH)

	cw := w - 2
	var lines []string
	lines = append(lines, m.boxify(focusTree, m.treePanelTitle(), w, treeH, m.treeRows(cw))...)
	lines = append(lines, m.boxify(focusSkills, m.skillsPanelTitle(), w, skillsH, m.skillRows(cw))...)
	lines = append(lines, m.boxify(focusAgents, m.agentsPanelTitle(), w, agentH, m.agentRows(cw))...)
	lines = append(lines, m.boxify(focusStatus, m.statusPanelTitle(), w, statH, m.statusRows(cw))...)
	lines = append(lines, boxBottom(w, false))
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return lines[:h]
}

func (m model) treePanelTitle() string {
	return "0 tree"
}

func (m model) skillsPanelTitle() string {
	if len(m.visibleLocal) > 0 {
		return fmt.Sprintf("1 skills (%d+%d outside) @%s", len(m.visibleSkills), len(m.visibleLocal), m.scope)
	}
	return fmt.Sprintf("1 skills (%d) @%s", len(m.visibleSkills), m.scope)
}

func (m model) agentsPanelTitle() string {
	if lo, ok := m.currentLocalOnly(); ok {
		return "2 copies → " + lo.Name + " (outside)"
	}
	if m.cliOverride {
		return "2 agents (cli)"
	}
	ctx := m.contextSkills()
	switch len(ctx) {
	case 0:
		return "2 agents"
	case 1:
		return "2 agents → " + path.Base(ctx[0].Dir())
	default:
		return fmt.Sprintf("2 agents → %d skills", len(ctx))
	}
}

func (m model) statusPanelTitle() string {
	if m.scanning {
		return "3 installed (scanning)"
	}
	return "3 installed"
}

// prow is a list row with an optional colored variant; plain is used for the
// cursor highlight and as a fallback when the styled variant would overflow.
type prow struct {
	plain  string
	styled string
}

func (m model) treeRows(cw int) []prow {
	rows := make([]prow, 0, len(m.dirs))
	for _, d := range m.dirs {
		if d.Path == localOnlyDir {
			count := 0
			marked := 0
			for _, lo := range m.localOnly {
				if len(lo.copiesFor(m.scope)) > 0 {
					count++
					if m.localMarked[lo.Key] {
						marked++
					}
				}
			}
			mark := " "
			switch {
			case count > 0 && marked == count:
				mark = "x"
			case marked > 0:
				mark = "~"
			}
			suffix := fmt.Sprintf(" (%d)", count)
			label := truncate(d.Label(), max(1, cw-4-len(suffix)))
			rows = append(rows, prow{
				plain:  fmt.Sprintf("[%s] %s%s", mark, label, suffix),
				styled: "[" + styledMark(mark) + "] " + warnStyle.Render(label) + mutedStyle.Render(suffix),
			})
			continue
		}
		count := 0
		marked := 0
		for _, s := range m.skills {
			if underDir(s.Path, d.Path) {
				count++
				if m.selected[s.Path] {
					marked++
				}
			}
		}
		if d.Path == "" {
			for _, lo := range m.localOnly {
				if len(lo.copiesFor(m.scope)) == 0 {
					continue
				}
				count++
				if m.localMarked[lo.Key] {
					marked++
				}
			}
		}
		mark := " "
		switch {
		case count > 0 && marked == count:
			mark = "x"
		case marked > 0:
			mark = "~"
		}
		indent := strings.Repeat("  ", d.Depth)
		suffix := fmt.Sprintf(" (%d)", count)
		label := truncate(d.Label(), max(1, cw-4-len(indent)-len(suffix)))
		rows = append(rows, prow{
			plain:  fmt.Sprintf("[%s] %s%s%s", mark, indent, label, suffix),
			styled: "[" + styledMark(mark) + "] " + indent + dirStyle.Render(label) + mutedStyle.Render(suffix),
		})
	}
	return rows
}

func (m model) skillRows(cw int) []prow {
	rows := make([]prow, 0, m.skillsListLen())
	for _, idx := range m.visibleSkills {
		s := m.skills[idx]
		mark := " "
		if m.selected[s.Path] {
			mark = "x"
		}
		label := s.Dir()
		if m.dirFilter != "" {
			label = strings.TrimPrefix(label, m.dirFilter+"/")
		}
		ind := m.skillIndicator(s)
		coverage := ""
		if installed, total := m.skillCoverage(s); total > 0 {
			coverage = fmt.Sprintf(" %d/%d", installed, total)
		}
		label = truncate(label, max(1, cw-5-len(coverage)))
		rows = append(rows, prow{
			plain:  fmt.Sprintf("[%s]%s %s%s", mark, ind, label, coverage),
			styled: "[" + selectionMarkStyled(mark) + "]" + styledMark(ind) + " " + label + mutedStyle.Render(coverage),
		})
	}
	for _, idx := range m.visibleLocal {
		lo := m.localOnly[idx]
		suffix := ""
		if n := len(lo.copiesFor(m.scope)); n > 1 {
			suffix = fmt.Sprintf(" ×%d", n)
		}
		mark := " "
		if m.localMarked[lo.Key] {
			mark = "x"
		}
		label := truncate(lo.Name, max(1, cw-5-len(suffix)))
		rows = append(rows, prow{
			plain:  "[" + mark + "]O " + label + suffix,
			styled: "[" + selectionMarkStyled(mark) + "]" + warnStyle.Render("O") + " " + label + mutedStyle.Render(suffix),
		})
	}
	return rows
}

// agentAction summarizes what an Install/Update plan would do for one agent.
// installed is the number of context skills tracked at its destination.
func (m model) agentAction(p installTarget, ctx []skill) (action string, actionStyle lipgloss.Style, state string, installed int) {
	outdated, modified := 0, 0
	outside := false
	for _, s := range ctx {
		switch m.badgeState(s, p) {
		case badgeManaged:
			installed++
		case badgeOutdated:
			installed++
			outdated++
		case badgeModified:
			installed++
			modified++
		case badgeForeign:
			outside = true
		case badgeUntracked:
			outside = true
		}
	}
	switch {
	case modified > 0:
		state = "m"
	case outdated > 0:
		state = "↓"
	case outside:
		state = "O"
	case installed > 0:
		state = "✓"
	default:
		state = " "
	}
	if !m.agentSelected[p.Name] || len(ctx) == 0 {
		return "", actionStyle, state, installed
	}
	// Count only work an Install/Update plan would actually do: current copies are
	// skipped unless force reinstalls them
	force := m.force || m.agentForce[p.Name]
	newCnt := len(ctx) - installed
	updCnt := outdated + modified
	if force {
		updCnt = installed
	}
	affected := newCnt + updCnt
	if affected == 0 {
		return "", actionStyle, state, installed
	}
	switch {
	case modified > 0 && force:
		action = "overwrite"
		actionStyle = errorStyle
	case modified > 0:
		action = "needs force"
		actionStyle = errorStyle
	case newCnt > 0 && updCnt > 0:
		action = "new+upd"
		actionStyle = updateStyle
	case updCnt > 0:
		action = "update"
		actionStyle = updateStyle
		if force && outdated == 0 && modified == 0 {
			action = "reinstall"
		}
	default:
		action = "new"
		actionStyle = newStyle
	}
	if len(ctx) > 1 {
		action = fmt.Sprintf("%s %d/%d", action, affected, len(ctx))
	}
	return action, actionStyle, state, installed
}

// agentMark is selection only. Current state and planned work are rendered
// in their own columns/text; brackets never encode an operation.
func (m model) agentMark(p installTarget) string {
	if m.agentSelected[p.Name] {
		return "x"
	}
	return " "
}

// toggleForceMark enables an explicit overwrite/reinstall override for the
// cursor agent. The bracket remains selection-only; the action text and
// plan spell out the destructive operation.
func (m *model) toggleForceMark() {
	if _, ok := m.currentLocalOnly(); ok {
		m.status = "outside skill: p proposes an import PR (P picks the destination)"
		return
	}
	if m.cliOverride {
		m.status = "agents fixed by --agent/--dir flags"
		return
	}
	if len(m.installTargets) == 0 {
		return
	}
	p := m.installTargets[m.cursors[focusAgents]]
	if m.agentForce[p.Name] {
		delete(m.agentForce, p.Name)
		m.status = "force mark cleared: " + p.Name
		return
	}
	m.agentForce[p.Name] = true
	m.agentSelected[p.Name] = true
	m.status = "force override enabled for " + p.Name
}

func selectionMarkStyled(mark string) string {
	if mark == "x" {
		return markStyle.Render(mark)
	}
	return mark
}

// agentRows mirrors the skills-panel format: [selection]state name action.
// The action is only a preview of a future i plan; Delete is resolved by d
// and never encoded in the bracket.
func (m model) agentRows(cw int) []prow {
	if lo, ok := m.currentLocalOnly(); ok {
		// Outside mode is an inventory view, not agent selection. A copy
		// marker is deliberately outside brackets so it cannot look selected.
		copies := lo.copiesFor(m.scope)
		rows := make([]prow, 0, len(m.installTargets))
		for _, p := range m.installTargets {
			has := false
			for _, c := range copies {
				if containsStr(c.Agents, p.Short) {
					has = true
					break
				}
			}
			name := truncate(p.Name, max(1, cw-5))
			if has {
				rows = append(rows, prow{
					plain:  "  O " + name + " copy",
					styled: "  " + warnStyle.Render("O") + " " + name + " " + mutedStyle.Render("copy"),
				})
			} else {
				rows = append(rows, prow{plain: "    " + name, styled: "    " + mutedStyle.Render(name)})
			}
		}
		return rows
	}
	ctx := m.contextSkills()
	rows := make([]prow, 0, len(m.installTargets))
	for _, p := range m.installTargets {
		action, actionStyle, state, _ := m.agentAction(p, ctx)
		mark := m.agentMark(p)
		actionW := 0
		if action != "" {
			actionW = len(action) + 1 // leading space
		}
		name := truncate(p.Name, max(1, cw-5-actionW))
		styledName := name
		if !m.agentSelected[p.Name] {
			styledName = mutedStyle.Render(name)
		}
		styledAction := ""
		plainAction := ""
		if action != "" {
			styledName = actionStyle.Render(name)
			styledAction = " " + actionStyle.Render(action)
			plainAction = " " + action
		}
		rows = append(rows, prow{
			plain:  fmt.Sprintf("[%s]%s %s%s", mark, state, name, plainAction),
			styled: "[" + selectionMarkStyled(mark) + "]" + styledMark(state) + " " + styledName + styledAction,
		})
	}
	return rows
}

func (m model) statusRows(cw int) []prow {
	if m.scanning && len(m.targets) == 0 {
		return []prow{{plain: "scanning..."}}
	}
	rows := make([]prow, 0, len(m.targets))
	for _, t := range m.targets {
		managed, outside := 0, 0
		for _, inst := range t.Skills {
			if m.installedCopyOutside(inst) {
				outside++
			} else {
				managed++
			}
		}
		counts := fmt.Sprintf(" managed:%d outside:%d", managed, outside)
		names := truncate(strings.Join(t.Agents, "+"), max(1, cw-2-lipgloss.Width(counts)))
		styledCounts := " " + markStyle.Render(fmt.Sprintf("managed:%d", managed)) +
			" " + warnStyle.Render(fmt.Sprintf("outside:%d", outside))
		rows = append(rows, prow{
			plain:  string(t.Scope[0]) + " " + names + counts,
			styled: string(t.Scope[0]) + " " + names + styledCounts,
		})
	}
	return rows
}

func (m model) installedCopyOutside(inst installedSkill) bool {
	if !m.copyFromConfiguredSource(inst) || inst.Key() == "" {
		return true
	}
	for _, s := range m.skills {
		if m.skillKey(s) == inst.Key() {
			return false
		}
	}
	return true
}

func (m model) boxify(area focusArea, title string, w, contentH int, rows []prow) []string {
	focused := m.focus == area && !m.confirmMode && m.addPick == nil
	lines := []string{boxTop(title, w, focused)}
	cursor := m.cursors[area]
	off := listOffset(cursor, len(rows), contentH)
	cw := w - 2
	for i := 0; i < contentH; i++ {
		idx := off + i
		cell := strings.Repeat(" ", cw)
		if idx >= 0 && idx < len(rows) {
			r := rows[idx]
			switch {
			case focused && idx == cursor:
				cell = activeStyle.Render(fitText(r.plain, cw))
			case r.styled != "" && lipgloss.Width(r.styled) <= cw:
				cell = padRendered(r.styled, cw)
			default:
				cell = previewStyle.Render(fitText(r.plain, cw))
			}
		}
		lines = append(lines, boxRowCell(cell, w, focused))
	}
	return lines
}

func (m model) renderMainPanel(w, h int) []string {
	title, body := m.mainContent(w - 2)
	if !m.confirmMode && m.addPick == nil {
		title = "4 " + title
	}
	focused := m.confirmMode || m.addPick != nil || m.focus == focusMain
	lines := []string{boxTop(title, w, focused)}
	contentH := h - 2
	cw := w - 2
	scroll := min(m.mainScroll, max(0, len(body)-contentH))
	for i := 0; i < contentH; i++ {
		idx := scroll + i
		cell := strings.Repeat(" ", cw)
		if idx >= 0 && idx < len(body) {
			text := body[idx]
			if strings.Contains(text, "\x1b") {
				if lipgloss.Width(text) > cw {
					text = lipgloss.NewStyle().MaxWidth(cw).Render(text)
				}
				cell = padRendered(text, cw)
			} else {
				cell = previewStyle.Render(fitText(text, cw))
			}
		}
		lines = append(lines, boxRowCell(cell, w, focused))
	}
	lines = append(lines, boxBottom(w, focused))
	return lines
}

func (m model) detailArea() focusArea {
	if m.focus == focusMain {
		return m.detail
	}
	return m.focus
}

func (m model) mainContent(w int) (string, []string) {
	if m.result != nil {
		return m.resultContent(w)
	}
	if m.addPick != nil {
		return m.addPickContent(w)
	}
	if m.confirmMode {
		return m.confirmContent(w)
	}
	switch m.detailArea() {
	case focusTree:
		return m.treeDetail(w)
	case focusSkills:
		return m.skillDetail(w)
	case focusAgents:
		return m.agentDetail(w)
	case focusStatus:
		return m.statusDetail(w)
	}
	return "", nil
}

func (m model) resultContent(w int) (string, []string) {
	result := m.result
	lines := []string{result.Summary, ""}
	if len(result.Succeeded) > 0 {
		lines = append(lines, markStyle.Render("Succeeded:"))
		for _, item := range result.Succeeded {
			for _, line := range wrapLines("  ✓ "+item, w) {
				lines = append(lines, markStyle.Render(line))
			}
		}
	}
	if len(result.Failed) > 0 {
		lines = append(lines, "", errorStyle.Render("Failed:"))
		for _, item := range result.Failed {
			for _, line := range wrapLines("  ✗ "+item, w) {
				lines = append(lines, errorStyle.Render(line))
			}
		}
	}
	if len(result.Skipped) > 0 {
		lines = append(lines, "", mutedStyle.Render("Skipped / notes:"))
		for _, item := range result.Skipped {
			lines = append(lines, mutedStyle.Render("  "+item))
		}
	}
	lines = append(lines, "", warnStyle.Render("enter: return to TUI  esc/q: exit"))
	return strings.ToLower(result.Kind.String()) + " result", lines
}

type planBlockGroup struct {
	Reason  string
	Targets []string
}

// groupPlanBlocks turns repeated "target: reason" diagnostics into one human
// readable reason followed by its targets. Global blockers without a target
// remain their own reason-only group.
func groupPlanBlocks(blocks []string) []planBlockGroup {
	var groups []planBlockGroup
	byReason := make(map[string]int)
	for _, block := range blocks {
		target, reason, found := strings.Cut(block, ": ")
		if !found {
			reason = block
			target = ""
		}
		idx, exists := byReason[reason]
		if !exists {
			idx = len(groups)
			byReason[reason] = idx
			groups = append(groups, planBlockGroup{Reason: reason})
		}
		if target != "" {
			groups[idx].Targets = append(groups[idx].Targets, target)
		}
	}
	return groups
}

func (m model) confirmContent(w int) (string, []string) {
	title := strings.ToLower(m.planKind.String()) + " plan"
	var lines []string
	switch m.planKind {
	case opPR:
		if m.prPlan != nil {
			title, lines = m.prConfirmContent(w)
		}
	case opDelete:
		if len(m.deletePlan) > 0 {
			lines = append(lines, fmt.Sprintf("Ready — %d managed director(ies) will be removed:", len(m.deletePlan)), "")
			for _, e := range m.deletePlan {
				for _, l := range wrapLines("✗ "+homeShorten(e.Dir)+"  ("+e.Agents+")", w) {
					lines = append(lines, errorStyle.Render(l))
				}
			}
		}
	case opInstall:
		if len(m.plan) > 0 {
			lines = append(lines, fmt.Sprintf("Ready — %d install command(s):", len(m.plan)), "")
			for _, e := range m.plan {
				label := strings.ToUpper(firstNonEmpty(e.Action, "install")) + "  " + e.Skill + " → " + e.Agent
				for _, l := range wrapLines(label, w) {
					lines = append(lines, codeStyle.Render(l))
				}
				for _, l := range wrapLines("  "+shellJoin(e.Args), w) {
					lines = append(lines, mutedStyle.Render(l))
				}
			}
		}
		if len(m.planSkipped) > 0 {
			lines = append(lines, "", "No work / deduplicated:")
			for _, s := range m.planSkipped {
				lines = append(lines, mutedStyle.Render("  "+s))
			}
		}
	}
	if len(m.planWarns) > 0 {
		lines = append(lines, "", warnStyle.Render("Warnings:"))
		for _, warning := range m.planWarns {
			for _, l := range wrapLines("  ⚠ "+warning, w) {
				lines = append(lines, warnStyle.Render(l))
			}
		}
	}
	if len(m.planBlocks) > 0 {
		title += " — BLOCKED"
		lines = append(lines, "", errorStyle.Render("Blocking reasons:"))
		for _, group := range groupPlanBlocks(m.planBlocks) {
			heading := "  ✗ " + group.Reason
			if len(group.Targets) > 1 {
				heading += fmt.Sprintf(" (%d skills)", len(group.Targets))
			}
			for _, l := range wrapLines(heading, w) {
				lines = append(lines, errorStyle.Render(l))
			}
			for _, target := range group.Targets {
				for _, l := range wrapLines("      • "+target, w) {
					lines = append(lines, mutedStyle.Render(l))
				}
			}
		}
		lines = append(lines, "", errorStyle.Render("Nothing will run. Esc returns to selection."))
		return title, lines
	}
	lines = append(lines, "", warnStyle.Render("enter/y: run  esc: cancel"))
	return title, lines
}

// addPickContent renders the destination picker for importing an outside
// skill to the source repository.
func (m model) addPickContent(w int) (string, []string) {
	ap := m.addPick
	dirLabel := func(d string) string {
		if d == "" {
			return "(repo root)"
		}
		return d + "/"
	}
	lines := []string{
		"skill:  " + ap.Local.Name,
		"from:   " + homeShorten(ap.Copy.Inst.Dir) + " (" + strings.Join(ap.Copy.Agents, "+") + ", " + ap.Copy.Scope + " scope)",
		"",
	}
	if ap.Stage == 0 {
		lines = append(lines, "destination directory in "+m.cfg.Source+":", "")
		row := func(i int, label string) string {
			if !ap.Direct && i == ap.Cursor {
				return "→ " + warnStyle.Render(label)
			}
			return "  " + label
		}
		for i, d := range ap.Dirs {
			lines = append(lines, row(i, dirLabel(d)))
		}
		lines = append(lines, row(len(ap.Dirs), "(direct input…)"))
		if ap.Direct {
			lines = append(lines, "", searchStyle.Render("dir: "+ap.DirInput+"_"))
		}
		lines = append(lines, "", warnStyle.Render("enter: next (skill name)  esc: cancel"))
		return "add to source (1/2): destination dir", lines
	}
	name := strings.TrimSpace(ap.NameInput)
	dest := path.Join(ap.Dir, name)
	lines = append(lines,
		"destination: "+dirLabel(ap.Dir),
		searchStyle.Render("name: "+ap.NameInput+"_"),
		"",
		"creates: "+dest+"/",
	)
	if name != "" && m.sourceHasSkillDir(dest) {
		lines = append(lines, errorStyle.Render("⚠ already exists in the source"))
	}
	lines = append(lines, "", warnStyle.Render("enter: build PR plan  esc: back"))
	return "add to source (2/2): skill name", lines
}

func (m model) treeDetail(w int) (string, []string) {
	if len(m.dirs) == 0 {
		return "tree", []string{"no skills"}
	}
	d := m.dirs[m.cursors[focusTree]]
	if d.Path == localOnlyDir {
		var entries []string
		hidden := 0
		for _, lo := range m.localOnly {
			if len(lo.copiesFor(m.scope)) == 0 {
				hidden++
				continue
			}
			var locs []string
			for _, c := range lo.copiesFor(m.scope) {
				locs = append(locs, strings.Join(c.Agents, "+"))
			}
			entries = append(entries, warnStyle.Render("O")+" "+truncate(lo.Name, max(1, w-4))+
				"  "+mutedStyle.Render("("+lo.Reason.String()+"; "+strings.Join(locs, ", ")+")"))
		}
		lines := []string{fmt.Sprintf("%d outside-source skill(s) @%s", len(entries), m.scope), ""}
		lines = append(lines, entries...)
		if hidden > 0 {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("(+%d @%s — u switches scope)", hidden, otherScope(m.scope))))
		}
		lines = append(lines, "", "space selects; p proposes import, or a matching source row can ADOPT")
		return "(outside source)", lines
	}
	title := d.Path + "/"
	if d.Path == "" {
		title = "(all)"
	}
	var lines []string
	count, marked := 0, 0
	var entries []string
	for _, s := range m.skills {
		if !underDir(s.Path, d.Path) {
			continue
		}
		count++
		mark := " "
		if m.selected[s.Path] {
			mark = "x"
			marked++
		}
		entries = append(entries,
			"["+styledMark(mark)+"]"+styledMark(m.skillIndicator(s))+" "+truncate(s.Dir(), max(1, w-6)))
	}
	lines = append(lines, fmt.Sprintf("%d skills, %d marked", count, marked), "")
	lines = append(lines, entries...)
	return title, lines
}

func (m model) skillDetail(w int) (string, []string) {
	if lo, ok := m.currentLocalOnly(); ok {
		lines := wrapLines("outside source: "+lo.Reason.String()+" (p proposes import; a matching source row can ADOPT)", w)
		cp, errMsg := m.resolveOutsideCopy(lo)
		if errMsg != "" {
			lines = append(lines, "", errorStyle.Render(errMsg), "", "copies:")
			for _, c := range lo.copiesFor(m.scope) {
				lines = append(lines, wrapLines("  "+strings.Join(c.Agents, "+")+"  "+homeShorten(c.Inst.Dir), w)...)
			}
			return lo.Name + " (outside)", lines
		}
		if cp.Inst.RepoSlug != "" {
			lines = append(lines, wrapLines("original repository: "+cp.Inst.RepoSlug, w)...)
		}
		lines = append(lines, wrapLines("showing copy: "+homeShorten(cp.Inst.Dir)+
			" ("+strings.Join(cp.Agents, "+")+", "+cp.Scope+" scope)", w)...)
		lines = append(lines, strings.Repeat("─", max(1, min(w, 40))))
		lines = append(lines, highlightMarkdown(wrapLines(cp.Inst.SkillMD, w))...)
		return lo.Name + " (outside)", lines
	}
	s, ok := m.currentSkill()
	if !ok {
		return "skill", []string{"no skill selected"}
	}
	lines := m.skillInstallSummary(s, w)
	switch {
	case s.LoadErr != "":
		lines = append(lines, wrapLines(s.LoadErr, w)...)
	case s.Content != "":
		lines = append(lines, highlightMarkdown(wrapLines(s.Content, w))...)
	case s.Loading:
		lines = append(lines, "loading preview...")
	default:
		lines = append(lines, "preview not loaded")
	}
	return s.Dir(), lines
}

// skillInstallSummary spells out per agent what the badge column encodes.
func (m model) skillInstallSummary(s skill, w int) []string {
	var parts []string
	for _, p := range m.installTargets {
		if !m.agentSelected[p.Name] {
			continue
		}
		switch m.badgeState(s, p) {
		case badgeManaged:
			parts = append(parts, p.Name+":✓")
		case badgeOutdated:
			parts = append(parts, p.Name+":↓")
		case badgeModified:
			parts = append(parts, p.Name+":m")
		case badgeForeign:
			parts = append(parts, p.Name+":O")
		case badgeUntracked:
			parts = append(parts, p.Name+":O")
		default:
			parts = append(parts, p.Name+":-")
		}
	}
	if len(parts) == 0 {
		return nil
	}
	lines := wrapLines("installed@"+m.scope+":  "+strings.Join(parts, "  "), w)
	return append(lines, strings.Repeat("─", max(1, min(w, 40))))
}

// highlightMarkdown colorizes already-wrapped plain lines; wrapping must
// happen first because styled lines cannot be safely truncated later.
func highlightMarkdown(lines []string) []string {
	out := make([]string, 0, len(lines))
	inFrontmatter := len(lines) > 0 && strings.TrimSpace(lines[0]) == "---"
	inCode := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case inFrontmatter:
			out = append(out, mutedStyle.Render(line))
			if i > 0 && trimmed == "---" {
				inFrontmatter = false
			}
		case strings.HasPrefix(trimmed, "```"):
			inCode = !inCode
			out = append(out, mutedStyle.Render(line))
		case inCode:
			out = append(out, codeStyle.Render(line))
		case strings.HasPrefix(trimmed, "#"):
			out = append(out, headingStyle.Render(line))
		case strings.HasPrefix(trimmed, ">"):
			out = append(out, quoteStyle.Render(line))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ "):
			rest := strings.TrimLeft(line, " \t")
			indent := line[:len(line)-len(rest)]
			out = append(out, indent+warnStyle.Render(rest[:2])+rest[2:])
		default:
			out = append(out, line)
		}
	}
	return out
}

func classLegend() []string {
	return []string{
		markStyle.Render("✓") + " managed   = installed from an allowed source",
		updateStyle.Render("↓") + " outdated  = installed, but the source has newer content",
		modifiedStyle.Render("m") + " modified  = installed copy was edited locally (p proposes a PR)",
		warnStyle.Render("O") + " outside   = not managed here (p imports; matching source row can ADOPT)",
		mutedStyle.Render("brackets are selection only: [ ] unselected / [x] selected"),
	}
}

func (m model) agentDetail(w int) (string, []string) {
	// Outside mode lists copy locations; it never becomes an install/delete
	// target; describe which copy p would propose.
	if lo, ok := m.currentLocalOnly(); ok {
		cp, errMsg := m.resolveOutsideCopy(lo)
		lines := []string{"copies of this outside skill @" + m.scope + " (→ = PR source):", ""}
		for _, c := range lo.copiesFor(m.scope) {
			marker := "  "
			if errMsg == "" && c.Inst.Dir == cp.Inst.Dir {
				marker = "→ "
			}
			lines = append(lines, marker+strings.Join(c.Agents, "+")+"  "+homeShorten(c.Inst.Dir))
		}
		if other := lo.copiesFor(otherScope(m.scope)); len(other) > 0 {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("  (+%d copy(ies) @%s — u switches scope)", len(other), otherScope(m.scope))))
		}
		if errMsg != "" {
			lines = append(lines, "", errorStyle.Render(errMsg))
			lines = append(lines, "", warnStyle.Render("move the panel-2 cursor onto the desired agent"))
			return lo.Name + " (outside)", lines
		}
		lines = append(lines, "", "files in the PR-source copy:")
		var rels []string
		for rel := range cp.Inst.FileShas {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			lines = append(lines, "  "+rel)
		}
		lines = append(lines, "", warnStyle.Render("p proposes an import PR; P picks the destination first"))
		lines = append(lines, "")
		lines = append(lines, classLegend()...)
		return lo.Name + " (outside)", lines
	}
	p := m.installTargets[m.cursors[focusAgents]]
	dest := p.dirFor(m.scope, m.projectRoot)

	var command string
	if p.GHAgent != "" {
		command = fmt.Sprintf("gh skill install %s <skill> --agent %s --scope %s", m.cfg.Source, p.GHAgent, m.scope)
	} else {
		command = fmt.Sprintf("gh skill install %s <skill> --dir %s", m.cfg.Source, homeShorten(dest))
	}

	var lines []string
	lines = append(lines, "command used by an i plan:")
	for _, l := range wrapLines("  "+command, w) {
		lines = append(lines, codeStyle.Render(l))
	}
	if p.GHAgent == "" {
		lines = append(lines, "  (gh has no --agent value for "+p.Name+", so --dir is used instead)")
	}
	if m.cliOverride {
		lines = append(lines, "", "note: --agent/--dir was given on the command line,", "      so panel-2 marks are ignored")
	}

	scopeMark := func(s string) string {
		if s == m.scope {
			return warnStyle.Render("  ← current (u key toggles)")
		}
		return ""
	}
	lines = append(lines,
		"",
		"where skills end up, by scope:",
		fmt.Sprintf("  user    scope: %s%s", p.UserDir, scopeMark("user")),
		fmt.Sprintf("  project scope: %s%s", homeShorten(filepath.Join(m.projectRoot, p.ProjectDir)), scopeMark("project")),
		"",
		"already installed there:",
	)
	found := false
	for _, t := range m.targets {
		if !containsStr(t.Agents, p.Short) {
			continue
		}
		found = true
		managed, outside := 0, 0
		for _, inst := range t.Skills {
			if m.installedCopyOutside(inst) {
				outside++
			} else {
				managed++
			}
		}
		if managed+outside == 0 {
			lines = append(lines, fmt.Sprintf("  %-7s %s: empty", t.Scope, t.Display))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-7s %s: %d managed  %d outside",
			t.Scope, t.Display, managed, outside))
	}
	if !found {
		lines = append(lines, "  (scanning...)")
	}

	if s, ok := m.currentSkill(); ok {
		if inst, ok := m.lookup.byKey[m.skillKey(s)][p.Short+"|"+m.scope]; ok && m.copyModified(inst) {
			lines = append(lines, "", modifiedStyle.Render("local changes")+" in "+homeShorten(inst.Dir)+"  (p proposes a PR):")
			for _, c := range m.copyChanges(inst) {
				lines = append(lines, "  "+c)
			}
			lines = append(lines, "")
			lines = append(lines, m.skillMDDiff(s, inst, w)...)
		}
	}

	lines = append(lines, "")
	lines = append(lines, classLegend()...)
	return p.Name, lines
}

// skillMDDiff renders a unified diff of the installed (normalized) SKILL.md
// against the source version, colorized per line.
func (m model) skillMDDiff(s skill, inst installedSkill, w int) []string {
	if s.Content == "" {
		return []string{mutedStyle.Render("(SKILL.md diff pending: source content not loaded yet)")}
	}
	key := fmt.Sprintf("%s|%s|%d", inst.Dir, gitBlobSha([]byte(inst.SkillMD+s.Content)), w)
	if cached, ok := m.diffCache[key]; ok {
		return cached
	}
	installed := reconstructSkillMD(s.Content, inst.SkillMD)
	diff, err := diffTexts(s.Content, installed)
	var out []string
	switch {
	case err != nil:
		out = []string{mutedStyle.Render("(diff unavailable: " + err.Error() + ")")}
	case strings.TrimSpace(diff) == "":
		out = []string{mutedStyle.Render("(SKILL.md identical; changes are in other files)")}
	default:
		out = renderDiff(diff, w)
	}
	m.diffCache[key] = out
	return out
}

func (m model) statusDetail(w int) (string, []string) {
	if len(m.targets) == 0 {
		return "installed", []string{"scanning..."}
	}
	t := m.targets[m.cursors[focusStatus]]
	title := fmt.Sprintf("%s  (%s: %s)", t.Display, t.Scope, strings.Join(t.Agents, ", "))
	var lines []string
	if t.Err != "" {
		lines = append(lines, "scan error: "+t.Err, "")
	}
	if len(t.Skills) == 0 {
		lines = append(lines, "no skills installed")
		return title, lines
	}
	for _, s := range t.Skills {
		detail := ""
		switch {
		case s.Class == classManaged && s.LocalPath != "":
			detail = homeShorten(s.LocalPath)
		case s.Class == classManaged || s.Class == classForeign:
			detail = s.RepoSlug
			if s.Ref != "" {
				detail += "@" + s.Ref
			}
		default:
			detail = "(no tracking metadata)"
		}
		mark := s.Class.mark()
		markStyled := s.Class.style().Render(mark)
		note := ""
		if m.installedCopyOutside(s) {
			mark = "O"
			markStyled = warnStyle.Render(mark)
			switch {
			case s.Class == classUntracked:
				note = " (outside: no tracking)"
			case !m.copyFromConfiguredSource(s):
				note = " (outside: external source)"
			default:
				note = " (outside: source path missing)"
			}
		} else if s.Class == classManaged && s.TreeSha != "" {
			if current, ok := m.treeShas[s.Key()]; ok && current != s.TreeSha {
				markStyled = updateStyle.Render("↓")
				note = " (outdated)"
			}
		}
		if note == "" && s.Class == classManaged && m.copyModified(s) {
			markStyled = modifiedStyle.Render("m")
			note = " (edited locally)"
		}
		body := truncate(fmt.Sprintf("%s  %s%s", s.Name, detail, note), max(1, w-2))
		line := markStyled + " " + body
		lines = append(lines, line)
	}
	lines = append(lines, "")
	lines = append(lines, classLegend()...)
	return title, lines
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// --- box drawing ---

func listOffset(cursor, n, h int) int {
	if n <= h || h <= 0 {
		return 0
	}
	off := cursor - h/2
	return max(0, min(n-h, off))
}

func boxTop(title string, w int, focused bool) string {
	style := borderStyle
	if focused {
		style = focusBorderStyle
	}
	inner := "─ " + title + " "
	if lipgloss.Width(inner) > w-2 {
		inner = truncate(inner, w-2)
	}
	line := "┌" + inner + strings.Repeat("─", max(0, w-2-lipgloss.Width(inner))) + "┐"
	return style.Render(line)
}

// boxRowCell wraps an already-fitted cell (exactly w-2 cells wide) in borders.
func boxRowCell(cell string, w int, focused bool) string {
	style := borderStyle
	if focused {
		style = focusBorderStyle
	}
	return style.Render("│") + cell + style.Render("│")
}

func boxBottom(w int, focused bool) string {
	style := borderStyle
	if focused {
		style = focusBorderStyle
	}
	return style.Render("└" + strings.Repeat("─", max(0, w-2)) + "┘")
}

func simpleView(width, height int, lines []string) string {
	width = max(60, width)
	height = max(12, height)
	out := make([]string, 0, height)
	for _, line := range lines {
		if len(out) >= height {
			break
		}
		out = append(out, padRendered(line, width))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return strings.Join(out, "\n")
}
