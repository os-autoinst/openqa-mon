package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strings"
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

func (job *Job) Println(useColors bool) {
	name := job.Test + "@" + job.Settings.Machine

	if job.State == "running" {
		if useColors {
			fmt.Print(KGRN)
		}
		fmt.Printf(" %-6d %-59s %12s\n", job.ID, name, job.State)
		if useColors {
			fmt.Print(KNRM)
		}
	} else if job.State == "done" {
		if useColors {
			switch job.Result {
			case "failed":
				fmt.Print(KRED)
			case "incomplete":
				fmt.Print(KRED)
			case "user_cancelled":
				fmt.Print(KYEL)
			case "passed":
				fmt.Print(KBLU)
			default:
				fmt.Print(KWHT)
			}
		}
		fmt.Printf(" %-6d %-59s %12s\n", job.ID, name, job.Result)
		if useColors {
			fmt.Print(KNRM)
		}
	} else {

		if useColors {
			fmt.Print(KCYN)
		}
		fmt.Printf(" %-6d %-59s %12s\n", job.ID, name, job.State)
		if useColors {
			fmt.Print(KNRM)
		}
	}

}

type byId []Job

func (s byId) Len() int {
	return len(s)
}
func (s byId) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byId) Less(i, j int) bool {
	return s[i].ID < s[j].ID

}

func FetchJob(url string, jobId int) (Job, error) {
	var job JobStruct
	url = fmt.Sprintf("%s/api/v1/jobs/%d", url, jobId)
	resp, err := http.Get(url)
	if err != nil {
		return job.Job, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return job.Job, err
	}
	err = json.Unmarshal(body, &job)
	if err != nil {
		return job.Job, err
	}
	return job.Job, nil
}

func GetLatestJobs(url string) ([]Job, error) {
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
		job, err = FetchJob(url, job.ID)
		if err != nil {
			return jobs, err
		}
		jobs[i] = job
	}
	return jobs, nil
}

func main() {
	args := os.Args
	if len(args) < 2 {
		fmt.Printf("Usage: %s REMOTE\n  REMOTE is the base URL of the OpenQA server\n", args[0])
		return
	}

	useColors := true
	printN := 10
	for _, remote := range args[1:] {
		remote = ensureHTTP(remote)

		jobs, err := GetLatestJobs(remote)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching jobs: %s\n", err)
			continue
		}
		if len(jobs) == 0 {
			fmt.Println("No jobs on instance found")
			continue
		}
		sort.Sort(byId(jobs))

		// Print only the last n jobs
		for i, job := range jobs {
			if i >= len(jobs)-printN {
				job.Println(useColors)
			}
		}
	}

}
