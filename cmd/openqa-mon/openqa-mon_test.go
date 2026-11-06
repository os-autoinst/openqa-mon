package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/os-autoinst/gopenqa"
)

func TestAddRemoteFromCLI(t *testing.T) {
	remotes = make([]Remote, 0)
	testargs := []string{"openqa-mon", "https://openqa.opensuse.org/5701879", "https://openqa.opensuse.org/5701880"}
	err := parseProgramArguments(testargs)
	if err != nil {
		t.Error("Error parsing command line:", err)
	}
	if len(remotes) != 2 {
		t.Error("Expected 2 remotes, got ", len(remotes))
	}
}

func TestAdd2JobsToSameRemote(t *testing.T) {
	remotes = make([]Remote, 0)
	testargs := []string{"openqa-mon", "https://openqa.opensuse.org/t5701879+1"}
	err := parseProgramArguments(testargs)
	if err != nil {
		t.Error("Error parsing command line:", err)
	}
	expected_jobs := []int64{5701879, 5701880}
	if !reflect.DeepEqual(remotes[0].Jobs, expected_jobs) {
		t.Error("Expected,", expected_jobs, "jobs, got", remotes[0].Jobs)
	}
	expected_URI := "https://openqa.opensuse.org"
	if remotes[0].URI != expected_URI {
		t.Error("Expected remote URI=", expected_URI, ",got", remotes[0].URI)
	}
}

func TestRemotesMultipleJobs(t *testing.T) {
	remotes = make([]Remote, 0)
	testargs := []string{"openqa-mon", "https://openqa.suse.de/t100..150", "https://openqa.opensuse.org/t19999+1"}
	err := parseProgramArguments(testargs)
	if err != nil {
		t.Error("Error parsing command line:", err)
	}
	if len(remotes) != 2 {
		t.Error("Expected 2 remotes, got ", len(remotes))
	}
	totalJobs := len(remotes[0].Jobs) + len(remotes[1].Jobs)
	if totalJobs != 53 {
		t.Error("Expected 53 jobs, got", totalJobs)
	}
}

func TestShouldGiveError(t *testing.T) {
	tests := []struct {
		input       []string
		expectedmsg string
	}{
		{
			[]string{"openqa-mon", "--jobs", "foo,bar,baz"},
			"jobs need to be defined after a remote instance",
		},
		{
			[]string{"openqa-mon", "https://openqa.opensuse.org", "--jobs", "tfoobar"},
			"illegal job identifier: tfoobar",
		},
		{
			[]string{"openqa-mon", "https://openqa.opensuse.org", "--jobs", "-31415"},
			"missing job IDs",
		},
	}
	for _, tc := range tests {
		remotes = make([]Remote, 0)
		err := parseProgramArguments(tc.input)
		if err.Error() != tc.expectedmsg {
			t.Error("Expected", tc.expectedmsg, "got:", err)
		}
	}
}

func TestVisibleJobsHidesBeforePagination(t *testing.T) {
	tui := CreateTUI()
	tui.Model.HideStates = []string{"passed"}
	for i := 0; i < 30; i++ {
		tui.Model.jobs = append(tui.Model.jobs, gopenqa.Job{ID: int64(i), State: "done", Result: "passed"})
	}
	for i := 30; i < 33; i++ {
		tui.Model.jobs = append(tui.Model.jobs, gopenqa.Job{ID: int64(i), State: "running"})
	}
	jobs := tui.visibleJobs()
	start, end, total, _ := pageLayout(len(jobs), 5, 0)
	if total != 1 || end-start != 3 {
		t.Errorf("expected 3 running jobs on 1 page, got %d jobs on %d pages", end-start, total)
	}
	tui.hideEnable = false
	if n := len(tui.visibleJobs()); n != 33 {
		t.Errorf("expected 33 jobs without hiding, got %d", n)
	}
}

func TestPageLayout(t *testing.T) {
	tests := []struct{ n, per, page, start, end, total, cur int }{
		{10, 4, 0, 0, 4, 3, 0},
		{10, 4, 2, 8, 10, 3, 2},
		{10, 4, 9, 8, 10, 3, 2}, // clamped
		{0, 4, 3, 0, 0, 1, 0},   // empty
		{5, 0, 0, 0, 1, 5, 0},   // at least one job per page
	}
	for _, tc := range tests {
		start, end, total, cur := pageLayout(tc.n, tc.per, tc.page)
		if start != tc.start || end != tc.end || total != tc.total || cur != tc.cur {
			t.Errorf("pageLayout(%d,%d,%d) = %d,%d,%d,%d; want %d,%d,%d,%d", tc.n, tc.per, tc.page, start, end, total, cur, tc.start, tc.end, tc.total, tc.cur)
		}
	}
}

func TestUpdateHeaderPages(t *testing.T) {
	tui := CreateTUI()
	tui.totalPages = 1
	tui.UpdateHeader()
	if strings.Contains(tui.header, "Page") {
		t.Error("single page should not show page info:", tui.header)
	}
	tui.totalPages, tui.currentPage = 3, 1
	tui.UpdateHeader()
	if !strings.Contains(tui.header, "Page 2/3") {
		t.Error("expected 'Page 2/3' in header:", tui.header)
	}
}
