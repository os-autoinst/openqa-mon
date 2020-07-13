package main

import (
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Remote instance
type Remote struct {
	URI  string
	Jobs []int
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
	fmt.Println("  -p,--hierarchy                   Show job hierarchy (i.e. children jobs)")
	fmt.Println("  --config FILE                    Read additional config file FILE")
	fmt.Println("")
	fmt.Println("2020, https://github.com/grisu48/openqa-mon")
}

/** Try to match the url to be a test url. On success, return the remote and the job id */
func matchTestURL(url string) (bool, string, []int) {
	jobs := make([]int, 0)
	r, _ := regexp.Compile("^http[s]?://.+/(t[0-9]+$|t[0-9]+..[0-9]+$|tests/[0-9]+$|tests/[0-9]+..[0-9]$)")
	match := r.MatchString(url)
	if !match {
		return match, "", jobs
	}
	// Parse
	rEnd, _ := regexp.Compile("/(t[0-9]+$|t[0-9]+..[0-9]+$)")
	loc := rEnd.FindStringIndex(url)
	if len(loc) == 2 {
		i := loc[0]
		jobs = parseJobIDs(url[i+2:])
		return true, url[0:i], jobs
	} else {
		rEnd, _ = regexp.Compile("/tests/([0-9]+$|[0-9]+..[0-9]+)")
		loc := rEnd.FindStringIndex(url)
		if len(loc) == 2 {
			i := loc[0]
			jobs = parseJobIDs(url[i+7:])
			return true, url[0:i], jobs
		}
	}
	return false, "", jobs
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

func containsInt(a []int, cmp int) bool {
	for _, i := range a {
		if i == cmp {
			return true
		}
	}
	return false
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
				case 'p':
					ret = append(ret, "--hierarchy")
				}
			}
		} else {
			ret = append(ret, arg)
		}
	}
	return ret
}

func printJobHierarchy(job Job, useColors bool, termWidth int) ([]Job, error) {
	jobs := make([]Job, 0)

	for _, id := range job.Children.Chained {
		cJob, err := fetchJob(job.Remote, id)
		if err != nil {
			return jobs, err
		}
		cJob.Prefix = "  [C]"
		cJob.Println(useColors, termWidth)
		jobs = append(jobs, cJob)
	}
	for _, id := range job.Children.DirectlyChained {
		cJob, err := fetchJob(job.Remote, id)
		if err != nil {
			return jobs, err
		}
		cJob.Prefix = "  [D]"
		cJob.Println(useColors, termWidth)
		jobs = append(jobs, cJob)
	}
	for _, id := range job.Children.Parallel {
		cJob, err := fetchJob(job.Remote, id)
		if err != nil {
			return jobs, err
		}
		cJob.Prefix = "  [P]"
		cJob.Println(useColors, termWidth)
		jobs = append(jobs, cJob)
	}
	return jobs, nil
}

func homeDir() string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return usr.HomeDir
}

func main() {
	var err error
	var config Config
	args := expandArguments(os.Args[1:])
	remotes := make([]Remote, 0)
	// Configuration - apply default values and read config files: Global '/etc/openqa/openqa-mon.conf' and user '~/openqa-mon.conf'
	config.Continuous = 0
	config.Notify = false
	config.Bell = false
	config.Follow = false
	config.Hierarchy = false
	// readConfig returns nil also if the file does not exists
	err = readConfig("/etc/openqa/openqa-mon.conf", &config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config '/etc/openqa/openqa-mon.conf': %s\n", err)
		os.Exit(1)
	}
	err = readConfig(homeDir()+"/.openqa-mon.conf", &config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config '"+homeDir()+"/.openqa-mon.conf': %s\n", err)
		os.Exit(1)
	}

	// Manually parse program arguments, as the "flag" package is not sufficent for automatic parsing of job links and job numbers
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if arg[0] == '-' {
			switch arg {
			case "--help":
				printHelp()
				return
			case "--jobs":
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
			case "--continuous":
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "Missing continous period")
					os.Exit(1)
				}
				config.Continuous, err = strconv.Atoi(args[i])
				if err != nil || config.Continuous < 0 {
					fmt.Fprintln(os.Stderr, "Invalid continous period")
					fmt.Println("Continous duration needs to be a positive, non-zero integer that determines the seconds between refreshes")
					os.Exit(1)
				}
			case "--bell":
				config.Bell = true
			case "--notify":
				config.Notify = true
			case "--follow":
				config.Follow = true
			case "--hierarchy":
				config.Hierarchy = true
			case "--config":
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "Missing config file")
					os.Exit(1)
				}
				err = readConfig(args[i], &config)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading config '%s': %s\n", args[i], err)
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
				// Try to parse as job run (e.g. http://phoenix-openqa.qam.suse.de/t1241)
				match, url, jobIDs := matchTestURL(arg)
				if match {
					for _, jobID := range jobIDs {
						remotes = appendRemote(remotes, url, jobID)
					}
				} else {
					remotes = appendRemote(remotes, arg, 0)
				}
			} else {
				// If the argument is a number only, assume it's a job ID otherwise it's a host
				jobIDs := parseJobIDs(arg)
				if len(jobIDs) > 0 {
					if len(remotes) == 0 {
						// Apply default remote, if defined
						if config.DefaultRemote == "" {
							fmt.Fprintf(os.Stderr, "Jobs need to be defined after a remote instance\n")
							os.Exit(1)
						}
						remote := Remote{URI: config.DefaultRemote}
						remotes = append(remotes, remote)
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
		// Apply default remote, if defined
		if config.DefaultRemote == "" {
			printHelp()
			return
		}
		remote := Remote{URI: config.DefaultRemote}
		remotes = append(remotes, remote)

	}
	for _, remote := range remotes {
		remote.Jobs = unique(remote.Jobs)
	}

	if config.Continuous > 0 {
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
		if config.Continuous > 0 {
			moveCursorBeginning()
			line := fmt.Sprintf("openqa-mon - Monitoring %s | Refresh every %d seconds", remotesString, config.Continuous)
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
					job.Remote = remote.URI
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error fetching job %d: %s\n", id, err)
						continue
					}
					if config.Follow && job.CloneID != 0 && job.CloneID != id {
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
				if job.ID <= 0 { // Job not found
					continue
				}
				job.Println(useColors, termWidth)
				lines++
				if config.Hierarchy {
					// Print children as well. We do this here, so to keep the hierarchy
					nn, err := printJobHierarchy(job, useColors, termWidth)
					if err != nil {
						// XXX: For now we swallow the error
					}
					lines += len(nn)
				}
			}
			lines++
			currentJobs = append(currentJobs, jobs...)
		}
		if config.Continuous <= 0 {
			break
		} else {
			// Check if jobs have changes
			if config.Bell || config.Notify {
				if len(jobsMemory) == 0 {
					jobsMemory = currentJobs
				} else {
					changedJobs := jobsChanged(currentJobs, jobsMemory)
					changedJobs = eraseTrivialChanges(changedJobs)
					if len(changedJobs) > 0 {
						jobsMemory = currentJobs
						if config.Bell {
							bell()
						}
						if config.Notify {
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
			time.Sleep(time.Duration(config.Continuous) * time.Second)
			moveCursorLineBeginning(termHeight)
			fmt.Print(line + spaces(termWidth-len(line)-14) + "Refreshing ...")
		}
	}

}
