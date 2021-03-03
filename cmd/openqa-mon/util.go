/* Small utility functions, moved here to keep the main code tidy */
package main

import (
	"os/user"
	"strconv"
	"strings"

	"github.com/grisu48/gopenqa"
)

// Removes fragment from url
func removeFragment(url string) string {
	if i := strings.Index(url, "#"); i >= 0 {
		return url[:i]
	}
	return url
}

// Ensure the given remoet has a http or https prefix
func ensureHTTP(remote string) string {
	if !(strings.HasPrefix(remote, "http://") || strings.HasPrefix(remote, "https://")) {
		return "http://" + remote
	} else {
		return remote
	}
}

// Remove the trailing / from a url
func homogenizeRemote(remote string) string {
	for len(remote) > 0 && strings.HasSuffix(remote, "/") {
		remote = remote[:len(remote)-1]
	}
	return remote
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

func trimSplit(s string, sep string) []string {
	split := strings.Split(s, sep)
	for i, t := range split {
		split[i] = strings.TrimSpace(t)
	}
	return split
}

func trimLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func homeDir() string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return usr.HomeDir
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

// Filter a job slice based on a function
func filterJobs(jobs []gopenqa.Job, f func(job gopenqa.Job) bool) []gopenqa.Job {
	i := 0
	for _, job := range jobs {
		if f(job) {
			jobs[i] = job
			i++
		}
	}
	return jobs[:i]
}

func findJob(jobs []gopenqa.Job, id int) (gopenqa.Job, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	var job gopenqa.Job
	return job, false
}

// Remove duplicate entries based on the job ID. Only the first entries will be kept,
func uniqueJobs(jobs []gopenqa.Job) []gopenqa.Job {
	ret := make([]gopenqa.Job, 0)
	for _, job := range jobs {
		if _, ok := findJob(ret, job.ID); !ok {
			ret = append(ret, job)
		}
	}
	return ret
}
