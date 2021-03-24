package main

import "testing"

func TestAddJobs(t *testing.T) {
    args_t := []string{"https://openqa.opensuse.org", "--jobs", "5701879,5701880"}
    remotes_t := make([]Remote, 0)
    j := []int{1, 2, 3}
    r1 := Remote{"http://openqa.opensuse.org", j}
    remotes_t = append(remotes_t, r1)
    parseArgs(args_t, &remotes_t)
    actual_len := len(remotes_t[0].Jobs)
    expected_jobs := 3
    if (actual_len != expected_jobs) {
      t.Error("Expected", expected_jobs, "got ", actual_len)
    }
}
