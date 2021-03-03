package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/grisu48/gopenqa"
)

/* Group is a single configurable monitoring unit. A group contains all parameters that will be queried from openQA */
type Group struct {
	Name   string
	Flavor string            // Job flavor
	Params map[string]string // Default parameters for query
}

/* Program configuration parameters */
type Config struct {
	Instance        string   // Instance URL to be used
	RabbitMQ        string   // RabbitMQ url to be used
	RabbitMQTopic   string   // Topic to subscribe to
	DefaultDistri   string   // Default distri settings to be used
	HideStatus      []string // Status to hide
	Notify          bool     // Notify on job status change
	RefreshInterval int64    // Periodic refresh delay in seconds
	Groups          []Group  // Groups that will be monitord
	MaxJobs         int      // Maximum number of jobs per flavor to consider
	GroupBy         string   // Display group mode: "none", "groups"
}

var cf Config
var knownJobs []gopenqa.Job

func (cf *Config) LoadToml(filename string) error {
	_, err := toml.DecodeFile(filename, cf)
	return err
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
	cf.DefaultDistri = "opensuse"
	cf.Groups = make([]Group, 0)
	cf.MaxJobs = 20
	return cf
}

// CreateGroup creates a group with the default settings
func CreateGroup(flavor string) Group {
	var grp Group
	grp.Flavor = flavor
	grp.Params = make(map[string]string, 0)
	grp.Params["distri"] = cf.DefaultDistri
	return grp
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
	for {
		job, err := instance.GetJob(id)
		if err != nil {
			return job, err
		}
		if job.CloneID == 0 || job.CloneID == job.ID {
			return job, nil
		} else {
			id = job.CloneID
		}
	}
}

func FetchJobs(instance gopenqa.Instance) ([]gopenqa.Job, error) {
	ret := make([]gopenqa.Job, 0)
	for _, group := range cf.Groups {
		params := group.Params
		params["flavor"] = group.Flavor
		jobs, err := instance.GetOverview("", params)
		if err != nil {
			return ret, err
		}
		// Limit jobs to at most 100, otherwise it's too much
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

/** Try to update the given job, if it exists and if not the same. Returns the found job and true, if an update was successful*/
func updateJob(job gopenqa.Job) (gopenqa.Job, bool) {
	for i, j := range knownJobs {
		if j.ID == job.ID {
			if j.State != job.State || j.Result != job.Result {
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

func loadDefaultConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile := home + "/.openqa-revtui.toml"
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
		} else if arg == "-h" || arg == "--help" {
			printUsage()
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
		} else {
			return fmt.Errorf("Illegal argument: %s", arg)
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
					tui.SetStatus(fmt.Sprintf("Last update: [%s] Job %d-%s:%s %s", now.Format("15:04:05"), job.ID, status.Flavor, status.Build, status.Result))
					tui.SetTracker(fmt.Sprintf("[%s] Job %d-%s:%s %s", now.Format("15:04:05"), job.ID, status.Flavor, status.Build, status.Result))
					tui.Update()
					NotifySend(job.String())
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
	cf.DefaultDistri = "opensuse"
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
	rc := tui_main(&tui, instance)
	tui.LeaveAltScreen() // Ensure we leave alt screen
	os.Exit(rc)
}

func refreshJobs(tui *TUI, instance gopenqa.Instance) {
	// Get fresh jobs
	status := tui.Status()
	tui.SetStatus("Refreshing jobs ... ")
	tui.Update()
	if jobs, err := FetchJobs(instance); err == nil {
		for _, j := range jobs {
			if job, found := updateJob(j); found {
				status = fmt.Sprintf("Last update: [%s] Job %d-%s %s", time.Now().Format("15:04:05"), job.ID, job.Name, job.JobState())
				tui.SetStatus(status)
				tui.Update()
				NotifySend(job.String())
			}
		}
	}
	tui.SetStatus(status)
	tui.Update()
}

// main routine for the TUI instance
func tui_main(tui *TUI, instance gopenqa.Instance) int {
	var rabbitmq gopenqa.RabbitMQ
	var err error

	refreshing := false
	tui.Keypress = func(key byte) {
		// Input handling
		if key == 'r' {
			if !refreshing {
				refreshing = true
				go func() {
					refreshJobs(tui, instance)
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
	tui.Start()
	tui.EnterAltScreen()
	tui.Clear()
	tui.SetHeader("openqa Review - TUI Dashboard")
	defer tui.LeaveAltScreen()

	// Initial query instance via REST API
	fmt.Printf("Initial querying instance %s ... \n", cf.Instance)
	fmt.Println("\tGet job groups ... ")
	jobgroups, err := FetchJobGroups(instance)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching job groups: %s\n", err)
		return 1
	}
	if len(jobgroups) == 0 {
		fmt.Fprintf(os.Stderr, "Warn: No job groups\n")
	}
	tui.Model.SetJobGroups(jobgroups)
	fmt.Printf("\tGet jobs for %d groups ... \n", len(cf.Groups))
	jobs, err := FetchJobs(instance)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching jobs: %s\n", err)
		os.Exit(1)
	}
	knownJobs = jobs
	tui.Model.Apply(knownJobs)

	// Register RabbitMQ
	if cf.RabbitMQ != "" {
		rabbitmq, err = registerRabbitMQ(tui, cf.RabbitMQ, cf.RabbitMQTopic)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error establishing link to RabbitMQ %s: %s\n", rabbitRemote(cf.RabbitMQ), err)
		}
		// defer rabbitmq.Close()
	}

	// Periodic refresh
	if cf.RefreshInterval > 0 {
		go func() {
			for {
				time.Sleep(time.Duration(cf.RefreshInterval) * time.Second)
				refreshJobs(tui, instance)
			}
		}()
	}

	tui.awaitTerminationSignal()
	tui.LeaveAltScreen()
	if cf.RabbitMQ != "" {
		rabbitmq.Close()
	}
	return 0
}
