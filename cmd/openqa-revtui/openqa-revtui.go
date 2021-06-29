package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/grisu48/gopenqa"
)

const VERSION = "0.3"

/* Group is a single configurable monitoring unit. A group contains all parameters that will be queried from openQA */
type Group struct {
	Name   string
	Params map[string]string // Default parameters for query
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

func isReviewed(job gopenqa.Job, instance gopenqa.Instance) (bool, error) {
	comments, err := instance.GetComments(job.ID)
	if err != nil {
		return false, nil
	}
	for _, c := range comments {
		if len(c.BugRefs) > 0 {
			return true, nil
		}
		// Manually check for poo or bsc reference
		if strings.Contains(c.Text, "poo#") || strings.Contains(c.Text, "bsc#") {
			return true, nil
		}
		// Or for link to progress/bugzilla ticket
		if strings.Contains(c.Text, "://progress.opensuse.org/issues/") || strings.Contains(c.Text, "://bugzilla.suse.com/show_bug.cgi?id=") {
			return true, nil
		}
	}
	return false, nil
}

func FetchJobGroups(instance gopenqa.Instance) (map[int]gopenqa.JobGroup, error) {
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

/* Get job or restarted current job of the given job ID */
func FetchJob(id int, instance gopenqa.Instance) (gopenqa.Job, error) {
	var job gopenqa.Job
	for i := 0; i < 25; i++ { // Max recursion depth is 25
		var err error
		job, err = instance.GetJob(id)
		if err != nil {
			return job, err
		}
		if job.CloneID != 0 && job.CloneID != job.ID {
			id = job.CloneID
			time.Sleep(100 * time.Millisecond) // Don't spam the instance
			continue
		}
		return job, nil
	}
	return job, fmt.Errorf("max recursion depth reached")
}

func FetchJobs(instance gopenqa.Instance) ([]gopenqa.Job, error) {
	ret := make([]gopenqa.Job, 0)
	for _, group := range cf.Groups {
		params := group.Params
		jobs, err := instance.GetOverview("", params)
		if err != nil {
			return ret, err
		}
		// Limit jobs to at most MaxJobs
		if len(jobs) > cf.MaxJobs {
			jobs = jobs[:cf.MaxJobs]
		}
		// Get detailed job instances
		for _, job := range jobs {
			if job, err = FetchJob(job.ID, instance); err != nil {
				return ret, err
			} else {
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

/** Try to update the given job, if it exists and if not the same. Returns the found job and true, if an update was successful */
func updateJob(orig_id int, job gopenqa.Job, instance gopenqa.Instance) (gopenqa.Job, bool) {
	for i, j := range knownJobs {
		if j.ID == orig_id {
			if j.ID != job.ID || j.State != job.State || j.Result != job.Result {
				knownJobs[i] = job
				return knownJobs[i], true
			} else {
				return job, false
			}
		}
	}
	return job, false
}

/** Try to update the job with the given status, if present. Returns the found job and true if the job was present */
func updateJobStatus(status gopenqa.JobStatus) (gopenqa.Job, bool) {
	var job gopenqa.Job
	for i, j := range knownJobs {
		if j.ID == status.ID {
			knownJobs[i].State = "done"
			knownJobs[i].Result = status.Result
			return knownJobs[i], true
		}
	}
	return job, false
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

func homeDir() string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return usr.HomeDir
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

// Split a NAME=VALUE string
func splitNV(v string) (string, string, error) {
	i := strings.Index(v, "=")
	if i < 0 {
		return "", "", fmt.Errorf("no separator")
	}
	return v[:i], v[i+1:], nil
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
					return fmt.Errorf("Missing argument: %s", "config file")
				}
				filename := os.Args[i]
				if err := cf.LoadToml(filename); err != nil {
					return fmt.Errorf("In %s: %s", filename, err)
				}
			} else if arg == "-r" || arg == "--remote" {
				if i++; i >= n {
					return fmt.Errorf("Missing argument: %s", "remote")
				}
				cf.Instance = os.Args[i]
			} else if arg == "-q" || arg == "--rabbit" || arg == "--rabbitmq" {
				if i++; i >= n {
					return fmt.Errorf("Missing argument: %s", "RabbitMQ link")
				}
				cf.RabbitMQ = os.Args[i]
			} else if arg == "-i" || arg == "--hide" || arg == "--hide-status" {
				if i++; i >= n {
					return fmt.Errorf("Missing argument: %s", "Status to hide")
				}
				cf.HideStatus = append(cf.HideStatus, strings.Split(os.Args[i], ",")...)
			} else if arg == "-p" || arg == "--param" {
				if i++; i >= n {
					return fmt.Errorf("Missing argument: %s", "parameter")
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
				return fmt.Errorf("Illegal argument: %s", arg)
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
					return fmt.Errorf("In %s: %s", arg, err)
				}
			}
		}
	}
	return nil
}

func printUsage() {
	// TODO: Write this
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
	err := tui_main(&tui, instance)
	tui.LeaveAltScreen() // Ensure we leave alt screen
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func refreshJobs(tui *TUI, instance gopenqa.Instance) error {
	// Get fresh jobs
	status := tui.Status()
	tui.SetStatus("Refreshing jobs ... ")
	tui.Update()
	jobs := tui.Model.Jobs()
	for i, job := range jobs {
		orig_id := job.ID
		job, err := FetchJob(job.ID, instance)
		if err != nil {
			return err
		}
		job, found := updateJob(orig_id, job, instance)
		if found {
			status = fmt.Sprintf("Last update: [%s] Job %d-%s %s", time.Now().Format("15:04:05"), job.ID, job.Name, job.JobState())
			tui.SetStatus(status)
			jobs[i] = job
			tui.Model.Apply(jobs)
			tui.Update()
			if cf.Notify && !hideJob(job) {
				NotifySend(fmt.Sprintf("%s: %s %s", job.JobState(), job.Name, job.Test))
			}
		}
		// Failed jobs will be also scanned for comments
		state := job.JobState()
		if state == "failed" || state == "incomplete" {
			reviewed, err := isReviewed(job, instance)
			if err != nil {
				return err
			}
			tui.Model.SetReviewed(job.ID, reviewed)
			tui.Update()
		}
	}
	tui.Model.Apply(jobs)
	tui.SetStatus(status)
	tui.Update()
	return nil
}

// main routine for the TUI instance
func tui_main(tui *TUI, instance gopenqa.Instance) error {
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
	fmt.Printf("Initial querying instance %s ... \n", cf.Instance)
	fmt.Println("\tGet job groups ... ")
	jobgroups, err := FetchJobGroups(instance)
	if err != nil {
		return fmt.Errorf("Error fetching job groups: %s", err)
	}
	if len(jobgroups) == 0 {
		fmt.Fprintf(os.Stderr, "Warn: No job groups\n")
	}
	tui.Model.SetJobGroups(jobgroups)
	fmt.Printf("\tGet jobs for %d groups ... \n", len(cf.Groups))
	jobs, err := FetchJobs(instance)
	if err != nil {
		return fmt.Errorf("Error fetching jobs: %s", err)
	}
	// Failed jobs will be also scanned for comments
	for _, job := range jobs {
		state := job.JobState()
		if state == "failed" || state == "incomplete" {
			reviewed, err := isReviewed(job, instance)
			if err != nil {
				return fmt.Errorf("Error fetching job comment: %s", err)
			}
			tui.Model.SetReviewed(job.ID, reviewed)
		}
	}
	knownJobs = jobs
	tui.Model.Apply(knownJobs)
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
