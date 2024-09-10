/* openqa-mon is a simple CLI utility for active monitoring of openQA jobs */
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/os-autoinst/gopenqa"
	"github.com/os-autoinst/openqa-mon/internal"
)

var config Config
var tui TUI

// Remote instance
type Remote struct {
	URI  string
	Jobs []int64
}

var remotes []Remote

func printHelp() {
	fmt.Printf("Usage: %s [OPTIONS] REMOTE\n", os.Args[0])
	fmt.Println("  REMOTE can be the directlink to a test (e.g. https://openqa.opensuse.org/t123)")
	fmt.Println("  or a job range (e.g. https://openqa.opensuse.org/t123..125 or https://openqa.opensuse.org/t123+2)")
	fmt.Println("")
	fmt.Println("OPTIONS")
	fmt.Println("")
	fmt.Println("  -h, --help                       Print this help message")
	fmt.Println("  --version                        Display program version")
	fmt.Println("  -j, --jobs JOBS                  Display information only for the given JOBS")
	fmt.Println("                                   JOBS can be a single job id, a comma separated list (e.g. 42,43,1337)")
	fmt.Println("                                   or a job range (1335..1339 or 1335+4)")
	fmt.Println("  -c,--continuous SECONDS          Continuously display stats, use rabbitmq if available otherwise status pulling")
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
	fmt.Println("  --no-follow                      Don't follow jobs")
	fmt.Println("  --rabbitmq                       Explicitly enable rabbitmq (experimental!!)")
	fmt.Println("  --rabbit FILE                    Explicitly enable rabbitmq and load configurations from FILE")
	fmt.Println("  --no-rabbit                      Don't use RabbitMQ, even if available")
	fmt.Println("  -p,--hierarchy                   Show job hierarchy (i.e. children jobs)")
	fmt.Println("  --hide-state STATES              Hide jobs with that are in the given state (e.g. 'running,assigned')")
	fmt.Println("")
	fmt.Println("  --config FILE                    Read additional config file FILE")
	fmt.Println("  -i, --input FILE                 Read jobs from FILE (additionally to stdin)")
	fmt.Println("")
	fmt.Println("2024, https://github.com/os-autoinst/openqa-mon")
}

/** Try to match the url to be a test url. On success, return the remote and the job id */
func matchTestURL(url string) (bool, string, []int64) {
	jobs := make([]int64, 0)
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

/* checks if the set of jobs contains failed jobs. This includes incomplete jobs */
func getFailedJobs(jobs []gopenqa.Job) []gopenqa.Job {
	ret := make([]gopenqa.Job, 0)
	for _, job := range jobs {
		// We only consider completed jobs
		if job.State == "cancelled" {
			ret = append(ret, job)
		} else if job.State == "done" {
			// Assume a job is failed, if it is not passed or softfailed
			if job.Result != "passed" && job.Result != "softfail" {
				ret = append(ret, job)
			}
			// Note: incomplete jobs also have state "done". They are also considered failed
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
func appendRemote(remotes []Remote, remote string, jobID int64) []Remote {
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
	rem.Jobs = make([]int64, 0)
	if jobID > 0 {
		rem.Jobs = append(rem.Jobs, jobID)
	}
	return append(remotes, rem)
}

// Expand short arguments
func expandArguments(args []string) ([]string, error) {
	ret := make([]string, 0)

	for _, arg := range args {
		if arg == "" {
			continue
		}
		if len(arg) >= 2 && arg[0] == '-' && arg[1] != '-' {
			for i := 1; i < len(arg); i++ {
				c := arg[i]
				switch c {
				case 'h':
					ret = append(ret, "--help")
				case 'c':
					ret = append(ret, "--continuous")
					// We expect a number after -c, e.g. "-c10" for 10 seconds. Extract the number
					if i < len(arg)-1 {
						number := ""
						// Note: Iterating over a string in go takes unicode characters into account.
						var pos int
						var r rune
						for pos, r = range arg[i+1:] {
							if unicode.IsDigit(r) {
								number += string(r)
							} else {
								break
							}
						}
						i += pos
						if number, err := strconv.Atoi(number); err != nil {
							return ret, fmt.Errorf("invalid number for continuous monitoring")
						} else {
							// Append the extracted number right after the --continuous argument
							ret = append(ret, fmt.Sprintf("%d", number))
						}
					}
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
				case 'i':
					ret = append(ret, "--input")
				}
			}
		} else {
			ret = append(ret, arg)
		}
	}
	return ret, nil
}

func jobsContainId(jobs []gopenqa.Job, id int64) bool {
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
	chained, err := job.FetchChildren(unique64(job.Children.Chained), follow)
	if err != nil {
		return jobs, err
	}
	jobs = append(jobs, chained...)
	directlyChained, err := job.FetchChildren(unique64(job.Children.DirectlyChained), follow)
	if err != nil {
		return jobs, err
	}
	jobs = append(jobs, directlyChained...)
	parallel, err := job.FetchChildren(unique64(job.Children.Parallel), follow)
	if err != nil {
		return jobs, err
	}
	jobs = append(jobs, parallel...)

	return jobs, nil
}

/** Try to update the job with the given status, if present. Returns the found job and true if the job was present */
func updateJobStatus(status gopenqa.JobStatus, openqaURI string) (gopenqa.Job, bool) {
	if len(tui.Model.jobs) == 0 {
		return gopenqa.Job{ID: 0}, false
	}
	for i, j := range tui.Model.jobs {
		if j.ID == status.ID && j.Remote == openqaURI {
			tui.Model.jobs[i].State = "done"
			tui.Model.jobs[i].Result = fmt.Sprintf("%s", status.Result)
			return tui.Model.jobs[i], true
		}
	}
	return gopenqa.Job{ID: 0}, false
}

/** Search for the given job and fetch it from the openQA instance. Returns the new job and a boolean indicating if the job has been found */
func updateJob(job int64, openqaURI string) (gopenqa.Job, bool) {
	jobs := tui.Model.jobs
	if len(jobs) == 0 {
		return gopenqa.Job{ID: 0}, false
	}
	for i, j := range jobs {
		if j.ID == job && j.Remote == openqaURI {
			instance := gopenqa.CreateInstance(ensureHTTP(openqaURI))
			instance.SetUserAgent("openqa-mon")
			instance.SetMaxRecursionDepth(100) // Certain jobs (e.g. verification runs) can have a lot of clones
			if job, err := instance.GetJobFollow(job); err == nil {
				jobs[i] = job
				tui.Model.SetJobs(jobs)
				return job, true
			}
			return jobs[i], true
		}
	}
	return gopenqa.Job{ID: 0}, false
}

func assembleRabbitMQRemote(remote, username, password string) string {
	// Extract protocol, if present
	protocol := "amqps://"
	if strings.Contains(remote, "://") {
		i := strings.Index(remote, "://")
		protocol = remote[:i]
		remote = remote[i+3:]
	}
	return fmt.Sprintf("%s://%s:%s@%s", protocol, username, password, remote)
}

// Read the existing rabbitMQ configuration files and assemble them into a map
func ReadRabbitMQs() (map[string]RabbitConfig, error) {
	ret := make(map[string]RabbitConfig)

	// Follow the following precedence in reading the configuration files:
	// 1. Read the system-wide config
	// 2. Read user config
	// 3. Read optionally provided additional files

	// 1. First the the system configuration (if present)
	rabbits, err := ReadRabbitMQ("/etc/openqa/openqamon-rabbitmq.conf")
	if err != nil {
		return ret, err
	}
	for _, rabbit := range rabbits {
		ret[rabbit.Hostname] = rabbit
	}

	// 2. Read the user config (if present)
	rabbits, err = ReadRabbitMQ(homeDir() + "/.config/openqa/openqamon-rabbitmq.conf")
	if err != nil {
		return ret, err
	}
	for _, rabbit := range rabbits {
		ret[rabbit.Hostname] = rabbit
	}

	// 3. Read user provided files, if present
	for _, filename := range config.RabbitMQFiles {
		rabbits, err = ReadRabbitMQ(filename)
		if err != nil {
			return ret, err
		}
		for _, rabbit := range rabbits {
			ret[rabbit.Hostname] = rabbit
		}
	}

	return ret, nil
}

// Extract the hostname for a given uri
func getHostname(uri string) string {
	hostname := uri
	// First trim the protocol (if present)
	i := strings.Index(hostname, "://")
	if i > 0 {
		hostname = hostname[i+3:]
	}
	// Trim the path (if present)
	i = strings.Index(hostname, "/")
	if i > 0 {
		hostname = hostname[:i]
	}
	return hostname
}

// Register the given rabbitMQ instance for the tui
func registerRabbitMQ(tui *TUI, openqaURI string, remote string, topics []string) (gopenqa.RabbitMQ, error) {
	rmq, err := gopenqa.ConnectRabbitMQ(remote)
	if err != nil {
		return rmq, fmt.Errorf("RabbitMQ connection error: %s", err)
	}

	recvFunction := func(rmq *gopenqa.RabbitMQ) {
		connectedFlag := make(chan int)
		reconnects := 0

		// Loop until closed
		for !rmq.Closed() {
			// Subscribe to all topics in their own goroutine.
			// subscriptions notify us via the connectedFlag channel about error events.
			for _, topic := range topics {
				go func(topic string) {
					sub, err := rmq.Subscribe(topic)
					if err != nil {
						tui.SetStatus(fmt.Sprintf("RabbitMQ subscribe error: %s", err))
						connectedFlag <- 0 // Notify that something's off
						return
					}

					for {
						if status, err := sub.ReceiveJobStatus(); err != nil {
							// Receive failed
							tui.SetStatus(fmt.Sprintf("rabbitmq recv error: %s", err))
							connectedFlag <- 0
							return
							// Ignore empty updates (status.ID == 0)
						} else if status.ID != 0 {
							if status.Type == "job.done" {
								tui.SetStatus(fmt.Sprintf("Job %d - %s", status.ID, status.Result))
								// Update job, if present
								if job, found := updateJobStatus(status, openqaURI); found {
									tui.Update()
									if config.Notify {
										jobs := make([]gopenqa.Job, 0)
										jobs = append(jobs, job)
										NotifyJobsChanged(jobs)
									}
								}
							} else if status.Type == "job.restarted" {
								// Update the job that is being restarted

								if job, found := updateJob(status.ID, openqaURI); found {
									tui.Update()
									if config.Notify {
										jobs := make([]gopenqa.Job, 0)
										jobs = append(jobs, job)
										NotifyJobsChanged(jobs)
									}
								}
							} else {
								// Unknown job status
								tui.SetStatus(fmt.Sprintf("job %d: %s", status.ID, status.Type))
							}
						}
					}
				}(topic)
			}

			// Wait for someone to notify us about a broken channel
			if reconnects == 0 {
				tui.SetStatus("RabbitMQ mode")
			} else if reconnects == 1 {
				tui.SetStatus("RabbitMQ mode (reconnected)")
			} else {
				tui.SetStatus(fmt.Sprintf("RabbitMQ mode (%dx reconnected)", reconnects))
			}
			<-connectedFlag
			rmq.Close() // Close for everyone and wait a bit before reconnecting
			reconnects++
			tui.SetStatus(fmt.Sprintf("RabbitMQ reconnecting %d ...", reconnects))
			time.Sleep(time.Duration(2) * time.Second)
			// Consume remaining signals
			consuming := true
			for consuming {
				select {
				case <-connectedFlag:
					consuming = true
				default:
					consuming = false
				}
			}
			rmq.Reconnect()
			tui.SetStatus(fmt.Sprintf("RabbitMQ reconnecting %d ...", reconnects))
		}
	}
	go recvFunction(&rmq)
	return rmq, nil
}

func readJobs(filename string) ([]Remote, error) {
	remotes := make([]Remote, 0)

	fIn, err := os.OpenFile(filename, os.O_RDONLY, 0400)
	if err != nil {
		return remotes, err
	}
	defer fIn.Close()
	scanner := bufio.NewScanner(fIn)
	iLine := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		iLine++
		// Ignore empty lines or comments
		if line == "" || line[0] == '#' {
			continue
		}

		// Only accept URLs here
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			// Try to parse as job run (e.g. http://phoenix-openqa.qam.suse.de/t1241)
			match, url, jobIDs := matchTestURL(removeFragment(line))
			if match {
				for _, jobID := range jobIDs {
					remotes = appendRemote(remotes, url, jobID)
				}
			} else {
				remotes = appendRemote(remotes, line, 0)
			}
		} else {
			return remotes, fmt.Errorf("invalid job link (line %d)", iLine)
		}
	}

	return remotes, scanner.Err()
}

func SetStatus() {
	if config.Paused {
		if config.RabbitMQ {
			tui.SetStatus("RabbitMQ mode")
		} else {
			tui.SetStatus("Paused")
		}
		return
	}
	if config.Continuous > 0 {
		status := fmt.Sprintf("(continuous monitoring) | %d seconds |", config.Continuous)
		if config.Bell || config.Notify {
			status += " ("
			if config.Bell {
				status += "b"
			}
			if config.Notify {
				status += "n"
			}
			status += ")"
		}
		tui.SetStatus(status)
	} else {
		tui.SetStatus("")
	}
	tui.UpdateHeader()
}

func parseProgramArguments(cliargs []string) error {
	args, err := expandArguments(cliargs[1:])
	if err != nil {
		return err
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
				os.Exit(0)
			case "--version":
				fmt.Println("openqa-mon version " + internal.VERSION)
				os.Exit(0)
			case "--jobs":
				i++
				if i >= len(args) {
					return fmt.Errorf("missing job IDs")
				}
				if len(remotes) == 0 {
					return fmt.Errorf("jobs need to be defined after a remote instance")
				}
				jobIDs := parseJobIDs(args[i])
				if len(jobIDs) > 0 {
					if len(remotes) == 0 {
						return fmt.Errorf("jobs need to be defined after a remote instance")
					}
					remote := &remotes[len(remotes)-1]
					for _, jobID := range jobIDs {
						remote.Jobs = append(remote.Jobs, jobID)
						fmt.Println(jobID)
					}
				} else {
					return fmt.Errorf("illegal job identifier: %s", args[i])
				}
			case "--continuous":
				i++
				if i >= len(args) {
					return fmt.Errorf("missing continous period")
				}
				config.Continuous, err = strconv.Atoi(args[i])
				if err != nil || config.Continuous < 0 {
					fmt.Fprintln(os.Stderr, "Continous duration needs to be a positive, non-zero integer that determines the seconds between refreshes")
					return fmt.Errorf("invalid continous period")
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
			case "--no-follow":
				config.Follow = false
			case "--rabbitmq":
				config.RabbitMQ = true
			case "--rabbit":
				config.RabbitMQ = true
				i++
				if i >= len(args) {
					return fmt.Errorf("missing RabbitMQ config file")
				}
				config.RabbitMQFiles = append(config.RabbitMQFiles, args[i])
			case "--no-rabbit":
				config.RabbitMQ = false
			case "--hierarchy":
				config.Hierarchy = true
			case "--config":
				i++
				if i >= len(args) {
					return fmt.Errorf("missing config file")
				}
				err = config.ReadFile(args[i])
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading config '%s': %s\n", args[i], err)
					os.Exit(1)
				}
			case "--hide-state", "--hide-job-state", "--hide":
				i++
				if i >= len(args) {
					return fmt.Errorf("missing job state")
				}
				states := trimSplit(args[i], ",")
				config.HideStates = append(config.HideStates, states...)
			case "--quit", "--exit":
				config.Quit = true
			case "--input":
				i++
				if i >= len(args) {
					return fmt.Errorf("missing input file")
				}
				// Read input file and add jobs
				if jobs, err := readJobs(args[i]); err != nil {
					return fmt.Errorf("error reading jobs: %s", err)
				} else {
					// Append all found jobs
					for _, remote := range jobs {
						for _, job := range remote.Jobs {
							remotes = appendRemote(remotes, remote.URI, job)
						}
					}
				}
			default:
				return fmt.Errorf("invalid argument: %s", arg)
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
							return fmt.Errorf("jobs need to be defined after a remote instance")
						}
						remote := Remote{URI: config.DefaultRemote}
						remotes = append(remotes, remote)
					}
					remote := &remotes[len(remotes)-1]
					remote.Jobs = append(remote.Jobs, jobIDs...)
				} else {
					return fmt.Errorf("illegal input: %s. Input must be either a REMOTE (starting with http:// or https://) or a JOB identifier", arg)
				}
			}
		}
	}
	return nil
}

func main() {
	var err error
	remotes = make([]Remote, 0)
	config.SetDefaults()
	// Read config files: Global '/etc/openqa/openqa-mon.conf' and user '~/openqa-mon.conf'
	// readConfig ignores a nonexisting file and returns nil
	err = config.ReadFile("/etc/openqa/openqa-mon.conf")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config '/etc/openqa/openqa-mon.conf': %s\n", err)
		os.Exit(1)
	}
	// Still read the deprecated configuration file location
	err = config.ReadFile(homeDir() + "/.openqa-mon.conf")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config '"+homeDir()+"/.openqa-mon.conf': %s\n", err)
		os.Exit(1)
	}
	err = config.ReadFile(homeDir() + "/.config/openqa-mon/openqa-mon.conf")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config '"+homeDir()+"/.config/openqa-mon/openqa-mon.conf': %s\n", err)
		os.Exit(1)
	}

	if err := parseProgramArguments(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
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

	// Remove duplicate IDs and sort jobs by ID
	for _, remote := range remotes {
		remote.Jobs = unique64(remote.Jobs)
		sort.Slice(remote.Jobs, func(i, j int) bool {
			return remote.Jobs[i] < remote.Jobs[j]
		})
	}

	// Single listing mode, no TUI
	if config.Continuous <= 0 {
		singleCall(remotes)
		os.Exit(0)
	}

	// Refresh rates below 30 seconds are not allowed on public instances
	if config.Continuous < 30 {
		for _, remote := range remotes {
			if strings.Contains(remote.URI, "://openqa.suse.de") || strings.Contains(remote.URI, "://openqa.opensuse.org") {
				config.Continuous = 30
				break
			}
		}
	}

	tui = CreateTUI()
	tui.EnterAltScreen()
	tui.Clear()
	remotesString := fmt.Sprintf("%d remotes", len(remotes))
	if len(remotes) == 1 {
		remotesString = remotes[0].URI
	}
	tui.remotes = remotesString
	tui.SetHeader(fmt.Sprintf("openqa-mon v%s - Monitoring %s", internal.VERSION, remotesString))
	tui.Model.HideStates = config.HideStates
	tui.Update()
	defer tui.LeaveAltScreen()
	continuousMonitoring(remotes)
	os.Exit(0)
}

/* Get all jobs from the given remotes
 * callback will be called for each received job
 * returns the (possibly modified) input remotes
 */
func FetchJobs(remotes []Remote, callback func(int64, gopenqa.Job)) ([]Remote, error) {
	for i, remote := range remotes {
		instance := gopenqa.CreateInstance(ensureHTTP(remote.URI))
		instance.SetUserAgent("openqa-mon")
		instance.SetMaxRecursionDepth(20) // Certain jobs (e.g. verification runs) can have a lot of clones
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
			jobs, err := instance.GetJobs(remote.Jobs)
			if err != nil {
				return remotes, err
			}
			for i, job := range jobs {
				if config.Follow && (job.IsCloned()) {
					job, err = instance.GetJobFollow(job.ID)
					if err != nil {
						// It's better to ignore a single failure than to suppress following jobs as well
						continue
					}
					remote.Jobs[i] = job.ID
					jobsModified = true
				}
				callback(job.ID, job)

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
				jobs := unique64(remote.Jobs)
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
func NotifyJobsChanged(jobs []gopenqa.Job) {
	if config.Bell {
		bell()
	}
	if config.Notify {
		notification := ""
		if len(jobs) == 1 {
			j := jobs[0]
			notification = fmt.Sprintf("[%s] - Job %d %s", j.JobState(), j.ID, j.Name)
		} else {
			for _, j := range jobs {
				notification += fmt.Sprintf("[%s] %s\n", j.JobState(), j.Name)
			}
		}
		notification = strings.TrimSpace(notification)

		if notification != "" {
			notifySend(notification)
		}
	}
}

// Single call - Run without terminal user interface, just list the received jobs and quit
func singleCall(remotes []Remote) {
	width, _ := terminalSize()
	// If not a tty, disable color
	color := IsTTY()

	// Fetch jobs and list them
	_, err := FetchJobs(remotes, func(id int64, job gopenqa.Job) {
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
	p := make([]byte, 3) // History, needed for special keys
	tui.Keypress = func(b byte) {
		p[2], p[1], p[0] = p[1], p[0], b

		// Handle special keys
		if p[2] == 27 && p[1] == 91 {
			switch p[0] {
			case 72: // home
				tui.FirstPage()
			case 70: // end
				tui.LastPage()
			case 53: // page up
				tui.PrevPage()
			case 54: // page down
				tui.NextPage()
			case 68: // arrow left
				tui.PrevPage()
			case 67: // arrow right
				tui.NextPage()
			}
		} else {
			switch b {
			case 'q':
				tui.LeaveAltScreen()
				os.Exit(0)
			case 'r':
				// Refresh
				refreshSignal <- 1
				return
			case '?':
				tui.SetShowHelp(!tui.DoShowHelp())
			case 'h':
				tui.SetHideStates(!tui.DoHideStates())
			case 'p':
				config.Paused = !config.Paused
				if config.Paused {
					SetStatus()
				} else {
					refreshSignal <- 1
				}
				return
			case 'd', 'n':
				config.Notify = !config.Notify
				SetStatus()
			case 'b':
				config.Bell = !config.Bell
				SetStatus()
			case 'm':
				config.Notify = false
				config.Bell = false
				SetStatus()
			case 'l':
				config.Notify = true
				config.Bell = true
				SetStatus()
			case '+':
				config.Continuous++
				SetStatus()
			case '-':
				if config.Continuous > 1 {
					config.Continuous--
				}
				SetStatus()
			case '>':
				tui.NextPage()
			case '<':
				tui.PrevPage()
			}
		}
		tui.Update()
	}

	// Start TUI handlers (keypress, ecc)
	tui.Start()

	// Register RabbitMQ
	config.Paused = false // Pause pulling new stats, if all remotes are present as RabbitMQ hosts
	if config.RabbitMQ {

		rabbits, err := ReadRabbitMQs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading rabbitmq configuration: %s\n", err)
			// This error is non-critical for now, as the fallback to pull the stats is still working
		}

		// Try to register to all remotes. Unpause if we fail on at least one
		config.Paused = true

		for _, remote := range remotes {
			openqaURI := remote.URI
			hostname := getHostname(openqaURI)
			if rabbit, ok := rabbits[hostname]; ok {
				// generic queues that should work with all openQA instances
				// Note: "#.job.cancel" is included in "#.job.done"
				// Note: There are no messages that signal when a job is started
				queue := []string{"#.job.done", "#.job.restart"}
				remote := assembleRabbitMQRemote(rabbit.Remote, rabbit.Username, rabbit.Password)
				rabbitmq, err := registerRabbitMQ(&tui, openqaURI, remote, queue)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error establishing link to RabbitMQ %s: %s\n", rabbit.Remote, err)
					config.Paused = false
				}
				defer rabbitmq.Close()
			} else {
				config.Paused = false
				fmt.Fprintf(os.Stderr, "No RabbitMQ available for %s\n", hostname)
			}
		}
	}

	// Current state of the jobs
	jobs := make([]gopenqa.Job, 0)

	tui.SetStatus("Initial job fetching ... ")
	force := true // Forced refresh
	for {
		if !force && config.Paused {
			SetStatus()
		} else {
			force = false
			exists := make(map[int64]bool, 0)    // Keep track of existing jobs
			notifyJobs := make([]gopenqa.Job, 0) // jobs which fire a notification
			// Fetch new jobs. Update remotes (job id's) when necessary
			remotes, err = FetchJobs(remotes, func(id int64, job gopenqa.Job) {
				exists[job.ID] = true
				// Job received. Update existing job or add job if not yet present
				for i, j := range jobs {
					if j.ID == id { // Compare to given id as this is the original id (not the ID of a possible cloned job)
						jobs[i] = job
						// Ignore if job status remains the same
						if j.JobState() == job.JobState() {
							return
						}
						// Ignore trivial changes (uploading, assigned) and skipped jobs
						state := job.JobState()
						if state == "uploading" || state == "assigned" || state == "skipped" || state == "cancelled" {
							return
						}
						// Notify about job update
						notifyJobs = append(notifyJobs, job)
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
			if len(notifyJobs) > 0 {
				NotifyJobsChanged(notifyJobs)
			}

			// Remove items which are not present anymore - (e.g. old children)
			jobs = uniqueJobs(filterJobs(jobs, func(job gopenqa.Job) bool {
				_, ok := exists[job.ID]
				return ok
			}))
			tui.Model.SetJobs(jobs)
			if err != nil {
				tui.SetStatus(fmt.Sprintf("Error fetching jobs: %s", err))
			} else {
				SetStatus()
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
		}

		// Wait for next update, or timeout or signal
		select {
		case <-refreshSignal:
			tui.SetStatus("Manual refresh ... ")
			force = true
		case <-time.After(time.Duration(config.Continuous) * time.Second):
			tui.SetStatus("Refreshing ... ")
		}

	}

}
