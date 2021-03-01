/* Terminal user interface for openqa-mon */
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
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

/* Declares the terminal user interface */
type TUI struct {
	// Note: The convention is that changes in the tui will trigger an immediate update(), while changes in the model don't

	Model TUIModel
	done  chan bool

	Keypress KeyPressCallback

	header     string
	status     string // Additional status text
	showStatus bool   // Show status line
	showHelp   bool   // Show help line
	hideEnable bool   // If hideStates will be considered
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

func spaces(n int) string {
	ret := ""
	for i := 0; i < n; i++ {
		ret += " "
	}
	return ret
}

func CreateTUI() TUI {
	var tui TUI
	tui.done = make(chan bool, 1)
	tui.Keypress = nil
	tui.status = ""
	tui.showStatus = true
	tui.hideEnable = true
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
	if i == 0 {
	} else if i < 0 {
		name = name[:width-fixedCharacters]
		link = ""
	} else {
		// Expand name
		name = name + spaces(i)
	}

	if len(status) < 18 {
		status = spaces(18-len(status)) + status
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
	m.jobs = jobs
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
	width, height := terminalSize()

	tui.Clear()
	lines := 0
	if tui.header != "" {
		fmt.Println(tui.header)
		lines++
	}
	if tui.showHelp {
		fmt.Println("?:Toggle help    r: Refresh    h:Toggle hide    q:Quit")
		lines++
	}
	fmt.Println()
	lines++

	offset := 0
	maxHeight := height
	if tui.showStatus {
		maxHeight -= 2
	}
	for _, job := range tui.Model.jobs {
		if tui.hideEnable && tui.doHideJob(job) {
			continue
		}
		// Ignore offset jobs (for scrolling)
		if offset > 0 {
			offset--
			continue
		}
		PrintJob(job, true, width)
		lines++
		if lines >= maxHeight {
			return
		}
	}

	// Status line
	if tui.showStatus {
		// Add footer, if possible
		status := tui.status
		footer := "openqa-mon (https://github.com/grisu48/openqa-mon)"
		if width >= len(status)+len(footer)+5 {
			status += spaces(width-len(status)-len(footer)) + footer
		}
		fmt.Println("")
		fmt.Println(status)
	}
}
