/* openQA related methods and functions for revtui */
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/os-autoinst/gopenqa"
)

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
		return false, err
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

/* Fetch the given jobs and follow their clones */
func fetchJobsFollow(ids []int64, instance *gopenqa.Instance) ([]gopenqa.Job, error) {
	// Obey the maximum number of job per requests.
	// We split the job ids into multiple requests if necessary
	jobs := make([]gopenqa.Job, 0)
	for len(ids) > 0 {
		n := min(cf.RequestJobLimit, len(ids))
		chunk, err := instance.GetJobsFollow(ids[:n])
		ids = ids[n:]
		if err != nil {
			return jobs, err
		}
		jobs = append(jobs, chunk...)
	}

	return jobs, nil
}

/* Fetch the given jobs from the instance at once */
func fetchJobs(ids []int64, instance *gopenqa.Instance) ([]gopenqa.Job, error) {
	// Obey the maximum number of job per requests.
	// We split the job ids into multiple requests if necessary
	jobs := make([]gopenqa.Job, 0)
	for len(ids) > 0 {
		n := min(cf.RequestJobLimit, len(ids))
		chunk, err := instance.GetJobs(ids[:n])
		ids = ids[n:]
		if err != nil {
			return jobs, err
		}
		jobs = append(jobs, chunk...)
	}

	// Get cloned jobs, if present
	for i, job := range jobs {
		if job.IsCloned() {
			if job, err := FetchJob(job.ID, instance); err != nil {
				return jobs, err
			} else {
				jobs[i] = job
			}
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
