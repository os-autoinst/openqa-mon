/* openqa-mon is a simple CLI utility for active monitoring of openQA jobs */
package main

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/grisu48/gopenqa"
)

// Remote instance
type Remote struct {
	URI  string
	Jobs []int
}

func printHelp() {
	fmt.Printf("Usage: %s [OPTIONS] REMOTE\n  REMOTE is the base URL of the openQA server (e.g. https://openqa.opensuse.org)\n\n", os.Args[0])
	fmt.Println("                             REMOTE can be the directlink to a test (e.g. https://openqa.opensuse.org/t123)")
	fmt.Println("                             or a job range (e.g. https://openqa.opensuse.org/t123..125 or https://openqa.opensuse.org/t123+2)")
	fmt.Println("")
	fmt.Println("OPTIONS")
	fmt.Println("")
	fmt.Println("  -h, --help                       Print this help message")
	fmt.Println("  -j, --jobs JOBS                  Display information only for the given JOBS")
	fmt.Println("                                   JOBS can be a single job id, a comma separated list (e.g. 42,43,1337)")
	fmt.Println("                                   or a job range (1335..1339 or 1335+4)")
	fmt.Println("  -c,--continous SECONDS           Continously display stats")
	fmt.Println("  -e,--exit                        Exit openqa-mon when all jobs are done (only in continuous mode)")
	fmt.Println("                                   Return code is 0 if all jobs are passed or softfailing, 1 otherwise.")
	fmt.Println("  -b,--bell                        Bell notification on job status changes")
	fmt.Println("  -n,--notify                      Send desktop notifications on job status changes")
	fmt.Println("  --no-bell                        Disable bell notification")
	fmt.Println("  --no-notify                      Disable desktop notifications")
	fmt.Println("  -m,--monitor                     Enable bell and desktop notifications")
	fmt.Println("  -s,--silent                      Disable bell and desktop notifications")
	fmt.Println("")
	fmt.Println("  -f,--follow                      Follow jobs, i.e. replace jobs by their clones if available")
	fmt.Println("  -p,--hierarchy                   Show job hierarchy (i.e. children jobs)")
	fmt.Println("  --hide-state STATES              Hide jobs with that are in the given state (e.g. 'running,assigned')")
	fmt.Println("")
	fmt.Println("  --config FILE                    Read additional config file FILE")
	fmt.Println("")
	fmt.Println("2021, https://github.com/grisu48/openqa-mon")
}

/** Try to match the url to be a test url. On success, return the remote and the job id */
func matchTestURL(url string) (bool, string, []int) {
	jobs := make([]int, 0)
	// TODO: Add fragment into regex
	r, _ := regexp.Compile("^http[s]?://.+/(t[0-9]+$|t[0-9]+..[0-9]+$|tests/[0-9]+$|tests/[0-9]+..[0-9]+$)")
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

/* checks if all given jobs are done */
func jobsDone(jobs []gopenqa.Job) bool {
	for _, job := range jobs {
		if job.State != "done" && job.State != "cancelled" {
			return false
		}
	}
	return true
}

/* checks if the set of jobs contains failed jobs */
func getFailedJobs(jobs []gopenqa.Job) []gopenqa.Job {
	ret := make([]gopenqa.Job, 0)
	for _, job := range jobs {
		// We only consider completed jobs
		if job.State == "cancelled" {
			ret = append(ret, job)
		} else if job.State != "done" {
			// Assume a job is failed, if it is not passed or softfailed
			if job.Result != "passed" && job.Result != "softfail" {
				ret = append(ret, job)
			}
		}
	}
	return ret
}

/** Check if the given job should not be displayed */
func hideJob(job gopenqa.Job, config Config) bool {
	for _, s := range config.HideStates {
		s = trimLower(s)
		if trimLower(job.State) == s || trimLower(job.Result) == s {
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

	for i, arg := range args {
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
					// The next argument will be the number of seconds, add them here
					ret = append(ret, args[i+1])
					args[i+1] = ""
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
				case 'm':
					ret = append(ret, "--monitor")
				case 's':
					ret = append(ret, "--silent")
				case 'e':
					ret = append(ret, "--exit")
				}
			}
		} else {
			ret = append(ret, arg)
		}
	}
	return ret
}

func jobsContainId(jobs []gopenqa.Job, id int) bool {
	for _, job := range jobs {
		if job.ID == id {
			return true
		}
	}
	return false
}

func getJobHierarchy(job gopenqa.Job, follow bool) ([]gopenqa.Job, error) {
	jobs := make([]gopenqa.Job, 0)
	// TODO: The prefix got missing ...
	chained, err := job.FetchChildren(unique(job.Children.Chained), follow)
	if err != nil {
		return jobs, err
	}
	jobs = append(jobs, chained...)
	directlyChained, err := job.FetchChildren(unique(job.Children.DirectlyChained), follow)
	if err != nil {
		return jobs, err
	}
	jobs = append(jobs, directlyChained...)
	parallel, err := job.FetchChildren(unique(job.Children.Parallel), follow)
	if err != nil {
		return jobs, err
	}
	jobs = append(jobs, parallel...)

	return jobs, nil
}

var config Config
var tui TUI

func main() {
	var err error
	args := expandArguments(os.Args[1:])
	remotes := make([]Remote, 0)
	// Configuration - apply default values and read config files: Global '/etc/openqa/openqa-mon.conf' and user '~/openqa-mon.conf'
	config.Continuous = 0
	config.Notify = false
	config.Bell = false
	config.Follow = false
	config.Hierarchy = false
	config.HideStates = make([]string, 0)
	config.Quit = false
	// readConfig ignores a nonexisting file and returns nil
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
			case "--no-bell":
				config.Bell = false
			case "--no-notify":
				config.Notify = false
			case "--silent":
				config.Bell = false
				config.Notify = false
			case "--monitor":
				config.Bell = true
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
			case "--hide-state", "--hide-job-state", "--hide":
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "Missing job state")
					os.Exit(1)
				}
				states := trimSplit(args[i], ",")
				config.HideStates = append(config.HideStates, states...)
			case "--quit", "--exit":
				config.Quit = true
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
				match, url, jobIDs := matchTestURL(removeFragment(arg))
				if match {
					for _, jobID := range jobIDs {
						remotes = appendRemote(remotes, url, jobID)
					}
				} else {
					remotes = appendRemote(remotes, arg, 0)
				}
			} else {
				// If the argument is a number only, assume it's a job ID otherwise it's a host
				jobIDs := parseJobIDs(removeFragment(arg))
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
	// Manually parse program arguments, as the "flag" package is not sufficent for automatic parsing of job links and job numbers
	parseArgs(args, &remotes)
	// Remove duplicate IDs and sort by ID
	for _, remote := range remotes {
		remote.Jobs = unique(remote.Jobs)
		sort.Slice(remote.Jobs, func(i, j int) bool {
			return remote.Jobs[i] < remote.Jobs[j]
		})
	}

	// Single listing mode, no TUI
	if config.Continuous <= 0 {
		singleCall(remotes)
		os.Exit(0)
	}
	tui = CreateTUI()
	tui.EnterAltScreen()
	tui.Clear()
	remotesString := fmt.Sprintf("%d remotes", len(remotes))
	if len(remotes) == 1 {
		remotesString = remotes[0].URI
	}
	tui.SetHeader(fmt.Sprintf("openqa-mon - Monitoring %s | Refreshing every %d seconds", remotesString, config.Continuous))
	tui.Model.HideStates = config.HideStates
	tui.Update()
	defer tui.LeaveAltScreen()
	continuousMonitoring(remotes)
	os.Exit(0)
}

func parseArgs(args []string, remotes *[]Remote) {
	var err error
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
				jobIDs := parseJobIDs(args[i])
				if len(jobIDs) > 0 {
					remote := &(*remotes)[len((*remotes))-1]
					for _, jobID := range jobIDs {
						remote.Jobs = append(remote.Jobs, jobID)
						fmt.Println(jobID)
					}
				} else {
					fmt.Fprintf(os.Stderr, "Illegal job identifier: %s\n", arg)
					os.Exit(1)
				}
			case "--continuous":
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "Missing continous period")
					os.Exit(1)
				}
				config.Continuous, err = strconv.Atoi(arg)
				if err != nil || config.Continuous < 0 {
					fmt.Fprintln(os.Stderr, "Invalid continous period")
					fmt.Println("Continous duration needs to be a positive, non-zero integer that determines the econds between refreshes")
					os.Exit(1)
				}
			case "--bell":
				config.Bell = true
			case "--notify":
				config.Notify = true
			case "--no-bell":
				config.Bell = false
			case "--no-notify":
				config.Notify = false
			case "--silent":
				config.Bell = false
				config.Notify = false
			case "--monitor":
				config.Bell = true
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
			case "--hide-state", "--hide-job-state", "--hide":
				i++
				if i >= len(args) {
					fmt.Fprintln(os.Stderr, "Missing job state")
					os.Exit(1)
				}
				states := trimSplit(args[i], ",")
				config.HideStates = append(config.HideStates, states...)
			case "--quit", "--exit":
				config.Quit = true
			default:
				fmt.Fprintf(os.Stderr, "Invalid argument: %s\n", args[i])
				fmt.Printf("Use %s --help to display available options\n", os.Args[0])
				os.Exit(1)
			}
		} else {
			// No argument, so it's either a job id, a job id range or a remote URI.
			// If it's a uri, skip the job id test
			if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
				// Try to parse as job run (e.g. http://phoenix-openqa.qam.suse.de/t1241)
				match, url, jobIDs := matchTestURL(removeFragment(arg))
				if match {
					for _, jobID := range jobIDs {
						(*remotes) = appendRemote((*remotes), url, jobID)
					}
				} else {
					(*remotes) = appendRemote((*remotes), arg, 0)
				}
			} else {
				// If the argument is a number only, assume it's a job ID otherwise it's a host
				jobIDs := parseJobIDs(removeFragment(arg))
				if len(jobIDs) > 0 {
					if len((*remotes)) == 0 {
						// Apply default remote, if defined
						if config.DefaultRemote == "" {
							fmt.Fprintf(os.Stderr, "Jobs need to be defined after a remote instance\n")
							os.Exit(1)
						}
						remote := Remote{URI: config.DefaultRemote}
						(*remotes) = append((*remotes), remote)
					}
					remote := &(*remotes)[len((*remotes))-1]
					for _, jobID := range jobIDs {
						remote.Jobs = append(remote.Jobs, jobID)
					}
				} else {
					fmt.Fprintf(os.Stderr, "Illegal input: %s. Input must be either a REMOTE (starting wih http:// or https://) or a JOB identifier\n", arg)
					os.Exit(1)
				}
			}
		}
	}
}

/* Get all jobs from the given remotes
 * callback will be called for each received job
 * returns the (possibly modified) input remotes
 */
func FetchJobs(remotes []Remote, callback func(int, gopenqa.Job)) ([]Remote, error) {
	for i, remote := range remotes {
		instance := gopenqa.CreateInstance(ensureHTTP(remote.URI))
		// If no jobs are defined, fetch overview
		if len(remote.Jobs) == 0 {
			overview, err := instance.GetOverview("", gopenqa.EmptyParams())
			if err != nil {
				return remotes, err
			}
			for _, job := range overview {
				callback(job.ID, job)
			}
		} else {
			// Fetch individual jobs
			jobsModified := false // If remote.Jobs has been modified (e.g. id changes when detecting a restarted job)
			for i, id := range remote.Jobs {
				var job gopenqa.Job
				var err error
				if config.Follow {
					job, err = instance.GetJobFollow(id)
				} else {
					job, err = instance.GetJob(id)
				}
				if err != nil {
					return remotes, err
				}
				if job.ID != id {
					remote.Jobs[i] = job.ID
					jobsModified = true
				}
				callback(id, job)

				// Fetch children
				if config.Hierarchy {
					// Depending on the child type, add prefix
					if children, err := job.FetchChildren(job.Children.DirectlyChained, true); err != nil {
						return remotes, err
					} else {
						for _, job := range children {
							job.Prefix = "  +"
							callback(job.ID, job)
						}
					}
					if children, err := job.FetchChildren(job.Children.Chained, true); err != nil {
						return remotes, err
					} else {
						for _, job := range children {
							job.Prefix = "  ."
							callback(job.ID, job)
						}
					}
					if children, err := job.FetchChildren(job.Children.Parallel, true); err != nil {
						return remotes, err
					} else {
						for _, job := range children {
							job.Prefix = "  +"
							callback(job.ID, job)
						}
					}
				}
			}
			if jobsModified {
				// Ensure the job IDs are unique and sorted
				jobs := unique(remote.Jobs)
				sort.Slice(jobs, func(i, j int) bool {
					return jobs[i] < jobs[j]
				})
				remotes[i].Jobs = jobs
			}
		}
	}
	return remotes, nil
}

// Fires a job notification, if notifications are enabled
func NotifyJobChanged(j gopenqa.Job) {
	if config.Bell {
		bell()
	}
	if config.Notify {
		notifySend(fmt.Sprintf("[%s] - Job %d %s", j.JobState(), j.ID, j.Name))
	}
}

// Single call - Run without terminal user interface, just list the received jobs and quit
func singleCall(remotes []Remote) {
	width, _ := terminalSize()
	// If not a tty, disable color
	color := IsTTY()

	// Fetch jobs and list them
	_, err := FetchJobs(remotes, func(id int, job gopenqa.Job) {
		PrintJob(job, color, width)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching jobs: %s\n", err)
		os.Exit(1)
	}
}

func continuousMonitoring(remotes []Remote) {
	var err error
	// Ensure cursor is visible after termination
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigs
		tui.LeaveAltScreen()
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			os.Exit(1)
		}
	}()

	// Signal for a refresh
	refreshSignal := make(chan int, 1)

	// Keybress callback
	tui.Keypress = func(b byte) {
		if b == 'q' {
			tui.LeaveAltScreen()
			os.Exit(0)
		} else if b == 'r' {
			// Refresh
			refreshSignal <- 1
		} else if b == '?' {
			tui.SetShowHelp(!tui.DoShowHelp())
		} else if b == 'h' {
			tui.SetHideStates(!tui.DoHideStates())
		} else {
			// Ignore keypress and update tui
			tui.Update()
		}
	}

	// Start TUI handlers (keypress, ecc)
	tui.Start()

	// Current state of the jobs
	jobs := make([]gopenqa.Job, 0)

	tui.SetStatus("Initial job fetching ... ")
	for {
		exists := make(map[int]bool, 0) // Keep track of existing jobs
		// Fetch new jobs. Update remotes (job id's) when necessary
		remotes, err = FetchJobs(remotes, func(id int, job gopenqa.Job) {
			exists[job.ID] = true
			// Job received. Update existing job or add job if not yet present
			for i, j := range jobs {
				if j.ID == id { // Compare to given id as this is the original id (not the ID of a possible cloned job)
					jobs[i] = job
					// Ignore if job status remains the same
					if j.JobState() == job.JobState() {
						return
					}
					// Ignore trivial changes (uploading, assigned)
					state := job.JobState()
					if state == "uploading" || state == "assigned" {
						return
					}
					// Notify about job update
					NotifyJobChanged(job)
					// Refresh tui after each job update
					tui.Model.SetJobs(jobs)
					tui.Update()
					return
				}
			}

			// Append new job
			jobs = append(jobs, job)
			tui.Model.SetJobs(jobs)
			tui.Update()
		})
		// Remove items which are not present anymore - (e.g. old children)
		jobs = uniqueJobs(filterJobs(jobs, func(job gopenqa.Job) bool {
			_, ok := exists[job.ID]
			return ok
		}))
		tui.Model.SetJobs(jobs)
		if err != nil {
			tui.SetStatus(fmt.Sprintf("Error fetching jobs: %s", err))
		} else {
			tui.SetStatus("")
		}
		tui.Update()
		// Terminate if all jobs are done
		if config.Quit && jobsDone(jobs) {
			tui.LeaveAltScreen()
			failed := getFailedJobs(jobs)
			if len(failed) > 0 {
				fmt.Fprintf(os.Stderr, "%d job(s) completed with errors\n", len(failed))
				for _, job := range failed {
					fmt.Fprintf(os.Stderr, "%s\n", job.String())
				}
				os.Exit(1)
			} else {
				os.Exit(0)
			}
		}

		// Wait for next update, or timeout or signal
		select {
		case <-refreshSignal:
			tui.SetStatus("Manual refresh ... ")
		case <-time.After(time.Duration(config.Continuous) * time.Second):
			tui.SetStatus("Refreshing ... ")
		}

	}

}
