package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Job is a running Job instance
type Job struct {
	AssignedWorkerID int      `json:"assigned_worker_id"`
	BlockedByID      int      `json:"blocked_by_id"`
	Children         Children `json:"children"`
	Parents          Children `json:"parents"`
	CloneID          int      `json:"clone_id"`
	GroupID          int      `json:"group_id"`
	ID               int      `json:"id"`
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
	Link   string
	Remote string
	Prefix string
}

// Children struct is for chained, directly chained and parallel children/parents
type Children struct {
	Chained         []int `json:"Chained"`
	DirectlyChained []int `json:"Directly chained"`
	Parallel        []int `json:"Parallel"`
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

	name := job.Prefix
	if len(name) > 0 {
		name += " "
	}
	name += job.Test + "@" + job.Settings.Machine
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
		if job, err = fetchJob(url, job.ID); err != nil {
			return jobs, err
		} else {
			job.Remote = url
			jobs[i] = job
		}
	}
	return jobs, nil
}

func parseJobs(jobs string) ([]int, error) {
	split := strings.Split(jobs, ",")
	ret := make([]int, 0)
	for _, sID := range split {
		if id, err := strconv.Atoi(sID); err != nil {
			return ret, err
		} else {
			ret = append(ret, id)
		}
	}
	return ret, nil
}

// parseJobID parses the given text for a valid job id ("[.*#]INTEGER[:]" and INTEGER > 0). Returns the job id if valid or 0 on error
func parseJobID(parseText string) int {
	// Remove possible fragment in case the user dumped a part of a url
	parseText = removeFragment(parseText)
	// Remove : at the end
	for len(parseText) > 1 && parseText[len(parseText)-1] == ':' {
		parseText = parseText[:len(parseText)-1]
	}
	if num, err := strconv.Atoi(parseText); err != nil || num <= 0 {
		return 0
	} else {
		return num
	}
}

func createIntRange(min int, max int, offset int) []int {
	ret := make([]int, 0)
	for i := min; i <= max; i++ {
		ret = append(ret, i+offset)
	}
	return ret
}

// parseJobIDs parses the given text for a valid job id ("[#]INTEGER[:]" and INTEGER > 0) or job id ranges (MIN..MAX). Returns the job id if valid or 0 on error
func parseJobIDs(parseText string) []int {
	ret := make([]int, 0)
	// Search for range
	i := strings.Index(parseText, "..")
	if i > 0 {
		lower, upper := parseText[:i], parseText[i+2:]
		min, max := parseJobID(lower), parseJobID(upper)
		if min <= 0 || max <= 0 {
			return ret
		}
		if min > max {
			min, max = max, min
		}
		return createIntRange(min, max, 0)
	}
	// Search for + (usage: jobID+3 returns [jobID,jobID+3])
	i = strings.Index(parseText, "+")
	if i > 0 {
		lower, upper := parseText[:i], parseText[i+1:]
		start := parseJobID(lower)
		if start <= 0 {
			return ret
		}
		steps, _ := strconv.Atoi(upper) // On errors it returns conveniently 0, so don't do error checking here
		return createIntRange(0, steps, start)
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
