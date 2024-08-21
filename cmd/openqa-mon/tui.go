/* Terminal user interface for openqa-mon */
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/grisu48/gopenqa"
	"github.com/os-autoinst/openqa-mon/internal"
	"golang.org/x/crypto/ssh/terminal"
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

/* Declares the terminal user interface */
type TUI struct {
	// Note: The convention is that changes in the tui will trigger an immediate update(), while changes in the model don't

	Model TUIModel
	done  chan bool

	Keypress KeyPressCallback

	header      string // program version, remote servers and current/total page
	status      string // Additional status text
	remotes     string // address of the openQA server or number of monitored instances
	showStatus  bool   // Show status line
	showHelp    bool   // Show help line
	hideEnable  bool   // If hideStates will be considered
	currentPage int    // the page we are displaying (0=first)
	totalPages  int    // how many pages are there to display
}

/* The model that will be displayed in the TUI */
type TUIModel struct {
	jobs       []gopenqa.Job // Jobs to be displayed
	HideStates []string      // Jobs with this status will be hidden
	mutex      sync.Mutex    // Access mutex to the model
}

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

type KeyPressCallback func(byte)

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

func IsTTY() bool {
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		return true
	} else {
		return false
	}
}

func PrintLine(line string, maxWidth int) {
	if maxWidth > 0 && len(line) > maxWidth {
		line = line[:maxWidth]
	}
	fmt.Println(line)
}

func CreateTUI() TUI {
	var tui TUI
	tui.done = make(chan bool, 1)
	tui.Keypress = nil
	tui.status = ""
	tui.showStatus = true
	tui.hideEnable = true
	tui.currentPage = 0
	tui.totalPages = 1
	return tui
}

func bell() {
	// Use system bell
	fmt.Print("\a")
}

func notifySend(text string) {
	cmd := exec.Command("notify-send", text)
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending notification via 'notify-send': %s\n", err)
	}
}

// Println prints the current job in a 80 character wide line with optional colors enabled
func PrintJob(job gopenqa.Job, useColors bool, width int) {
	status := job.JobState()
	if useColors {
		if job.State == "running" {
			fmt.Print(ANSI_BLUE)
		} else if job.State == "done" {
			status = job.Result
			switch job.Result {
			case "failed", "incomplete":
				fmt.Print(ANSI_RED)
			case "cancelled", "user_cancelled":
				fmt.Print(ANSI_MAGENTA)
			case "passed":
				fmt.Print(ANSI_GREEN)
			case "user_restarted", "parallel_restarted":
				fmt.Print(ANSI_BLUE)
			case "softfailed":
				fmt.Print(ANSI_YELLOW)
			default:
				fmt.Print(ANSI_WHITE)
			}
		} else if job.State == "cancelled" {
			fmt.Print(ANSI_MAGENTA)
		} else {
			fmt.Print(ANSI_CYAN)
		}
	}

	// Spacing rules:
	// |id 8 chars|2 spaces|name@machine[2spaces|link]|2 spaces|status 15 characteres

	// fixed characters: 8+2+2+18 = 30
	fixedCharacters := 30

	name := job.Prefix
	if len(name) > 0 {
		name += " "
	}
	name += job.Test + "@" + job.Settings.Machine
	link := job.Link

	// Is there space for the link (including 2 additional spaces between name and link)?
	if len(name)+len(link)+2 > width-fixedCharacters {
		link = ""
	}

	// Add two spaces between name and link, if applicable
	if link != "" {
		link = "  " + link
	}
	// Crop or extend name with spaces to fill the whole line
	i := width - fixedCharacters - len(link) - len(name)
	if i < 0 {
		name = name[:width-fixedCharacters]
		link = ""
	} else if i > 0 {
		// Expand name
		name = name + strings.Repeat(" ", i)
	}
	if len(status) < 18 {
		status = strings.Repeat(" ", 18-len(status)) + status
	}
	fmt.Printf("%8d  %s%s  %.18s\n", job.ID, name, link, status)

	// Reset color
	if useColors {
		fmt.Print(ANSI_RESET)
	}
}

func (m *TUIModel) SetJobs(jobs []gopenqa.Job) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.jobs = uniqueJobs(jobs)
}

func (tui *TUI) Start() {
	// disable input buffering, if tty
	if terminal.IsTerminal(int(os.Stdin.Fd())) {
		exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	} else {
		fmt.Fprintf(os.Stderr, "warning: running tui on a non-tty terminal\n")
	}
	go tui.readInput()
	// Listen for terminal changes signal
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGWINCH)
		for {
			<-sigs
			tui.UpdateHeader()
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

func (tui *TUI) SetHeader(header string) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.header = header
	tui.Update()
}

func (tui *TUI) Header() string {
	return tui.header
}

func (tui *TUI) SetStatus(status string) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.status = status
	tui.Update()
}

func (tui *TUI) Status() string {
	return tui.status
}

func (tui *TUI) SetShowHelp(enabled bool) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.showHelp = enabled
	tui.Update()
}

func (tui *TUI) DoShowHelp() bool {
	return tui.showHelp
}

func (tui *TUI) SetHideStates(enabled bool) {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.hideEnable = enabled
	tui.Update()
}

func (tui *TUI) UpdateHeader() {
	if tui.totalPages > 1 {
		tui.header = fmt.Sprintf("openqa-mon v%s - Monitoring %s - Page %d/%d", internal.VERSION, tui.remotes, tui.currentPage+1, tui.totalPages)
	} else {
		tui.header = fmt.Sprintf("openqa-mon v%s - Monitoring %s", internal.VERSION, tui.remotes)
	}
}

func (tui *TUI) FirstPage() {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.currentPage = 0
	tui.UpdateHeader()
}

func (tui *TUI) LastPage() {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.currentPage = max(0, tui.totalPages-1)
	tui.UpdateHeader()
}

func (tui *TUI) NextPage() {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.currentPage = min(tui.totalPages-1, tui.currentPage+1)
	tui.UpdateHeader()
}

func (tui *TUI) PrevPage() {
	tui.Model.mutex.Lock()
	defer tui.Model.mutex.Unlock()
	tui.currentPage = max(0, tui.currentPage-1)
	tui.UpdateHeader()
}

func (tui *TUI) DoHideStates() bool {
	return tui.hideEnable
}

// Read keys
func (tui *TUI) readInput() {
	var b []byte = make([]byte, 1)
	for {
		if n, err := os.Stdin.Read(b); err != nil {
			fmt.Fprintf(os.Stderr, "Input stream error: %s\n", err)
			break
		} else if n == 0 { // EOL
			break
		}
		if tui.Keypress != nil {
			tui.Keypress(b[0])
		}
	}
}

func (tui *TUI) doHideJob(j gopenqa.Job) bool {
	state := j.JobState()
	for _, s := range tui.Model.HideStates {
		if state == s {
			return true
		}
	}
	return false
}

// Redraw tui
func (tui *TUI) Update() {
	if len(tui.Model.jobs) == 0 {
		// no jobs fetched yet, nothing to display
		return
	}
	width, height := terminalSize()

	tui.Clear()
	lines := 0
	if tui.header != "" {
		PrintLine(tui.header, width)
		lines++
	}
	if tui.showHelp {
		help := "?:Toggle help  r:Refresh  d:Toggle notifications  b:Toggle bell  +/-:Modify refresh time  p:Toggle pause  <>:Page"
		if len(tui.Model.HideStates) > 0 {
			help += "  h:Toggle hide"
		}
		help += "  q:Quit"
		PrintLine(help, width)
		lines++
	}
	pageHeight := height - 1
	if tui.showStatus {
		pageHeight--
	}
	// ensure to always have something to display without exceeding the slice limits
	startIdx := min(tui.currentPage*pageHeight, len(tui.Model.jobs))
	endIdx := min(startIdx+pageHeight, len(tui.Model.jobs))
	tui.totalPages = len(tui.Model.jobs) / pageHeight
	if len(tui.Model.jobs)%pageHeight > 0 {
		tui.totalPages++ // one more page for any partial-page leftover
	}
	for _, job := range tui.Model.jobs[startIdx:endIdx] {
		if tui.hideEnable && tui.doHideJob(job) {
			continue
		}
		PrintJob(job, true, width)
		lines++
	}

	// print some empty lines if needed to fill last page and make footer always on last line
	for lines <= pageHeight {
		fmt.Println()
		lines++
	}

	// Status line
	if tui.showStatus {
		// Add footer, if possible
		status := tui.status
		footer := "openqa-mon (https://github.com/os-autoinst/openqa-mon)"
		if width >= len(status)+len(footer)+5 {
			spaces := strings.Repeat(" ", width-len(status)-len(footer)-1)
			status += spaces + footer
		}
		fmt.Print(status)
	}
}
