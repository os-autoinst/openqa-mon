package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/os-autoinst/gopenqa"
	"github.com/os-autoinst/openqa-mon/internal"
)

var tui *TUI

func parseProgramArgs(cf *Config) ([]Config, error) {
	cfs := make([]Config, 0)
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
					return cfs, fmt.Errorf("missing argument: %s", "config file")
				}
				filename := os.Args[i]
				var cf Config
				cf.RequestJobLimit = 100 // Set default limit
				if err := cf.LoadToml(filename); err != nil {
					return cfs, fmt.Errorf("in %s: %s", filename, err)
				}
				cfs = append(cfs, cf)
			} else if arg == "-r" || arg == "--remote" {
				if i++; i >= n {
					return cfs, fmt.Errorf("missing argument: %s", "remote")
				}
				cf.Instance = os.Args[i]
			} else if arg == "-q" || arg == "--rabbit" || arg == "--rabbitmq" {
				if i++; i >= n {
					return cfs, fmt.Errorf("missing argument: %s", "RabbitMQ link")
				}
				cf.RabbitMQ = os.Args[i]
			} else if arg == "-i" || arg == "--hide" || arg == "--hide-status" {
				if i++; i >= n {
					return cfs, fmt.Errorf("missing argument: %s", "Status to hide")
				}
				cf.HideStatus = append(cf.HideStatus, strings.Split(os.Args[i], ",")...)
			} else if arg == "-p" || arg == "--param" {
				if i++; i >= n {
					return cfs, fmt.Errorf("missing argument: %s", "parameter")
				}
				if name, value, err := splitNV(os.Args[i]); err != nil {
					return cfs, fmt.Errorf("argument parameter is invalid: %s", err)
				} else {
					cf.DefaultParams[name] = value
				}
			} else if arg == "-n" || arg == "--notify" || arg == "--notifications" {
				cf.Notify = true
			} else if arg == "-m" || arg == "--mute" || arg == "--silent" || arg == "--no-notify" {
				cf.Notify = false
			} else {
				return cfs, fmt.Errorf("illegal argument: %s", arg)
			}
		} else {
			// Convenience logic. If it contains a = then assume it's a parameter, otherwise assume it's a config file
			if strings.Contains(arg, "=") {
				if name, value, err := splitNV(arg); err != nil {
					return cfs, fmt.Errorf("argument parameter is invalid: %s", err)
				} else {
					cf.DefaultParams[name] = value
				}
			} else {
				// Assume it's a config file
				var cf Config
				if err := cf.LoadToml(arg); err != nil {
					return cfs, fmt.Errorf("in %s: %s", arg, err)
				}
				cfs = append(cfs, cf)
			}
		}
	}
	return cfs, nil
}

func printUsage() {
	fmt.Printf("Usage: %s [OPTIONS] [FLAVOR] [CONFIG...]\n", os.Args[0])
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
	fmt.Println("openqa-review is part of openqa-mon (https://github.com/os-autoinst/openqa-mon/)")
}

// Register the given rabbitMQ instance for the tui
func registerRabbitMQ(model *TUIModel, remote, topic string) (gopenqa.RabbitMQ, error) {
	rmq, err := gopenqa.ConnectRabbitMQ(remote)
	if err != nil {
		return rmq, fmt.Errorf("RabbitMQ connection error: %s", err)
	}
	sub, err := rmq.Subscribe(topic)
	if err != nil {
		return rmq, fmt.Errorf("RabbitMQ subscribe error: %s", err)
	}
	// Receive function
	go func(model *TUIModel) {
		cf := model.Config
		for {
			if status, err := sub.ReceiveJobStatus(); err == nil {
				now := time.Now()
				// Check if we know this job or if this is just another job.
				job := model.Job(status.ID)
				if job.ID == 0 {
					continue
				}

				tui.SetTracker(fmt.Sprintf("[%s] Job %d-%s:%s %s", now.Format("15:04:05"), job.ID, status.Flavor, status.Build, status.Result))
				job.State = "done"
				job.Result = fmt.Sprintf("%s", status.Result)

				if cf.Notify && !model.HideJob(*job) {
					NotifySend(fmt.Sprintf("%s: %s %s", job.JobState(), job.Name, job.Test))
				}
			}
		}
	}(model)
	return rmq, err
}

func main() {
	var defaultConfig Config
	var err error
	var cfs []Config

	if defaultConfig, err = LoadDefaultConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading default config file: %s\n", err)
		os.Exit(1)
	}
	if cfs, err = parseProgramArgs(&defaultConfig); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	// Use default configuration only if no configuration files are loaded.
	if len(cfs) < 1 {
		if err := defaultConfig.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(1)
		}
		cfs = append(cfs, defaultConfig)
	}

	// Run terminal user interface from all available configuration objects
	tui = CreateTUI()
	for _, cf := range cfs {
		model := tui.CreateTUIModel(&cf)

		// Apply sorting of the first group
		switch cf.GroupBy {
		case "none", "":
			model.SetSorting(0)
		case "groups", "jobgroups":
			model.SetSorting(1)
		default:
			fmt.Fprintf(os.Stderr, "Unsupported GroupBy: '%s'\n", cf.GroupBy)
			os.Exit(1)
		}
	}

	// Some settings get applied from the last available configuration
	cf := cfs[len(cfs)-1]
	tui.SetHideStatus(cf.HideStatus)

	// Enter main loop
	err = main_loop()
	tui.LeaveAltScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func RefreshJobs() error {
	model := tui.Model()

	// Determine if a job is already known or not
	knownJobs := model.jobs
	getKnownJob := func(id int64) (gopenqa.Job, bool) {
		for _, j := range knownJobs {
			if j.ID == id {
				return j, true
			}
		}
		return gopenqa.Job{}, false
	}

	// Get fresh jobs
	status := tui.Status()
	oldJobs := model.Jobs()
	tui.SetStatus(fmt.Sprintf("Refreshing %d jobs ... ", len(oldJobs)))
	tui.Update()

	// Refresh all jobs at once in one request
	ids := make([]int64, 0)
	for _, job := range oldJobs {
		ids = append(ids, job.ID)
	}
	callback := func(i, n int) {
		tui.SetStatus(fmt.Sprintf("Refreshing %d jobs ... %d%% ", len(oldJobs), 100/n*i))
		tui.Update()
	}
	jobs, err := fetchJobsFollow(ids, model, callback)
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
			model.Apply(jobs)
			tui.Update()
			if model.Config.Notify && !model.HideJob(job) {
				NotifySend(fmt.Sprintf("%s: %s %s", job.JobState(), job.Name, job.Test))
			}
		}
		tui.Update()
		// Scan failed jobs for comments
		state := job.JobState()
		if state == "failed" || state == "incomplete" || state == "parallel_failed" {
			reviewed, err := isReviewed(job, model, state == "parallel_failed")
			if err != nil {
				return err
			}

			model.SetReviewed(job.ID, reviewed)
			tui.Update()
		}
	}
	model.Apply(jobs)
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

func initialQuery() error {
	var err error
	if len(tui.Tabs) == 0 {
		model := &tui.Tabs[0]
		cf := model.Config
		fmt.Printf("Initial querying instance %s ... \n", cf.Instance)
	} else {
		fmt.Printf("Initial querying for %d configurations ... \n", len(tui.Tabs))
	}

	for i := range tui.Tabs {
		model := &tui.Tabs[i]
		cf := model.Config

		// Refresh rates below 5 minutes are not allowed on public instances due to the load it puts on them

		if cf.RefreshInterval < 300 {
			if strings.Contains(cf.Instance, "://openqa.suse.de") || strings.Contains(cf.Instance, "://openqa.opensuse.org") {
				cf.RefreshInterval = 300
			}
		}

		fmt.Printf("Initial querying instance %s for config %d/%d ... \n", cf.Instance, i+1, len(tui.Tabs))
		model.jobGroups, err = FetchJobGroups(model.Instance)
		if err != nil {
			return fmt.Errorf("error fetching job groups: %s", err)
		}
		if len(model.jobGroups) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: No job groups found\n")
		}
		fmt.Print("\033[s") // Save cursor position
		fmt.Printf("\tGet jobs for %d groups ...", len(cf.Groups))
		jobs, err := FetchJobs(model, func(group int, groups int, job int, jobs int) {
			fmt.Print("\033[u") // Restore cursor position
			fmt.Print("\033[K") // Erase till end of line
			fmt.Printf("\tGet jobs for %d groups ... %d/%d", len(cf.Groups), group, groups)
			if job == 0 {
				fmt.Printf(" (%d jobs)", jobs)
			} else {
				fmt.Printf(" (%d/%d jobs)", job, jobs)
			}
		})
		model.Apply(jobs)
		fmt.Println()
		if err != nil {
			return fmt.Errorf("error fetching jobs: %s", err)
		}
		if len(jobs) == 0 {
			// No reason to continue - there are no jobs to scan
			return fmt.Errorf("no jobs found")
		}
		// Failed jobs will be also scanned for comments
		for _, job := range model.jobs {
			state := job.JobState()
			if state == "failed" || state == "incomplete" || state == "parallel_failed" {
				reviewed, err := isReviewed(job, model, state == "parallel_failed")
				if err != nil {
					return fmt.Errorf("error fetching job comment: %s", err)
				}
				model.SetReviewed(job.ID, reviewed)
			}
		}
	}
	return nil
}

func registerRabbitMQs() ([]gopenqa.RabbitMQ, error) {
	rabbitmqs := make([]gopenqa.RabbitMQ, 0)
	for i := range tui.Tabs {
		model := &tui.Tabs[i]
		cf := model.Config

		// Register RabbitMQ
		if cf.RabbitMQ != "" {
			rabbitmq, err := registerRabbitMQ(model, cf.RabbitMQ, cf.RabbitMQTopic)
			// RabbitMQ is still considered experimental, as such errors are not considered critical for the program.
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error establishing link to RabbitMQ %s: %s\n", rabbitRemote(cf.RabbitMQ), err)
			}
			rabbitmqs = append(rabbitmqs, rabbitmq)
		}
	}
	return rabbitmqs, nil
}

// main loop
func main_loop() error {
	title := "openqa Review TUI Dashboard v" + internal.VERSION

	var rabbitmqs []gopenqa.RabbitMQ
	var err error

	tui.Keypress = func(key byte, update *bool) {
		refreshing := false
		// Input handling
		switch key {
		case 'r':
			if !refreshing {
				refreshing = true
				go func() {
					if err := RefreshJobs(); err != nil {
						tui.SetStatus(fmt.Sprintf("Error while refreshing: %s", err))
					}
					refreshing = false
				}()
			}
		case 'u':
			*update = true
		case 'q':
			tui.Done()
			*update = false
		case 'h':
			tui.SetHide(!tui.Hide())
			tui.Model().MoveHome()
		case 'm':
			tui.SetShowTracker(!tui.showTracker)
		case 's':
			// Shift through the sorting mechanism
			model := tui.Model()
			model.SetSorting((model.Sorting() + 1) % 2)
		case 'o', 'O':
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
		}
	}
	tui.EnterAltScreen()
	tui.Clear()
	tui.SetHeader(title)
	defer tui.LeaveAltScreen()

	// Program initialization
	fmt.Println(title)
	fmt.Println("")
	if err := initialQuery(); err != nil {
		return err
	}
	rabbitmqs, err = registerRabbitMQs()
	if err != nil {
		// RabbitMQ errors are not critical.
		fmt.Fprintf(os.Stderr, "RabbitMQ error: %s\n", err)
	}

	fmt.Println("Initialization completed. Entering main loop ... ")
	tui.Start()
	tui.Update()

	tui.StartPeriodicRefresh()
	tui.AwaitTermination()
	tui.LeaveAltScreen()

	for i := range rabbitmqs {
		rabbitmqs[i].Close()
	}
	return nil
}
