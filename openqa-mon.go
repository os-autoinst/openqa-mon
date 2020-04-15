package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Terminal color codes
const KNRM = "\x1B[0m"
const KRED = "\x1B[31m"
const KGRN = "\x1B[32m"
const KYEL = "\x1B[33m"
const KBLU = "\x1B[34m"
const KMAG = "\x1B[35m"
const KCYN = "\x1B[36m"
const KWHT = "\x1B[37m"

// Remote instance
type Remote struct {
	URI  string
	Jobs []int
}

// Job is a running Job instance
type Job struct {
	AssignedWorkerID int `json:"assigned_worker_id"`
	BlockedByID      int `json:"blocked_by_id"`
	// Children
	CloneID int `json:"clone_id"`
	GroupID int `json:"group_id"`
	ID      int `json:"id"`
	// Modules
	Name string `json:"name"`
	// Parents
	Priority  int      `json:"priority"`
	Result    string   `json:"result"`
	Settings  Settings `json:"settings"`
	State     string   `json:"state"`
	Tfinished string   `json:"t_finished"`
	Tstarted  string   `json:"t_started"`
	Test      string   `json:"test"`
	/* this is added by the program and not part of the fetched json */
	Link string
}

type JobStruct struct {
	Job Job `json:"job"`
}

type Jobs struct {
	Jobs []Job `json:"jobs"`
}

type Settings struct {
	Arch    string `json:"ARCH"`
	Backend string `json:"BACKEND"`
	Machine string `json:"MACHINE"`
}

func ensureHTTP(remote string) string {
	if !(strings.HasPrefix(remote, "http://") || strings.HasPrefix(remote, "https://")) {
		return "http://" + remote
	} else {
		return remote
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

// Println prints the current job in a 80 character wide line with optional colors enabled
func (job *Job) Println(useColors bool, width int) {
	status := job.State
	if useColors {
		if job.State == "running" {
			fmt.Print(KBLU)
		} else if job.State == "done" {
			status = job.Result
			switch job.Result {
			case "failed":
				fmt.Print(KRED)
			case "incomplete":
				fmt.Print(KRED)
			case "user_cancelled":
				fmt.Print(KYEL)
			case "passed":
				fmt.Print(KGRN)
			default:
				fmt.Print(KWHT)
			}
		} else {
			fmt.Print(KCYN)
		}
	}

	// Spacing rules:
	// |id 8 chars|2 spaces|name@machine[2spaces|link]|2 spaces|status 15 characteres

	// fixed characters: 8+2+2+15 = 27
	fixedCharacters := 27

	name := job.Test + "@" + job.Settings.Machine
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

	fmt.Printf("%8d  %s%s  %15s\n", job.ID, name, link, status)

	// Reset color
	if useColors {
		fmt.Print(KNRM)
	}
}

/* Struct for sorting job slice by job id */
type byID []Job

func (s byID) Len() int {
	return len(s)
}
func (s byID) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byID) Less(i, j int) bool {
	return s[i].ID < s[j].ID

}

func fetchJob(remote string, jobID int) (Job, error) {
	var job JobStruct
	url := fmt.Sprintf("%s/api/v1/jobs/%d", remote, jobID)
	resp, err := http.Get(url)
	if err != nil {
		return job.Job, err
	}
	if resp.StatusCode != 200 {
		if resp.StatusCode == 404 {
			job.Job.ID = 0
			return job.Job, nil
		} else if resp.StatusCode == 403 {
			return job.Job, errors.New("Access denied")
		} else {
			fmt.Fprintf(os.Stderr, "Http status code %d\n", resp.StatusCode)
		}
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return job.Job, err
	}
	err = json.Unmarshal(body, &job)
	if err != nil {
		return job.Job, err
	}
	if strings.HasSuffix(remote, "/") {
		job.Job.Link = fmt.Sprintf("%st%d", remote, jobID)
	} else {
		job.Job.Link = fmt.Sprintf("%s/t%d", remote, jobID)
	}
	return job.Job, nil
}

func getJobsOverview(url string) ([]Job, error) {
	var jobs []Job
	resp, err := http.Get(url + "/api/v1/jobs/overview")
	if err != nil {
		return jobs, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return jobs, err
	}
	err = json.Unmarshal(body, &jobs)

	// Fetch more details about the jobs
	for i, job := range jobs {
		job, err = fetchJob(url, job.ID)
		if err != nil {
			return jobs, err
		}
		jobs[i] = job
	}
	return jobs, nil
}

func printHelp() {
	fmt.Printf("Usage: %s [OPTIONS] REMOTE\n  REMOTE is the base URL of the openQA server (e.g. https://openqa.opensuse.org)\n\n", os.Args[0])
	fmt.Println("OPTIONS\n")
	fmt.Println("  -h, --help                       Print this help message")
	fmt.Println("  -j, --jobs JOBS                  Display information only for the given JOBS (comma separated ids)")
	fmt.Println("  -c,--continous SECONDS           Continously display stats")
	fmt.Println("")
}

func parseJobs(jobs string) ([]int, error) {
	split := strings.Split(jobs, ",")
	ret := make([]int, 0)
	for _, sID := range split {
		id, err := strconv.Atoi(sID)
		if err != nil {
			return ret, err
		}
		ret = append(ret, id)
	}
	return ret, nil
}

// parseJobID parses the given text for a valid job id ("[#]INTEGER[:]" and INTEGER > 0). Returns the job id if valid or 0 on error
func parseJobID(parseText string) int {
	// Remove # at beginning
	for len(parseText) > 1 && parseText[0] == '#' {
		parseText = parseText[1:]
	}
	// Remote : at the end
	for len(parseText) > 1 && parseText[len(parseText)-1] == ':' {
		parseText = parseText[:len(parseText)-1]
	}
	if len(parseText) == 0 {
		return 0
	}
	num, err := strconv.Atoi(parseText)
	if err != nil {
		return 0
	}
	if num <= 0 {
		return 0
	}
	return num
}

// parseJobIDs parses the given text for a valid job id ("[#]INTEGER[:]" and INTEGER > 0) or job id ranges (MIN..MAX). Returns the job id if valid or 0 on error
func parseJobIDs(parseText string) []int {
	ret := make([]int, 0)

	// Search for range
	i := strings.Index(parseText, "..")
	if i > 0 {
		lower, upper := parseText[:i], parseText[i+2:]
		min := parseJobID(lower)
		if min <= 0 {
			return ret
		}
		max := parseJobID(upper)
		if max <= 0 {
			return ret
		}

		// Create range
		for i = min; i <= max; i++ {
			ret = append(ret, i)
		}
		return ret
	}
	i = parseJobID(parseText)
	if i > 0 {
		ret = append(ret, i)
	}
	return ret
}

func clearScreen() {
	fmt.Println("\033[2J\033[;H") //\033[2J\033[H\033[2J")
}

func moveCursorBeginning() {
	fmt.Println("\033[0;0H")
}

func hideCursor() {
	fmt.Print("\033[?25l")
}

func showCursor() {
	fmt.Print("\033[?25h")
}

func spaces(n int) string {
	ret := ""
	for i := 0; i < n; i++ {
		ret += " "
	}
	return ret
}

func max(x int, y int) int {
	if x > y {
		return x
	}
	return y
}

func main() {
	var err error
	args := os.Args[1:]
	remotes := make([]Remote, 0)
	continuous := 0 // If > 0, continously monitor

	// Parse program arguments
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if arg[0] == '-' {
			switch arg {
			case "-h", "--help":
				printHelp()
				return
			case "-j", "--jobs":
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "Missing job IDs")
					os.Exit(1)
				}
				if len(remotes) == 0 {
					fmt.Fprintf(os.Stderr, "Jobs need to be defined after a remote instance\n")
					os.Exit(1)
				}
				jobIDs := parseJobIDs(arg)
				if len(jobIDs) > 0 {
					if len(remotes) == 0 {
						fmt.Fprintf(os.Stderr, "Jobs need to be defined after a remote instance\n")
						os.Exit(1)
					}
					remote := &remotes[len(remotes)-1]
					for _, jobID := range jobIDs {
						remote.Jobs = append(remote.Jobs, jobID)
					}
				}
			case "-c", "--continous":
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "Missing continous period")
					os.Exit(1)
				}
				continuous, err = strconv.Atoi(args[i])
				if err != nil || continuous < 0 {
					fmt.Fprintln(os.Stderr, "Invalid continous period")
					fmt.Println("Continous duration needs to be a positive, non-zero integer that determines the seconds between refreshes")
					os.Exit(1)
				}
			default:
				fmt.Fprintf(os.Stderr, "Invalid argument: %s\n", arg)
				fmt.Printf("Use %s --help to display available options\n", os.Args[0])
				os.Exit(1)
			}
		} else {
			// No argument, so it's either a job id, a job id range or a remote URI.
			// If it's a uri, skip the job id test

			if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
				remote := Remote{URI: arg}
				remote.Jobs = make([]int, 0)
				remotes = append(remotes, remote)
			} else {
				// If the argument is a number only, assume it's a job ID otherwise it's a host
				jobIDs := parseJobIDs(arg)
				if len(jobIDs) > 0 {
					if len(remotes) == 0 {
						fmt.Fprintf(os.Stderr, "Jobs need to be defined after a remote instance\n")
						os.Exit(1)
					}
					remote := &remotes[len(remotes)-1]
					for _, jobID := range jobIDs {
						remote.Jobs = append(remote.Jobs, jobID)
					}
				} else {
					remote := Remote{URI: arg}
					remote.Jobs = make([]int, 0)
					remotes = append(remotes, remote)
				}
			}
		}
	}

	if len(remotes) == 0 {
		printHelp()
		return
	}

	if continuous > 0 {
		clearScreen()
	}

	// Ensure cursor is visible after termination
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		showCursor()
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			fmt.Println("Terminating.")
			os.Exit(1)
		}
	}()

	for {
		termWidth, termHeight := terminalSize()
		// Ensure a certain minimum extend
		termWidth = max(termWidth, 50)
		termHeight = max(termHeight, 10)
		spacesRow := spaces(termWidth)
		useColors := true
		if continuous > 0 {
			hideCursor()
			moveCursorBeginning()
			if len(remotes) == 1 {
				line := fmt.Sprintf("openqa-mon - Monitoring %s | Refresh every %d seconds", remotes[0].URI, continuous)
				fmt.Print(line + spaces(termWidth-len(line)))
				fmt.Println(spacesRow)
			} else {
				line := fmt.Sprintf("openqa-mon - Monitoring %d remotes | Refresh every %d seconds", len(remotes), continuous)
				fmt.Print(line + spaces(termWidth-len(line)))
				fmt.Println(spacesRow)
			}
		}
		lines := 3
		for _, remote := range remotes {
			uri := ensureHTTP(remote.URI)

			var jobs []Job
			if len(remote.Jobs) == 0 { // If no jobs are defined, fetch overview
				jobs, err = getJobsOverview(uri)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error fetching jobs: %s\n", err)
					continue
				}
				if len(jobs) == 0 {
					fmt.Println("No jobs on instance found")
					continue
				}
			} else {
				// Fetch jobs
				jobs = make([]Job, 0)
				for _, id := range remote.Jobs {
					job, err := fetchJob(uri, id)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error fetching job %d: %s\n", id, err)
						continue
					}
					jobs = append(jobs, job)
				}
			}
			// Sort jobs by ID
			sort.Sort(byID(jobs))
			for _, job := range jobs {
				if job.ID > 0 { // Otherwise it's an empty (.e. not found) job
					job.Println(useColors, termWidth)
				}
			}
			lines += len(jobs) + 1
		}
		if continuous <= 0 {
			break
		} else {
			showCursor()
			// Fill remaining screen with blank characters to erase
			n := termHeight - lines
			for i := 0; i < n; i++ {
				fmt.Println(spacesRow)
			}
			line := "openqa-mon (https://github.com/grisu48/openqa-mon)"
			date := time.Now().Format("15:04:05")
			fmt.Print(line + spaces(termWidth-len(line)-len(date)) + date)
			time.Sleep(time.Duration(continuous) * time.Second)
		}
	}

}
