package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

/* Terminal color codes */
const KNRM = "\x1B[0m"
const KRED = "\x1B[31m"
const KGRN = "\x1B[32m"
const KYEL = "\x1B[33m"
const KBLU = "\x1B[34m"
const KMAG = "\x1B[35m"
const KCYN = "\x1B[36m"
const KWHT = "\x1B[37m"

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

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
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

func clearScreen() {
	fmt.Print("\033[2J\033[;H") //\033[2J\033[H\033[2J")
}

func moveCursorBeginning() {
	fmt.Print("\033[H")
}

func moveCursorLineBeginning(line int) {
	fmt.Printf("\033[%dH", line)
}

func hideCursor() {
	fmt.Print("\033[?25l")
}

func showCursor() {
	fmt.Print("\033[?25h")
}
