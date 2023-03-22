/* Terminal user interface package */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/grisu48/gopenqa"
)

// Declare ANSI color codes
const ANSI_RED = "\u001b[31m"
const ANSI_GREEN = "\u001b[32m"
const ANSI_YELLOW = "\u001b[33m"
const ANSI_BRIGHTYELLOW = "\u001b[33;1m"
const ANSI_BLUE = "\u001b[34m"
const ANSI_MAGENTA = "\u001b[35m"
const ANSI_CYAN = "\u001b[36m"
const ANSI_WHITE = "\u001b[37m"
const ANSI_RESET = "\u001b[0m"

const ANSI_ALT_SCREEN = "\x1b[?1049h"
const ANSI_EXIT_ALT_SCREEN = "\x1b[?1049l"

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

type KeyPressCallback func(byte)

/* Declares the terminal user interface */
type TUI struct {
	Model TUIModel
	done  chan bool

	Keypress KeyPressCallback

	status      string   // Additional status text
	tracker     string   // Additional tracker text for RabbitMQ messages
	header      string   // Additional header text
	hideStatus  []string // Statuses to hide
	hide        bool     // Hide statuses in hideStatus
	showTracker bool     // Show tracker
	showStatus  bool     // Show status line
	sorting     int      // Sorting method - 0: none, 1 - by job group

	screensize int // Lines per screen
}

func CreateTUI() TUI {
	var tui TUI
	tui.done = make(chan bool, 1)
	tui.Keypress = nil
	tui.hide = true
	tui.showTracker = false
	tui.showStatus = true
	tui.Model.jobs = make([]gopenqa.Job, 0)
	tui.Model.jobGroups = make(map[int]gopenqa.JobGroup, 0)
	tui.Model.reviewed = make(map[int64]bool, 0)
	return tui
}

/* The model that will be displayed in the TUI*/
type TUIModel struct {
	jobs       []gopenqa.Job            // Jobs to be displayed
	jobGroups  map[int]gopenqa.JobGroup // Job Groups
	mutex      sync.Mutex               // Access mutex to the model
	offset     int                      // Line offset for printing
	printLines int                      // Lines that would need to be printed, needed for offset handling
	reviewed   map[int64]bool           // Indicating if failed jobs are reviewed
}

func (tui *TUI) visibleJobCount() int {
	counter := 0
	for _, job := range tui.Model.jobs {
		if !tui.hideJob(job) {
			counter++
		}
	}
	return counter
}

func (model *TUIModel) SetReviewed(job int64, reviewed bool) {
	model.reviewed[job] = reviewed
}

func (model *TUIModel) isReviewed(job int64) bool {
	reviewed, found := model.reviewed[job]
	return found && reviewed
}

func (tui *TUIModel) MoveHome() {
	tui.mutex.Lock()
	defer tui.mutex.Unlock()
	tui.offset = 0
}

func (tui *TUIModel) Apply(jobs []gopenqa.Job) {
	tui.mutex.Lock()
	defer tui.mutex.Unlock()
	tui.jobs = jobs
}

func (model *TUIModel) Jobs() []gopenqa.Job {
	return model.jobs
}

func (tui *TUIModel) SetJobGroups(grps map[int]gopenqa.JobGroup) {
	tui.jobGroups = grps
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

// Apply sorting method. 0 = none, 1 = by job group
func (tui *TUI) SetSorting(sorting int) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.sorting = sorting
}

func (tui *TUI) Sorting() int {
	return tui.sorting
}

func (tui *TUI) SetStatus(status string) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.status = status
}

func (tui *TUI) Status() string {
	return tui.status
}

func (tui *TUI) SetTracker(tracker string) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.tracker = tracker
}

func (tui *TUI) SetShowTracker(tracker bool) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.showTracker = tracker
}

func (tui *TUI) ShowTracker() bool {
	return tui.showTracker

}

func (tui *TUI) SetHeader(header string) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.header = header
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
		k := b[0]

		// Shift history, do it manually for now
		p[2], p[1], p[0] = p[1], p[0], k

		// Catch special keys
		if p[1] == 91 && k == 65 { // Arrow up
			if tui.Model.offset > 0 {
				tui.Model.offset--
				tui.Update()
			}
		} else if p[1] == 91 && k == 66 { // Arrow down
			max := max(0, (tui.Model.printLines - tui.screensize))
			if tui.Model.offset < max {
				tui.Model.offset++
				tui.Update()
			}
		} else if p[2] == 27 && p[1] == 91 && p[0] == 72 { // home
			tui.Model.offset = 0
		} else if p[2] == 27 && p[1] == 91 && p[0] == 70 { // end
			tui.Model.offset = max(0, (tui.Model.printLines - tui.screensize))
		} else if p[2] == 27 && p[1] == 91 && p[0] == 53 { // page up
			// Always leave one line overlap for better orientation
			tui.Model.offset = max(0, tui.Model.offset-tui.screensize+1)
		} else if p[2] == 27 && p[1] == 91 && p[0] == 54 { // page down
			max := max(0, (tui.Model.printLines - tui.screensize))
			// Always leave one line overlap for better orientation
			tui.Model.offset = min(max, tui.Model.offset+tui.screensize-1)
		}

		// Forward keypress to listener
		if tui.Keypress != nil {
			tui.Keypress(k)
		}
	}
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

// awaits SIGINT or SIGTERM
func (tui *TUI) awaitTerminationSignal() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Println(sig)
		tui.done <- true
	}()
	<-tui.done
}

func (tui *TUI) hideJob(job gopenqa.Job) bool {
	if !tui.hide {
		return false
	}
	state := job.JobState()
	for _, s := range tui.hideStatus {
		if state == s {
			return true
		}

		// Special reviewed keyword
		if s == "reviewed" && (state == "failed" || state == "parallel_failed" || state == "incomplete") {
			if reviewed, found := tui.Model.reviewed[job.ID]; found && reviewed {
				return true
			}
		}
	}
	return false
}

// print all jobs unsorted
func (tui *TUI) buildJobsScreen(width int) []string {
	lines := make([]string, 0)
	for _, job := range tui.Model.jobs {
		if !tui.hideJob(job) {
			lines = append(lines, tui.formatJobLine(job, width))
		}
	}
	return lines
}

func sortedKeys(vals map[string]int) []string {
	n := len(vals)
	ret := make([]string, n)
	i := 0
	for s := range vals {
		ret[i] = s
		i++
	}
	sort.Strings(ret)
	return ret
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

	// Determine active groups first
	groups := make(map[int][]gopenqa.Job, 0)
	for _, job := range tui.Model.jobs {
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
		grp := tui.Model.jobGroups[id]
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

func cut(text string, n int) string {
	if len(text) < n {
		return text
	} else {
		return text[:n]
	}
}
func trimEmptyTail(lines []string) []string {
	// Crop empty elements at the end of the array
	for n := len(lines) - 1; n > 0; n-- {
		if lines[n] != "" {
			return lines[0 : n+1]
		}
	}
	return lines[0:0]
}

func trimEmptyHead(lines []string) []string {
	// Crop empty elements at the end of the array
	for i := 0; i < len(lines); i++ {
		if lines[i] != "" {
			return lines[i:]
		}
	}
	return lines[0:0]
}

func trimEmpty(lines []string) []string {
	lines = trimEmptyHead(lines)
	lines = trimEmptyTail(lines)
	return lines
}

func (tui *TUI) buildHeader(width int) []string {
	lines := make([]string, 0)
	if tui.header != "" {
		lines = append(lines, tui.header)
		lines = append(lines, "q:Quit   r:Refresh   h:Hide/Show jobs   m:Toggle RabbitMQ tracker   s:Switch sorting    Arrows:Move up/down")
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

	switch tui.sorting {
	case 1:
		lines = append(lines, tui.buildJobsScreenByGroup(width)...)
	default:
		lines = append(lines, tui.buildJobsScreen(width)...)
	}
	lines = trimEmpty(lines)

	tui.Model.printLines = len(lines)
	return lines
}

/* Redraw screen */
func (tui *TUI) Update() {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	width, height := terminalSize()
	if width <= 0 || height <= 0 {
		return
	}

	// Check for unreasonable values
	if width > 1000 {
		width = 1000
	}

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
	for elem := tui.Model.offset; remainingLines > 0; remainingLines-- {
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

// NotifySend fires a Desktop notification
func NotifySend(text string) {
	cmd := exec.Command("notify-send", text)
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending notification via 'notify-send': %s\n", err)
	}
}

func getStateColorcode(state string) string {
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

func getDateColorcode(t time.Time) string {
	now := time.Now()
	diff := now.Unix() - t.Unix()
	if diff > 2*24*60*60 {
		return ANSI_RED // 2 days: red
	} else if diff > 24*60*60 {
		return ANSI_BRIGHTYELLOW // 1 day: yellow
	}
	return ANSI_WHITE
}

func (tui *TUI) formatJobLine(job gopenqa.Job, width int) string {
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
	c2 := getStateColorcode(state)
	// If it is scheduled, it does not make any sense to display the starting time, since it's not set
	if state != "scheduled" && timestamp.Unix() > 0 {
		tStr = timestamp.Format("2006-01-02-15:04:05")
	}
	// For failed jobs check if they are reviewed
	if state == "failed" || state == "incomplete" || state == "parallel_failed" {
		if reviewed, found := tui.Model.reviewed[job.ID]; found && reviewed {
			c2 = ANSI_MAGENTA
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

func terminalSize() (int, int) {
	ws := &winsize{}
	ret, _, _ := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)))

	if int(ret) == 0 {
		return int(ws.Col), int(ws.Row)
	} else {
		return 80, 24 // Default value
	}
}

func spaces(n int) string {
	ret := ""
	for i := 0; i < n; i++ {
		ret += " "
	}
	return ret
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
