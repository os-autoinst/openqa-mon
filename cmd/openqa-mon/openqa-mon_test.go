package main

import (
	"reflect"
	"testing"
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
