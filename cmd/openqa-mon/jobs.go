package main

import (
	"strconv"
	"strings"

	"github.com/grisu48/gopenqa"
)

/* Struct for sorting job slice by job id */
type byID []gopenqa.Job

func (s byID) Len() int {
	return len(s)
}
func (s byID) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byID) Less(i, j int) bool {
	return s[i].ID < s[j].ID

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

func jobsMap(jobs []gopenqa.Job) map[int]gopenqa.Job {
	ret := make(map[int]gopenqa.Job, 0)
	for _, job := range jobs {
		ret[job.ID] = job
	}
	return ret
}

func containsJobID(jobs []gopenqa.Job, ID int) bool {
	for _, job := range jobs {
		if job.ID == ID {
			return true
		}
	}
	return false
}

/* jobsChanged returns the jobs that are in a different state between the two sets */
func jobsChanged(jobs1 []gopenqa.Job, jobs2 []gopenqa.Job) []gopenqa.Job {
	ret := make([]gopenqa.Job, 0)
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
