/* Terminal user interface package */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/os-autoinst/gopenqa"
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// Callback function - key and a update flag (defaults to true)
type KeyPressCallback func(byte, *bool)

/* Declares the terminal user interface */
type TUI struct {
	Tabs        []TUIModel       // Configuration containers displayed as tabs
	Keypress    KeyPressCallback // Callback function for every keypress
	currentTab  int              // Currently selected tab
	status      string           // Additional status text
	tracker     string           // Additional tracker text for RabbitMQ messages
	header      string           // Additional header text
	hideStatus  []string         // Statuses to hide
	hide        bool             // Hide statuses in hideStatus
	showTracker bool             // Show tracker
	showStatus  bool             // Show status line

	screensize int       // Lines per screen
	done       chan bool // Program termination signal
}

func CreateTUI() *TUI {
	var tui TUI
	tui.done = make(chan bool, 1)
	tui.Keypress = nil
	tui.hide = true
	tui.showTracker = false
	tui.showStatus = true
	tui.Tabs = make([]TUIModel, 0)
	return &tui
}

/* One tab for the TUI */
type TUIModel struct {
	Instance *gopenqa.Instance // openQA instance for this config
	Config   *Config           // Job group configuration for this model

	jobs       []gopenqa.Job            // Jobs to be displayed
	jobGroups  map[int]gopenqa.JobGroup // Job Groups
	offset     int                      // Line offset for printing
	printLines int                      // Lines that would need to be printed, needed for offset handling
	reviewed   map[int64]bool           // Indicating if failed jobs are reviewed
	sorting    int                      // Sorting method - 0: none, 1 - by job group
}

func (model *TUIModel) SetReviewed(job int64, reviewed bool) {
	model.reviewed[job] = reviewed
}

func (model *TUIModel) HideJob(job gopenqa.Job) bool {
	status := job.JobState()
	for _, s := range model.Config.HideStatus {
		if status == s {
			return true
		}
	}
	return false
}

func (tui *TUIModel) MoveHome() {
	tui.offset = 0
}

func (tui *TUIModel) Apply(jobs []gopenqa.Job) {
	tui.jobs = jobs
}

func (model *TUIModel) Jobs() []gopenqa.Job {
	return model.jobs
}

func (model *TUIModel) Job(id int64) *gopenqa.Job {
	for i := range model.jobs {
		if model.jobs[i].ID == id {
			return &model.jobs[i]
		}
	}
	// Return dummy job
	job := gopenqa.Job{ID: 0}
	return &job
}

func (tui *TUIModel) SetJobGroups(grps map[int]gopenqa.JobGroup) {
	tui.jobGroups = grps
}

// Apply sorting method. 0 = none, 1 = by job group
func (tui *TUIModel) SetSorting(sorting int) {
	tui.sorting = sorting
}

func (tui *TUIModel) Sorting() int {
	return tui.sorting
}

func (tui *TUI) NextTab() {
	if len(tui.Tabs) > 1 {
		tui.currentTab--
		if tui.currentTab < 0 {
			tui.currentTab = len(tui.Tabs) - 1
		}
		tui.Update()
	}
}

func (tui *TUI) GetVisibleJobs() []gopenqa.Job {
	jobs := make([]gopenqa.Job, 0)
	model := &tui.Tabs[tui.currentTab]
	for _, job := range model.jobs {
		if !tui.hideJob(job) {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func (tui *TUI) PreviousTab() {
	if len(tui.Tabs) > 1 {
		tui.currentTab = (tui.currentTab + 1) % len(tui.Tabs)
		tui.Update()
	}
}

func (tui *TUI) SetHide(hide bool) {
	tui.hide = hide
}

func (tui *TUI) Hide() bool {
	return tui.hide
}

func (tui *TUI) SetHideStatus(st []string) {
	tui.hideStatus = st
}

func (tui *TUI) SetStatus(status string) {
	tui.status = status
}

func (tui *TUI) SetTemporaryStatus(status string, duration int) {
	old := tui.status
	tui.status = status
	tui.Update()

	// Reset status text after duration, if the status text has not been altered in the meantime
	go func(old, status string, duration int) {
		time.Sleep(time.Duration(duration) * time.Second)
		if tui.status == status {
			tui.status = old
			tui.Update()
		}
	}(old, status, duration)
}

func (tui *TUI) Status() string {
	return tui.status
}

func (tui *TUI) SetTracker(tracker string) {
	tui.tracker = tracker
}

func (tui *TUI) SetShowTracker(tracker bool) {
	tui.showTracker = tracker
}

func (tui *TUI) ShowTracker() bool {
	return tui.showTracker

}

func (tui *TUI) SetHeader(header string) {
	tui.header = header
}

func (tui *TUI) CreateTUIModel(cf *Config) *TUIModel {
	instance := gopenqa.CreateInstance(cf.Instance)
	instance.SetUserAgent("openqa-mon/revtui")
	tui.Tabs = append(tui.Tabs, TUIModel{Instance: &instance, Config: cf})
	model := &tui.Tabs[len(tui.Tabs)-1]
	model.jobGroups = make(map[int]gopenqa.JobGroup)
	model.jobs = make([]gopenqa.Job, 0)
	model.reviewed = make(map[int64]bool)
	return model
}

// Model returns the currently selected model
func (tui *TUI) Model() *TUIModel {
	return &tui.Tabs[tui.currentTab]
}

func (tui *TUI) Start() {
	// disable input buffering
	exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	go tui.readInput()
	// Listen for terminal changes signal
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGWINCH)
		for {
			<-sigs
			tui.Update()
		}
	}()
}

func (tui *TUI) Clear() {
	fmt.Print("\033[2J\033[;H")
}

// Enter alternative screen
func (tui *TUI) EnterAltScreen() {
	fmt.Print(ANSI_ALT_SCREEN)
}

// Leave alternative screen
func (tui *TUI) LeaveAltScreen() {
	fmt.Print(ANSI_EXIT_ALT_SCREEN)
}

// Sends the Done signal to the TUI, indicating that the program should terminate now.
func (tui *TUI) Done() {
	tui.done <- true
}

// awaits SIGINT, SIGTERM or someone sending the done signal
func (tui *TUI) AwaitTermination() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Println(sig)
		tui.Done()
	}()
	<-tui.done
}

func (tui *TUI) StartPeriodicRefresh() {
	// Periodic refresh
	for i := range tui.Tabs {
		model := &tui.Tabs[i]
		interval := model.Config.RefreshInterval
		if interval > 0 {
			go func(currentTab int) {
				for {
					time.Sleep(time.Duration(interval) * time.Second)
					// Only refresh, if current tab is ours
					if tui.currentTab == currentTab {
						if err := RefreshJobs(); err != nil {
							tui.SetStatus(fmt.Sprintf("Error while refreshing: %s", err))
						}
					}
				}
			}(i)
		}
	}
}

func (tui *TUI) hideJob(job gopenqa.Job) bool {
	if !tui.hide {
		return false
	}
	state := job.JobState()
	model := &tui.Tabs[tui.currentTab]
	for _, s := range tui.hideStatus {
		if state == s {
			return true
		}

		// Special reviewed keyword
		if s == "reviewed" && (state == "failed" || state == "parallel_failed" || state == "incomplete") {
			if reviewed, found := model.reviewed[job.ID]; found && reviewed {
				return true
			}
		}
	}
	return false
}

// print all jobs unsorted
func (tui *TUI) buildJobsScreen(width int) []string {
	lines := make([]string, 0)
	model := &tui.Tabs[tui.currentTab]
	for _, job := range model.jobs {
		if !tui.hideJob(job) {
			lines = append(lines, tui.formatJobLine(job, width))
		}
	}
	return lines
}

func jobGroupHeader(group gopenqa.JobGroup, width int) string {
	if width <= 0 {
		return ""
	}
	line := fmt.Sprintf("===== %s =====", group.Name)
	for len(line) < width {
		line += "="
	}
	// Crop if necessary
	if len(line) > width {
		line = line[:width]
	}
	return line
}

func (tui *TUI) buildJobsScreenByGroup(width int) []string {
	lines := make([]string, 0)
	model := &tui.Tabs[tui.currentTab]

	// Determine active groups first
	groups := make(map[int][]gopenqa.Job, 0)
	for _, job := range model.jobs {
		// Create item if not existing, then append job
		if _, ok := groups[job.GroupID]; !ok {
			groups[job.GroupID] = make([]gopenqa.Job, 0)
		}
		groups[job.GroupID] = append(groups[job.GroupID], job)
	}
	// Get group list and sort it by index
	grpIDs := make([]int, 0)
	for k := range groups {
		grpIDs = append(grpIDs, k)
	}
	sort.Ints(grpIDs)

	// Now print them sorted by group ID
	first := true
	for _, id := range grpIDs {
		grp := model.jobGroups[id]
		jobs := groups[id]
		statC := make(map[string]int, 0)
		hidden := 0
		if first {
			first = false
		} else {
			lines = append(lines, "")
		}
		lines = append(lines, jobGroupHeader(grp, width))

		for _, job := range jobs {
			if !tui.hideJob(job) {
				lines = append(lines, tui.formatJobLine(job, width))
			} else {
				hidden++
			}
			// Increase status counter
			status := job.JobState()
			if status == "failed" && model.reviewed[job.ID] {
				status = "reviewed"
			}
			if c, exists := statC[status]; exists {
				statC[status] = c + 1
			} else {
				statC[status] = 1
			}
		}
		line := fmt.Sprintf("Total: %d", len(jobs))
		stats := sortedKeys(statC)
		for _, s := range stats {
			c := statC[s]
			line += ", "
			// Add some color
			if s == "passed" {
				line += ANSI_GREEN
			} else if s == "cancelled" {
				line += ANSI_MAGENTA
			} else if s == "failed" || s == "parallel_failed" || s == "incomplete" {
				line += ANSI_RED
			} else if s == "reviewed" {
				line += ANSI_MAGENTA
			} else if s == "softfailed" {
				line += ANSI_YELLOW
			} else if s == "uploading" || s == "scheduled" || s == "running" {
				line += ANSI_BLUE
			} else if s == "skipped" {
				line += ANSI_WHITE
			}
			line += fmt.Sprintf("%s: %d", s, c)
			line += ANSI_RESET // Clear color
		}
		if hidden > 0 {
			line += fmt.Sprintf(" (hidden: %d)", hidden)
		}
		lines = append(lines, line)
	}
	return lines
}

func (tui *TUI) buildHeader(_ int) []string {
	lines := make([]string, 0)
	if tui.header != "" {
		lines = append(lines, tui.header)
		lines = append(lines, "q:Quit r:Refresh h:Hide/Show jobs o:Open links m:Toggle RabbitMQ tracker s:Switch sorting Arrows:Move up/down")
		// Tabs if multiple configs are present
		if len(tui.Tabs) > 1 {
			tabs := ""
			for i := range tui.Tabs {
				enabled := (tui.currentTab == i)
				model := &tui.Tabs[i]
				cf := model.Config
				name := cf.Name
				if name == "" {
					name = fmt.Sprintf("Config %d", i+1)
				}
				if enabled {
					name = ANSI_BOLD + name + ANSI_RESET
				}
				tabs += fmt.Sprintf("  [%s]", name)
			}
			lines = append(lines, tabs)
		}
	}
	return lines
}

func (tui *TUI) buildFooter(width int) []string {
	footer := make([]string, 0)
	showStatus := tui.showStatus && tui.status != ""
	showTracker := tui.showTracker && tui.tracker != ""
	if showStatus && showTracker {
		// Check if status + tracker can be merged
		common := tui.status + spaces(5) + tui.tracker
		if len(common) <= width {
			footer = append(footer, common)
		} else {
			footer = append(footer, cut(tui.status, width))
			if len(tui.tracker) <= width {
				footer = append(footer, spaces(width-len(tui.tracker))+tui.tracker)
			} else {
				footer = append(footer, tui.tracker[:width])
			}
		}
	} else if showStatus {
		footer = append(footer, cut(tui.status, width))
	} else if showTracker {
		if len(tui.tracker) <= width {
			footer = append(footer, spaces(width-len(tui.tracker))+tui.tracker)
		} else {
			footer = append(footer, tui.tracker[:width])
		}
	}
	return footer
}

// Build the full screen
func (tui *TUI) buildScreen(width int) []string {
	lines := make([]string, 0)
	model := tui.Model()

	switch model.sorting {
	case 1:
		lines = append(lines, tui.buildJobsScreenByGroup(width)...)
	default:
		lines = append(lines, tui.buildJobsScreen(width)...)
	}
	lines = trimEmpty(lines)

	// We only scroll through the screen, so those are the relevant lines
	model.printLines = len(lines)

	return lines
}

/* Redraw screen */
func (tui *TUI) Update() {
	model := tui.Model()
	width, height := terminalSize()
	if width <= 0 || height <= 0 {
		return
	}

	// Check for unreasonable values
	width = min(width, 1024)
	height = min(height, 1024)

	// Header and footer are separate. We only scroll through the "screen"
	screen := tui.buildScreen(width)
	header := tui.buildHeader(width)
	footer := tui.buildFooter(width)

	remainingLines := height
	tui.Clear()

	// Print header
	if len(header) > 0 {
		header = append(header, "") // Add additional line after header

		for _, line := range header {
			fmt.Println(line)
			remainingLines--
			if remainingLines <= 0 {
				return // crap. no need to continue
			}
		}
	}

	// Reserve lines for footer, but spare footer if there is no space left
	if len(footer) > remainingLines {
		footer = make([]string, 0)
	} else {
		remainingLines -= len(footer)
	}

	// Print screen
	screensize := 0
	for elem := model.offset; remainingLines > 0; remainingLines-- {
		if elem >= len(screen) {
			fmt.Println("") // Fill screen with empty lines for alignment
		} else {
			//fmt.Println(strings.TrimSpace(screen[elem])) // XXX
			fmt.Println(screen[elem])
			elem++
		}
		screensize++
	}
	tui.screensize = screensize

	// Print footer
	if len(footer) > 0 {
		for _, line := range footer {
			fmt.Println("")
			fmt.Print(line)
		}
	}
}

func (tui *TUI) formatJobLine(job gopenqa.Job, width int) string {
	model := &tui.Tabs[tui.currentTab]

	c1 := ANSI_WHITE // date color
	tStr := ""       // Timestamp string

	// Use tfinished as timestamp, if present
	timestamp, err := time.Parse("2006-01-02T15:04:05", job.Tfinished)
	if err != nil {
		timestamp = time.Unix(0, 0)
	}
	state := job.JobState()
	if state == "running" {
		timestamp, _ = time.Parse("2006-01-02T15:04:05", job.Tstarted)
	} else {
		c1 = getDateColorcode(timestamp)
	}
	c2 := stateColor(state)
	// If it is scheduled, it does not make any sense to display the starting time, since it's not set
	if state != "scheduled" && timestamp.Unix() > 0 {
		tStr = timestamp.Format("2006-01-02-15:04:05")
	}
	// For failed jobs check if they are reviewed
	if state == "failed" || state == "incomplete" || state == "parallel_failed" {
		if reviewed, found := model.reviewed[job.ID]; found && reviewed {
			c2 = ANSI_MAGENTA
			state = "reviewed"
		}
	}

	// Crop the state field, if necessary
	if state == "timeout_exceeded" {
		state = "timeout"
	} else if len(state) > 12 {
		state = state[0:12]
	}

	// Full status line requires 89 characters (20+4+8+1+12+1+40+3) plus name
	if width > 90 {
		// Crop the name, if necessary
		cname := job.Name
		nName := len(cname)
		if width < 89+nName {
			cname = cname[:width-90]
		}
		return fmt.Sprintf("%s%20s%s    %8d %s%-12s%s %40s | %s", c1, tStr, ANSI_RESET, job.ID, c2, state, ANSI_RESET+ANSI_WHITE, job.Link, cname)
	} else if width > 60 {
		// Just not enough space for the full line (>89 characters) ...
		// We skip the timestamp and display only the link (or job number if not available)
		// Also crop the test name, if necessary

		link := job.Link
		if link == "" {
			link = fmt.Sprintf("%-40d", job.ID)
		}
		cname := job.Name
		nName := len(cname)
		if width < 58+nName {
			// Ensure width > 58 with upper if!
			cname = cname[:width-58]
		}
		return fmt.Sprintf("%40s %s%-12s%s | %s", link, c2, state, ANSI_RESET+ANSI_WHITE, cname)
	} else {
		// Simpliest case: Just enough room for cropped name+state
		cname := job.Name
		// Crop name if necessary
		if 13+len(job.Name) > width {
			if width > 13 {
				cname = cname[:width-13]
			} else {
				cname = ""
			}
		}
		return fmt.Sprintf(c2 + fmt.Sprintf("%-12s", state) + ANSI_RESET + " " + cname)
	}
}

func (tui *TUI) readInput() {
	var b []byte = make([]byte, 1)
	var p = make([]byte, 3) // History, needed for special keys
	for {
		if n, err := os.Stdin.Read(b); err != nil {
			fmt.Fprintf(os.Stderr, "Input stream error: %s\n", err)
			break
		} else if n == 0 { // EOL
			break
		}
		model := tui.Model()

		k := b[0]

		// Shift history, do it manually for now
		p[2], p[1], p[0] = p[1], p[0], k

		// Catch special keys
		if p[2] == 27 && p[1] == 91 {
			switch k {
			case 65: // arrow up
				if model.offset > 0 {
					model.offset--
				}
			case 66: // arrow down
				max := max(0, (model.printLines - tui.screensize))
				if model.offset < max {
					model.offset++
				}
			case 72: // home
				model.offset = 0
			case 70: // end
				model.offset = max(0, (model.printLines - tui.screensize))
			case 53: // page up
				// Always leave one line overlap for better orientation
				model.offset = max(0, model.offset-tui.screensize+1)
			case 54: // page down
				max := max(0, (model.printLines - tui.screensize))
				// Always leave one line overlap for better orientation
				model.offset = min(max, model.offset+tui.screensize-1)
			case 90: // Shift+Tab
				tui.PreviousTab()
			}
		}
		// Default keys
		if k == 9 { // Tab
			tui.NextTab()
		}

		update := true

		// Forward keypress to listener
		if tui.Keypress != nil {
			tui.Keypress(k, &update)
		}

		if update {
			tui.Update()
		}
	}
}

func stateColor(state string) string {
	if state == "scheduled" || state == "assigned" {
		return ANSI_BLUE
	} else if state == "done" || state == "passed" {
		return ANSI_GREEN
	} else if state == "softfail" || state == "softfailed" {
		return ANSI_YELLOW
	} else if state == "fail" || state == "failed" || state == "incomplete" || state == "parallel_failed" {
		return ANSI_RED
	} else if state == "cancelled" || state == "user_cancelled" {
		return ANSI_MAGENTA
	} else if state == "running" {
		return ANSI_CYAN
	}
	return ANSI_WHITE
}
