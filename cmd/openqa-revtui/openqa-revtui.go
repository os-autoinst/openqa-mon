package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/grisu48/gopenqa"
)

const VERSION = "1.2.0"

/* Group is a single configurable monitoring unit. A group contains all parameters that will be queried from openQA */
type Group struct {
	Name        string
	Params      map[string]string // Default parameters for query
	MaxLifetime int64             // Ignore entries that are older than this value in seconds from now
}

/* Program configuration parameters */
type Config struct {
	Instance        string            // Instance URL to be used
	RabbitMQ        string            // RabbitMQ url to be used
	RabbitMQTopic   string            // Topic to subscribe to
	DefaultParams   map[string]string // Default parameters
	HideStatus      []string          // Status to hide
	Notify          bool              // Notify on job status change
	RefreshInterval int64             // Periodic refresh delay in seconds
	Groups          []Group           // Groups that will be monitord
	MaxJobs         int               // Maximum number of jobs per group to consider
	GroupBy         string            // Display group mode: "none", "groups"
}

var cf Config
var knownJobs []gopenqa.Job
var updatedRefresh bool

func (cf *Config) LoadToml(filename string) error {
	if _, err := toml.DecodeFile(filename, cf); err != nil {
		return err
	}
	// Apply default parameters to group after loading
	for i, group := range cf.Groups {
		for k, v := range cf.DefaultParams {
			if _, exists := group.Params[k]; exists {
				continue
			} else {
				group.Params[k] = v
			}
		}
		// Apply parameter macros
		for k, v := range group.Params {
			param := parseParameter(v)
			if strings.Contains(param, "%") {
				return fmt.Errorf("invalid parameter macro in %s", param)
			}
			group.Params[k] = param
		}
		cf.Groups[i] = group
	}
	return nil
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

/* Create configuration instance and set default vaules */
func CreateConfig() Config {
	var cf Config
	cf.Instance = "https://openqa.opensuse.org"
	cf.RabbitMQ = "amqps://opensuse:opensuse@rabbit.opensuse.org"
	cf.RabbitMQTopic = "opensuse.openqa.job.done"
	cf.HideStatus = make([]string, 0)
	cf.Notify = true
	cf.RefreshInterval = 30
	cf.DefaultParams = make(map[string]string, 0)
	cf.Groups = make([]Group, 0)
	cf.MaxJobs = 20
	return cf
}

// CreateGroup creates a group with the default settings
func CreateGroup() Group {
	var grp Group
	grp.Params = make(map[string]string, 0)
	grp.Params = cf.DefaultParams
	grp.MaxLifetime = 0
	return grp
}

func hideJob(job gopenqa.Job) bool {
	status := job.JobState()
	for _, s := range cf.HideStatus {
		if status == s {
			return true
		}
	}
	return false
}

func isJobTooOld(job gopenqa.Job, maxlifetime int64) bool {
	if maxlifetime <= 0 {
		return false
	}
	if job.Tfinished == "" {
		return false
	}
	tfinished, err := time.Parse("2006-01-02T15:04:05", job.Tfinished)
	if err != nil {
		return false
	}
	deltaT := time.Now().Unix() - tfinished.Unix()
	return deltaT > maxlifetime
}

func isReviewed(job gopenqa.Job, instance *gopenqa.Instance, checkParallel bool) (bool, error) {
	reviewed, err := checkReviewed(job.ID, instance)
	if err != nil || reviewed {
		return reviewed, err
	}

	// If not reviewed but "parallel_failed", check parallel jobs if they are reviewed
	if checkParallel {
		for _, childID := range job.Children.Parallel {
			reviewed, err := checkReviewed(childID, instance)
			if err != nil {
				return reviewed, err
			}
			if reviewed {
				return true, nil
			}
		}
	}
	return false, nil
}

func checkReviewed(job int64, instance *gopenqa.Instance) (bool, error) {
	comments, err := instance.GetComments(job)
	if err != nil {
		return false, nil
	}
	for _, c := range comments {
		if len(c.BugRefs) > 0 {
			return true, nil
		}
		// Manually check for poo or bsc reference
		if strings.Contains(c.Text, "poo#") || strings.Contains(c.Text, "bsc#") || strings.Contains(c.Text, "boo#") {
			return true, nil
		}
		// Or for link to progress/bugzilla ticket
		if strings.Contains(c.Text, "://progress.opensuse.org/issues/") || strings.Contains(c.Text, "://bugzilla.suse.com/show_bug.cgi?id=") || strings.Contains(c.Text, "://bugzilla.opensuse.org/show_bug.cgi?id=") {
			return true, nil
		}
	}
	return false, nil
}

func FetchJobGroups(instance *gopenqa.Instance) (map[int]gopenqa.JobGroup, error) {
	jobGroups := make(map[int]gopenqa.JobGroup)
	groups, err := instance.GetJobGroups()
	if err != nil {
		return jobGroups, err
	}
	for _, jg := range groups {
		jobGroups[jg.ID] = jg
	}
	return jobGroups, nil
}

/* Get job or clone current job of the given job ID */
func FetchJob(id int64, instance *gopenqa.Instance) (gopenqa.Job, error) {
	var job gopenqa.Job
	for i := 0; i < 25; i++ { // Max recursion depth is 25
		var err error
		job, err = instance.GetJob(id)
		if err != nil {
			return job, err
		}
		if job.IsCloned() {
			id = job.CloneID
			time.Sleep(100 * time.Millisecond) // Don't spam the instance
			continue
		}
		return job, nil
	}
	return job, fmt.Errorf("max recursion depth reached")
}

/* Fetch the given jobs from the instance at once */
func fetchJobs(ids []int64, instance *gopenqa.Instance) ([]gopenqa.Job, error) {
	jobs, err := instance.GetJobs(ids)
	if err != nil {
		return jobs, err
	}

	// Get cloned jobs, if present
	for i, job := range jobs {
		if job.IsCloned() {
			job, err = FetchJob(job.ID, instance)
			if err != nil {
				return jobs, err
			}
			jobs[i] = job
		}
	}
	return jobs, nil
}

type FetchJobsCallback func(int, int, int, int)

func FetchJobs(instance *gopenqa.Instance, callback FetchJobsCallback) ([]gopenqa.Job, error) {
	ret := make([]gopenqa.Job, 0)
	for i, group := range cf.Groups {
		params := group.Params
		jobs, err := instance.GetOverview("", params)
		if err != nil {
			return ret, err
		}

		// Limit jobs to at most MaxJobs
		if len(jobs) > cf.MaxJobs {
			jobs = jobs[:cf.MaxJobs]
		}

		// Get detailed job instances. Fetch them at once
		ids := make([]int64, 0)
		for _, job := range jobs {
			ids = append(ids, job.ID)
		}
		if callback != nil {
			// Add one to the counter to indicate the progress to humans (0/16 looks weird)
			callback(i+1, len(cf.Groups), 0, len(jobs))
		}
		jobs, err = fetchJobs(ids, instance)
		if err != nil {
			return jobs, err
		}
		for _, job := range jobs {
			// Filter too old jobs and jobs with group=0
			if job.GroupID == 0 || !isJobTooOld(job, group.MaxLifetime) {
				ret = append(ret, job)
			}
		}
	}
	return ret, nil
}

// Returns the remote host from a RabbitMQ URL
func rabbitRemote(remote string) string {
	i := strings.Index(remote, "@")
	if i > 0 {
		return remote[i+1:]
	}
	return remote
}

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
				fmt.Println("openqa-revtui version " + VERSION)
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
	err := tui_main(&tui, &instance)
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
	title := "openqa Review TUI Dashboard v" + VERSION
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
		fmt.Printf(ANSI_YELLOW + "WARNING: For OSD and O3 a rate limit of 5 minutes between polling is enforced." + ANSI_RESET + "\n\n")
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
