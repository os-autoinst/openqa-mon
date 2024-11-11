/* Small utility functions, moved here to keep the main code tidy */
package main

import (
	"os/user"
	"strconv"
	"strings"

	"github.com/os-autoinst/gopenqa"
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

func unique[T comparable](slice []T) []T {
	seen := make(map[T]struct{})
	unique := []T{}
	for _, item := range slice {
		if _, exists := seen[item]; !exists {
			seen[item] = struct{}{}
			unique = append(unique, item)
		}
	}
	return unique
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

func createInt64Range(min int64, max int64, offset int64) []int64 {
	// create with capacity to avoid runtime reallocation
	ret := make([]int64, 0, (max-min)+1)
	for i := min; i <= max; i++ {
		ret = append(ret, i+offset)
	}
	return ret
}

// parseJobIDs parses the given text for a valid job id ("[#]INTEGER[:]" and INTEGER > 0) or job id ranges (MIN..MAX). Returns the job id if valid or 0 on error
func parseJobIDs(parseText string) []int64 {
	ret := make([]int64, 0)
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
		return createInt64Range(min, max, 0)
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
		return createInt64Range(0, int64(steps), start)
	}

	// Assume job ID set, which also covers single jobs IDs
	split := strings.Split(parseText, ",")
	for _, s := range split {
		i := parseJobID(s)
		if i > 0 {
			ret = append(ret, i)
		}
	}
	return ret
}

// parseJobID parses the given text for a valid job id ("[.*#]INTEGER[:]" and INTEGER > 0). Returns the job id if valid or 0 on error
func parseJobID(parseText string) int64 {
	// Remove possible fragment in case the user dumped a part of a url
	parseText = removeFragment(parseText)
	// Remove : at the end
	for len(parseText) > 1 && parseText[len(parseText)-1] == ':' {
		parseText = parseText[:len(parseText)-1]
	}
	if num, err := strconv.Atoi(parseText); err != nil || num <= 0 {
		return 0
	} else {
		return int64(num)
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

func findJob(jobs []gopenqa.Job, id int64) (gopenqa.Job, bool) {
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
