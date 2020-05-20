package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
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

func bell() {
	// Use system bell
	fmt.Print("\a")
}

func (job *Job) stateString() string {
	if job.State == "done" {
		return job.Result
	} else {
		return job.State
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
			case "failed", "incomplete":
				fmt.Print(KRED)
			case "cancelled", "user_cancelled":
				fmt.Print(KMAG)
			case "passed":
				fmt.Print(KGRN)
			case "user_restarted", "parallel_restarted":
				fmt.Print(KBLU)
			case "softfailed":
				fmt.Print(KYEL)
			default:
				fmt.Print(KWHT)
			}
		} else if job.State == "cancelled" {
			fmt.Print(KMAG)
		} else {
			fmt.Print(KCYN)
		}
	}

	// Spacing rules:
	// |id 8 chars|2 spaces|name@machine[2spaces|link]|2 spaces|status 15 characteres

	// fixed characters: 8+2+2+18 = 30
	fixedCharacters := 30

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

	if len(status) < 18 {
		status = spaces(18-len(status)) + status
	}
	fmt.Printf("%8d  %s%s  %.18s\n", job.ID, name, link, status)

	// Reset color
	if useColors {
		fmt.Print(KNRM)
	}
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

func ensureHTTP(remote string) string {
	if !(strings.HasPrefix(remote, "http://") || strings.HasPrefix(remote, "https://")) {
		return "http://" + remote
	} else {
		return remote
	}
}

func homogenizeRemote(remote string) string {
	for len(remote) > 0 && strings.HasSuffix(remote, "/") {
		remote = remote[:len(remote)-1]
	}
	return remote
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
	defer resp.Body.Close()
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
	defer resp.Body.Close()
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
	fmt.Println("                             REMOTE can be the directlink to a test (e.g. https://openqa.opensuse.org/t123)\n")
	fmt.Println("OPTIONS\n")
	fmt.Println("  -h, --help                       Print this help message")
	fmt.Println("  -j, --jobs JOBS                  Display information only for the given JOBS")
	fmt.Println("                                   JOBS can be a single job id, a comma separated list (e.g. 42,43,1337)")
	fmt.Println("                                   or a job range (1335..1339)")
	fmt.Println("  -c,--continous SECONDS           Continously display stats")
	fmt.Println("  -b,--bell                        Bell notification on job status changes")
	fmt.Println("  -n,--notify                      Send desktop notifications on job status changes")
	fmt.Println("  -f,--follow                      Follow jobs, i.e. replace jobs by their clones if available")
	fmt.Println("")
	fmt.Println("2020, https://github.com/grisu48/openqa-mon")
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
	// Remove : at the end
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
	// Assume job ID set, which also covers single jobs IDs
	split := strings.Split(parseText, ",")
	for _, s := range split {
		i = parseJobID(s)
		if i > 0 {
			ret = append(ret, i)
		}
	}
	return ret
}

/** Try to match the url to be a test url. On success, return the remote and the job id */
func matchTestURL(url string) (bool, string, int) {
	r, _ := regexp.Compile("^http[s]?://.+/(t[0-9]+$|tests/[0-9]+$)")
	match := r.MatchString(url)
	if !match {
		return match, "", 0
	}
	// Parse
	rEnd, _ := regexp.Compile("/t[0-9]+$")
	loc := rEnd.FindStringIndex(url)
	if len(loc) == 2 {
		i := loc[0]
		job, err := strconv.Atoi(url[i+2:])
		if err != nil {
			return false, "", 0
		}
		return true, url[0:i], job
	} else {
		rEnd, _ = regexp.Compile("/tests/[0-9]+$")
		loc := rEnd.FindStringIndex(url)
		if len(loc) == 2 {
			i := loc[0]
			job, err := strconv.Atoi(url[i+7:])
			if err != nil {
				return false, "", 0
			}
			return true, url[0:i], job
		}
	}
	return false, "", 0
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

func unique(a []int) []int {
	keys := make(map[int]bool)
	list := []int{}
	for _, entry := range a {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func jobsMap(jobs []Job) map[int]Job {
	ret := make(map[int]Job, 0)
	for _, job := range jobs {
		ret[job.ID] = job
	}
	return ret
}

func containsJobID(jobs []Job, ID int) bool {
	for _, job := range jobs {
		if job.ID == ID {
			return true
		}
	}
	return false
}

func containsInt(a []int, cmp int) bool {
	for _, i := range a {
		if i == cmp {
			return true
		}
	}
	return false
}

/* jobsChanged returns the jobs that are in a different state between the two sets */
func jobsChanged(jobs1 []Job, jobs2 []Job) []Job {
	ret := make([]Job, 0)
	jobs := jobsMap(jobs2)
	for _, job := range jobs1 {
		if val, ok := jobs[job.ID]; ok {
			if job.Result != val.Result || job.State != val.State {
				ret = append(ret, job)
			}
		} else {
			ret = append(ret, job)
		}
	}
	if len(jobs1) != len(jobs2) {
		// Also account for jobs, which are not present in jobs1 set
		jobs := jobsMap(jobs1)
		for _, job := range jobs2 {
			if _, ok := jobs[job.ID]; !ok {
				ret = append(ret, job)
			}
		}
	}
	return ret
}

/* Assuming the input jobs are status change jobs, remove the trivial status changes like uploading */
func eraseTrivialChanges(jobs []Job) []Job {
	ret := make([]Job, 0)
	for _, job := range jobs {
		if job.State == "uploading" || job.State == "assigned" {
			continue
		} else {
			ret = append(ret, job)
		}
	}
	return ret
}

/** Append the given remote by adding a job id to the existing remote or creating a new one */
func appendRemote(remotes []Remote, remote string, jobID int) []Remote {
	remote = homogenizeRemote(remote)
	// Search for existing remote
	for i, k := range remotes {
		if k.URI == remote {
			if jobID > 0 {
				remotes[i].Jobs = append(remotes[i].Jobs, jobID)
			}
			return remotes
		}
	}
	// Not found, add new remote
	rem := Remote{URI: remote}
	rem.Jobs = make([]int, 0)
	if jobID > 0 {
		rem.Jobs = append(rem.Jobs, jobID)
	}
	return append(remotes, rem)
}

// Expand short arguments to long one
func expandArguments(args []string) []string {
	ret := make([]string, 0)

	for _, arg := range args {
		if arg == "" {
			continue
		}
		if len(arg) >= 2 && arg[0] == '-' && arg[1] != '-' {
			for _, c := range arg[1:] {
				switch c {
				case 'h':
					ret = append(ret, "--help")
				case 'c':
					ret = append(ret, "--continuous")
				case 'f':
					ret = append(ret, "--follow")
				case 'b':
					ret = append(ret, "--bell")
				case 'n':
					ret = append(ret, "--notify")
				case 'j':
					ret = append(ret, "--jobs")
				}
			}
		} else {
			ret = append(ret, arg)
		}
	}
	return ret
}

func main() {
	var err error
	args := expandArguments(os.Args[1:])
	remotes := make([]Remote, 0)
	continuous := 0              // If > 0, continously monitor
	bellNotification := false    // Notify about status changes, if in continuous monitor
	desktopNotification := false // Notify about job status changes via notify-send
	followJobs := false          // Replace jobs by their cloned ones

	// Manually parse program arguments, as the "flag" package is not sufficent for automatic parsing of job links and job numbers
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
				jobIDs := parseJobIDs(args[i])
				if len(jobIDs) > 0 {
					if len(remotes) == 0 {
						fmt.Fprintf(os.Stderr, "Jobs need to be defined after a remote instance\n")
						os.Exit(1)
					}
					remote := &remotes[len(remotes)-1]
					for _, jobID := range jobIDs {
						remote.Jobs = append(remote.Jobs, jobID)
						fmt.Println(jobID)
					}
				} else {
					fmt.Fprintf(os.Stderr, "Illegal job identifier: %s\n", args[i])
					os.Exit(1)
				}
			case "-c", "--continuous":
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
			case "-b", "--bell":
				bellNotification = true
			case "-n", "--notify":
				desktopNotification = true
			case "-f", "--follow":
				followJobs = true
			default:
				fmt.Fprintf(os.Stderr, "Invalid argument: %s\n", arg)
				fmt.Printf("Use %s --help to display available options\n", os.Args[0])
				os.Exit(1)
			}
		} else {
			// No argument, so it's either a job id, a job id range or a remote URI.
			// If it's a uri, skip the job id test
			if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
				// Try to parse as job run (e.g. http://phoenix-openqa.qam.suse.de/t1241)
				match, url, jobID := matchTestURL(arg)
				if match {
					remotes = appendRemote(remotes, url, jobID)
				} else {
					remotes = appendRemote(remotes, arg, 0)
				}
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
					fmt.Fprintf(os.Stderr, "Illegal input: %s. Input must be either a REMOTE (starting with http:// or https://) or a JOB identifier\n", arg)
					os.Exit(1)
				}
			}
		}
	}

	if len(remotes) == 0 {
		printHelp()
		return
	}
	for _, remote := range remotes {
		remote.Jobs = unique(remote.Jobs)
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

	jobsMemory := make([]Job, 0)
	for {
		termWidth, termHeight := terminalSize()
		// Ensure a certain minimum extend
		termWidth = max(termWidth, 50)
		termHeight = max(termHeight, 10)
		spacesRow := spaces(termWidth)
		useColors := true
		remotesString := fmt.Sprintf("%d remotes", len(remotes))
		if len(remotes) == 1 {
			remotesString = remotes[0].URI
		}
		if continuous > 0 {
			moveCursorBeginning()
			line := fmt.Sprintf("openqa-mon - Monitoring %s | Refresh every %d seconds", remotesString, continuous)
			fmt.Print(line + spaces(termWidth-len(line)))
			fmt.Println(spaces(termWidth))
		}
		lines := 2
		currentJobs := make([]Job, 0)
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
				jobsModified := false
				for i, id := range remote.Jobs {
				fetchJob:
					job, err := fetchJob(uri, id)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error fetching job %d: %s\n", id, err)
						continue
					}
					if followJobs && job.CloneID != 0 && job.CloneID != id {
						id = job.CloneID
						if containsJobID(jobs, id) || containsInt(remote.Jobs, id) {
							continue
						}
						remote.Jobs[i] = id
						jobsModified = true
						goto fetchJob
					}
					jobs = append(jobs, job)
				}
				if jobsModified {
					remote.Jobs = unique(remote.Jobs)
				}
			}
			// Sort jobs by ID
			sort.Sort(byID(jobs))
			// Print jobs
			for _, job := range jobs {
				if job.ID > 0 { // Otherwise it's an empty (.e. not found) job
					job.Println(useColors, termWidth)
					lines++
				}
			}
			lines++
			currentJobs = append(currentJobs, jobs...)
		}
		if continuous <= 0 {
			break
		} else {
			// Check if jobs have changes
			if bellNotification {
				if len(jobsMemory) == 0 {
					jobsMemory = currentJobs
				} else {
					changedJobs := jobsChanged(currentJobs, jobsMemory)
					changedJobs = eraseTrivialChanges(changedJobs)
					if len(changedJobs) > 0 {
						jobsMemory = currentJobs
						if bellNotification {
							bell()
						}
						if desktopNotification {
							if len(changedJobs) == 1 {
								job := changedJobs[0]
								notifySend(fmt.Sprintf("[%s] - Job %d %s", job.stateString(), job.ID, job.Name))
							} else if len(changedJobs) < 4 { // Up to 3 jobs are ok to display
								message := fmt.Sprintf("%d jobs changed state:", len(changedJobs))
								for _, job := range changedJobs {
									message += "\n  " + fmt.Sprintf("[%s] - Job %d %s", job.stateString(), job.ID, job.Name)
								}
								notifySend(message)
							} else { // For more job it doesn't make any sense anymore to display them
								notifySend(fmt.Sprintf("%d jobs changed state", len(changedJobs)))
							}
						}
					}
				}
			}

			// Fill remaining screen with blank characters to erase
			n := termHeight - lines
			for i := 0; i < n; i++ {
				fmt.Println(spacesRow)
			}
			line := "openqa-mon (https://github.com/grisu48/openqa-mon)"
			date := time.Now().Format("15:04:05")
			fmt.Print(line + spaces(termWidth-len(line)-len(date)) + date)
			time.Sleep(time.Duration(continuous) * time.Second)
			moveCursorLineBeginning(termHeight)
			fmt.Print(line + spaces(termWidth-len(line)-14) + "Refreshing ...")
		}
	}

}
