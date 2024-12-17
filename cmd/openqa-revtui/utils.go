package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
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
const ANSI_BOLD = "\u001b[1m"
const ANSI_RESET = "\u001b[0m"

const ANSI_ALT_SCREEN = "\x1b[?1049h"
const ANSI_EXIT_ALT_SCREEN = "\x1b[?1049l"

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !errors.Is(err, fs.ErrNotExist)
}

func homeDir() string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return usr.HomeDir
}

// Split a NAME=VALUE string
func splitNV(v string) (string, string, error) {
	i := strings.Index(v, "=")
	if i < 0 {
		return "", "", fmt.Errorf("no separator")
	}
	return v[:i], v[i+1:], nil
}

// Parse additional parameter macros
func parseParameter(param string) string {
	if strings.Contains(param, "%today%") {
		today := time.Now().Format("20060102")
		param = strings.ReplaceAll(param, "%today%", today)
	}
	if strings.Contains(param, "%yesterday%") {
		today := time.Now().AddDate(0, 0, -1).Format("20060102")
		param = strings.ReplaceAll(param, "%yesterday%", today)
	}

	return param
}

// Returns the remote host from a RabbitMQ URL
func rabbitRemote(remote string) string {
	i := strings.Index(remote, "@")
	if i > 0 {
		return remote[i+1:]
	}
	return remote
}

func extractFilename(path string) string {
	i := strings.LastIndex(path, "/")
	if i > 0 && i < len(path)-1 {
		return path[i+1:]
	}
	return path
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

// NotifySend fires a Desktop notification
func NotifySend(text string) {
	cmd := exec.Command("notify-send", text)
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending notification via 'notify-send': %s\n", err)
	}
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
