package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/grisu48/gopenqa"
	"github.com/grisu48/openqa-mon/internal"
)

var knownJobs []gopenqa.Job
var updatedRefresh bool

func getKnownJob(id int64) (gopenqa.Job, bool) {
	for _, j := range knownJobs {
		if j.ID == id {
			return j, true
		}
	}
	return gopenqa.Job{}, false
}

/** Try to update the job with the given status, if present. Returns the found job and true if the job was present */
func updateJobStatus(status gopenqa.JobStatus) (gopenqa.Job, bool) {
	var job gopenqa.Job
	for i, j := range knownJobs {
		if j.ID == status.ID {
			knownJobs[i].State = "done"
			knownJobs[i].Result = fmt.Sprintf("%s", status.Result)
			return knownJobs[i], true
		}
	}
	return job, false
}

func loadDefaultConfig() error {
	configFile := homeDir() + "/.openqa-revtui.toml"
	if fileExists(configFile) {
		if err := cf.LoadToml(configFile); err != nil {
			return err
		}
	}
	return nil
}

func parseProgramArgs() error {
	n := len(os.Args)
	for i := 1; i < n; i++ {
		arg := os.Args[i]
		if arg == "" {
			continue
		}
		if arg[0] == '-' {
			if arg == "-h" || arg == "--help" {
				printUsage()
				os.Exit(0)
			} else if arg == "--version" {
				fmt.Println("openqa-revtui version " + internal.VERSION)
				os.Exit(0)
			} else if arg == "-c" || arg == "--config" {
				if i++; i >= n {
					return fmt.Errorf("missing argument: %s", "config file")
				}
				filename := os.Args[i]
				if err := cf.LoadToml(filename); err != nil {
					return fmt.Errorf("in %s: %s", filename, err)
				}
			} else if arg == "-r" || arg == "--remote" {
				if i++; i >= n {
					return fmt.Errorf("missing argument: %s", "remote")
				}
				cf.Instance = os.Args[i]
			} else if arg == "-q" || arg == "--rabbit" || arg == "--rabbitmq" {
				if i++; i >= n {
					return fmt.Errorf("missing argument: %s", "RabbitMQ link")
				}
				cf.RabbitMQ = os.Args[i]
			} else if arg == "-i" || arg == "--hide" || arg == "--hide-status" {
				if i++; i >= n {
					return fmt.Errorf("missing argument: %s", "Status to hide")
				}
				cf.HideStatus = append(cf.HideStatus, strings.Split(os.Args[i], ",")...)
			} else if arg == "-p" || arg == "--param" {
				if i++; i >= n {
					return fmt.Errorf("missing argument: %s", "parameter")
				}
				if name, value, err := splitNV(os.Args[i]); err != nil {
					return fmt.Errorf("argument parameter is invalid: %s", err)
				} else {
					cf.DefaultParams[name] = value
				}
			} else if arg == "-n" || arg == "--notify" || arg == "--notifications" {
				cf.Notify = true
			} else if arg == "-m" || arg == "--mute" || arg == "--silent" || arg == "--no-notify" {
				cf.Notify = false
			} else {
				return fmt.Errorf("illegal argument: %s", arg)
			}
		} else {
			// Convenience logic. If it contains a = then assume it's a parameter, otherwise assume it's a config file
			if strings.Contains(arg, "=") {
				if name, value, err := splitNV(arg); err != nil {
					return fmt.Errorf("argument parameter is invalid: %s", err)
				} else {
					cf.DefaultParams[name] = value
				}
			} else {
				// Assume it's a config file
				if err := cf.LoadToml(arg); err != nil {
					return fmt.Errorf("in %s: %s", arg, err)
				}
			}
		}
	}
	return nil
}

func printUsage() {
	fmt.Printf("Usage: %s [OPTIONS] [FLAVORS]\n", os.Args[0])
	fmt.Println("")
	fmt.Println("OPTIONS")
	fmt.Println("    -h,--help                           Print this help message")
	fmt.Println("    -c,--config FILE                    Load toml configuration from FILE")
	fmt.Println("    -r,--remote REMOTE                  Define openQA remote URL (e.g. 'https://openqa.opensuse.org')")
	fmt.Println("    -q,--rabbit,--rabbitmq URL          Define RabbitMQ URL (e.g. 'amqps://opensuse:opensuse@rabbit.opensuse.org')")
	fmt.Println("    -i,--hide,--hide-status STATUSES    Comma-separates list of job statuses which should be ignored")
	fmt.Println("    -p,--param NAME=VALUE               Set a default parameter (e.g. \"distri=opensuse\")")
	fmt.Println("    -n,--notify                         Enable notifications")
	fmt.Println("    -m,--mute                           Disable notifications")
	fmt.Println("")
	fmt.Println("openqa-review is part of openqa-mon (https://github.com/grisu48/openqa-mon/)")
}

// Register the given rabbitMQ instance for the tui
func registerRabbitMQ(tui *TUI, remote, topic string) (gopenqa.RabbitMQ, error) {
	rmq, err := gopenqa.ConnectRabbitMQ(remote)
	if err != nil {
		return rmq, fmt.Errorf("RabbitMQ connection error: %s", err)
	}
	sub, err := rmq.Subscribe(topic)
	if err != nil {
		return rmq, fmt.Errorf("RabbitMQ subscribe error: %s", err)
	}
	// Receive function
	go func() {
		for {
			if status, err := sub.ReceiveJobStatus(); err == nil {
				now := time.Now()
				// Update job, if present
				if job, found := updateJobStatus(status); found {
					tui.Model.Apply(knownJobs)
					tui.SetTracker(fmt.Sprintf("[%s] Job %d-%s:%s %s", now.Format("15:04:05"), job.ID, status.Flavor, status.Build, status.Result))
					tui.Update()
					if cf.Notify && !hideJob(job) {
						NotifySend(fmt.Sprintf("%s: %s %s", job.JobState(), job.Name, job.Test))
					}
				} else {
					name := status.Flavor
					if status.Build != "" {
						name += ":" + status.Build
					}
					tui.SetTracker(fmt.Sprintf("RabbitMQ: [%s] Foreign job %d-%s %s", now.Format("15:04:05"), job.ID, name, status.Result))
					tui.Update()
				}
			}
		}
	}()
	return rmq, err
}

func main() {
	cf = CreateConfig()
	if err := loadDefaultConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading default config file: %s\n", err)
		os.Exit(1)

	}
	if err := parseProgramArgs(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	if len(cf.Groups) == 0 {
		fmt.Fprintf(os.Stderr, "No review groups defined\n")
		os.Exit(1)
	}

	instance := gopenqa.CreateInstance(cf.Instance)
	instance.SetUserAgent("openqa-mon/revtui")

	// Refresh rates below 5 minutes are not allowed on public instances due to the load it puts on them
	updatedRefresh = false
	if cf.RefreshInterval < 300 {
		if strings.Contains(cf.Instance, "://openqa.suse.de") || strings.Contains(cf.Instance, "://openqa.opensuse.org") {
			cf.RefreshInterval = 300
			updatedRefresh = true
		}
	}

	// Run TUI and use the return code
	tui := CreateTUI()
	switch cf.GroupBy {
	case "none", "":
		tui.SetSorting(0)
	case "groups", "jobgroups":
		tui.SetSorting(1)
	default:
		fmt.Fprintf(os.Stderr, "Unsupported GroupBy: '%s'\n", cf.GroupBy)
		os.Exit(1)
	}
	tui.SetHideStatus(cf.HideStatus)
	err := tui_main(tui, &instance)
	tui.LeaveAltScreen() // Ensure we leave alt screen
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func refreshJobs(tui *TUI, instance *gopenqa.Instance) error {
	// Get fresh jobs
	status := tui.Status()
	oldJobs := tui.Model.Jobs()
	tui.SetStatus(fmt.Sprintf("Refreshing %d jobs ... ", len(oldJobs)))
	tui.Update()
	// Refresh all jobs at once in one request
	ids := make([]int64, 0)
	for _, job := range oldJobs {
		ids = append(ids, job.ID)
	}
	jobs, err := instance.GetJobsFollow(ids)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		updated := false
		if j, found := getKnownJob(job.ID); found {
			updated = j.ID != job.ID || j.State != job.State || j.Result != job.Result
		} else {
			updated = true
		}

		if updated {
			status = fmt.Sprintf("Last update: [%s] Job %d-%s %s", time.Now().Format("15:04:05"), job.ID, job.Name, job.JobState())
			tui.SetStatus(status)
			tui.Model.Apply(jobs)
			tui.Update()
			if cf.Notify && !hideJob(job) {
				NotifySend(fmt.Sprintf("%s: %s %s", job.JobState(), job.Name, job.Test))
			}
		}
		tui.Update()
		// Scan failed jobs for comments
		state := job.JobState()
		if state == "failed" || state == "incomplete" || state == "parallel_failed" {
			reviewed, err := isReviewed(job, instance, state == "parallel_failed")
			if err != nil {
				return err
			}

			tui.Model.SetReviewed(job.ID, reviewed)
			tui.Update()
		}
	}
	knownJobs = jobs
	tui.Model.Apply(jobs)
	tui.SetStatus(status)
	tui.Update()
	return nil
}

// openJobs opens the given jobs in the browser
func browserJobs(jobs []gopenqa.Job) error {
	for _, job := range jobs {
		if err := exec.Command("xdg-open", job.Link).Start(); err != nil {
			return err
		}
	}
	return nil
}

// main routine for the TUI instance
func tui_main(tui *TUI, instance *gopenqa.Instance) error {
	title := "openqa Review TUI Dashboard v" + internal.VERSION
	var rabbitmq gopenqa.RabbitMQ
	var err error

	refreshing := false
	tui.Keypress = func(key byte) {
		// Input handling
		if key == 'r' {
			if !refreshing {
				refreshing = true
				go func() {
					if err := refreshJobs(tui, instance); err != nil {
						tui.SetStatus(fmt.Sprintf("Error while refreshing: %s", err))
					}
					refreshing = false
				}()
				tui.Update()
			}
		} else if key == 'u' {
			tui.Update()
		} else if key == 'q' {
			tui.done <- true
		} else if key == 'h' {
			tui.SetHide(!tui.Hide())
			tui.Model.MoveHome()
			tui.Update()
		} else if key == 'm' {
			tui.SetShowTracker(!tui.showTracker)
			tui.Update()
		} else if key == 's' {
			// Shift through the sorting mechanism
			tui.SetSorting((tui.Sorting() + 1) % 2)
			tui.Update()
		} else if key == 'o' || key == 'O' {
			// Note: 'o' has a failsafe to not open more than 10 links. 'O' overrides this failsafe
			jobs := tui.GetVisibleJobs()
			if len(jobs) == 0 {
				tui.SetStatus("No visible jobs")
			} else if len(jobs) > 10 && key == 'o' {
				status := fmt.Sprintf("Refuse to open %d (>10) job links. Use 'O' to override", len(jobs))
				tui.SetTemporaryStatus(status, 5)
			} else {
				if err := browserJobs(jobs); err != nil {
					tui.SetStatus(fmt.Sprintf("error: %s", err))
				} else {
					tui.SetStatus(fmt.Sprintf("Opened %d links", len(jobs)))
				}
			}
			tui.Update()
		} else {
			tui.Update()
		}
	}
	tui.EnterAltScreen()
	tui.Clear()
	tui.SetHeader(title)
	defer tui.LeaveAltScreen()

	// Initial query instance via REST API

	fmt.Println(title)
	fmt.Println("")
	if updatedRefresh {
		fmt.Printf(ANSI_YELLOW + "For OSD and O3 a rate limit of 5 minutes between polling is applied" + ANSI_RESET + "\n\n")
	}
	fmt.Printf("Initial querying instance %s ... \n", cf.Instance)
	fmt.Println("\tGet job groups ... ")
	jobgroups, err := FetchJobGroups(instance)
	if err != nil {
		return fmt.Errorf("error fetching job groups: %s", err)
	}
	if len(jobgroups) == 0 {
		fmt.Fprintf(os.Stderr, "Warn: No job groups\n")
	}
	tui.Model.SetJobGroups(jobgroups)
	fmt.Print("\033[s") // Save cursor position
	fmt.Printf("\tGet jobs for %d groups ...", len(cf.Groups))
	jobs, err := FetchJobs(instance, func(group int, groups int, job int, jobs int) {
		fmt.Print("\033[u") // Restore cursor position
		fmt.Print("\033[K") // Erase till end of line
		fmt.Printf("\tGet jobs for %d groups ... %d/%d", len(cf.Groups), group, groups)
		if job == 0 {
			fmt.Printf(" (%d jobs)", jobs)
		} else {
			fmt.Printf(" (%d/%d jobs)", job, jobs)
		}
	})
	fmt.Println()
	if err != nil {
		return fmt.Errorf("error fetching jobs: %s", err)
	}
	if len(jobs) == 0 {
		// No reason to continue - there are no jobs to scan
		return fmt.Errorf("no jobs found")
	}
	// Failed jobs will be also scanned for comments
	for _, job := range jobs {
		state := job.JobState()
		if state == "failed" || state == "incomplete" || state == "parallel_failed" {
			reviewed, err := isReviewed(job, instance, state == "parallel_failed")
			if err != nil {
				return fmt.Errorf("error fetching job comment: %s", err)
			}
			tui.Model.SetReviewed(job.ID, reviewed)
		}
	}
	knownJobs = jobs
	tui.Model.Apply(knownJobs)
	fmt.Println("Initial fetching completed. Entering main loop ... ")
	tui.Start()
	tui.Update()

	// Register RabbitMQ
	if cf.RabbitMQ != "" {
		rabbitmq, err = registerRabbitMQ(tui, cf.RabbitMQ, cf.RabbitMQTopic)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error establishing link to RabbitMQ %s: %s\n", rabbitRemote(cf.RabbitMQ), err)
		}
		defer rabbitmq.Close()
	}

	// Periodic refresh
	if cf.RefreshInterval > 0 {
		go func() {
			for {
				time.Sleep(time.Duration(cf.RefreshInterval) * time.Second)
				if err := refreshJobs(tui, instance); err != nil {
					tui.SetStatus(fmt.Sprintf("Error while refreshing: %s", err))
				}
			}
		}()
	}

	tui.awaitTerminationSignal()
	tui.LeaveAltScreen()
	if cf.RabbitMQ != "" {
		rabbitmq.Close()
	}
	return nil
}
