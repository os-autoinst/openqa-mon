package main

import (
	"math"
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

func TestJobLimit(t *testing.T) {
	config.SetDefaults()
	defer config.SetDefaults()

	remotes := []Remote{{Jobs: createInt64Range(1, maxJobsPerRun, 0)}}
	if err := checkJobLimit(remotes); err != nil {
		t.Error("Expected", maxJobsPerRun, "jobs to be accepted, got:", err)
	}

	remotes[0].Jobs = append(remotes[0].Jobs, maxJobsPerRun+1)
	if err := checkJobLimit(remotes); err == nil {
		t.Error("Expected more than", maxJobsPerRun, "jobs to be refused")
	}

	config.DosProtection = false
	if err := checkJobLimit(remotes); err != nil {
		t.Error("Expected --disable-dos-protection to accept the jobs, got:", err)
	}
}

func TestJobLimitAcrossRemotesAndDuplicates(t *testing.T) {
	config.SetDefaults()
	defer config.SetDefaults()

	remotes := []Remote{
		{Jobs: createInt64Range(1, 60, 0)},
		{Jobs: createInt64Range(1000, 1059, 0)},
	}
	if err := checkJobLimit(remotes); err == nil {
		t.Error("Expected the job limit to apply across all remotes")
	}

	duplicates := make([]int64, 200)
	for i := range duplicates {
		duplicates[i] = 1
	}
	remotes = []Remote{{Jobs: duplicates}}
	if err := checkJobLimit(remotes); err != nil {
		t.Error("Expected duplicate job IDs to be ignored, got:", err)
	}
}

func TestJobLimitFlagOrder(t *testing.T) {
	defer config.SetDefaults()
	for _, args := range [][]string{
		{"openqa-mon", "--disable-dos-protection", "https://openqa.opensuse.org/t1..1000"},
		{"openqa-mon", "https://openqa.opensuse.org/t1..1000", "--disable-dos-protection"},
	} {
		config.SetDefaults()
		remotes = make([]Remote, 0)
		if err := parseProgramArguments(args); err != nil {
			t.Fatal("Error parsing command line:", err)
		}
		if err := checkJobLimit(remotes); err != nil {
			t.Error("Expected --disable-dos-protection to accept the jobs, got:", err)
		}
		if len(remotes) != 1 || len(remotes[0].Jobs) != 1000 {
			t.Errorf("Expected all 1000 jobs, got %d remotes", len(remotes))
		}
	}
}

func TestJobLimitWithRanges(t *testing.T) {
	config.SetDefaults()
	defer config.SetDefaults()
	remotes = make([]Remote, 0)

	if err := parseProgramArguments([]string{"openqa-mon", "https://openqa.opensuse.org/t1..100"}); err != nil {
		t.Fatal("Error parsing command line:", err)
	}
	if err := checkJobLimit(remotes); err != nil {
		t.Error("Expected exactly", maxJobsPerRun, "jobs to be accepted, got:", err)
	}

	remotes = make([]Remote, 0)
	if err := parseProgramArguments([]string{"openqa-mon", "https://openqa.opensuse.org/t1..1000"}); err != nil {
		t.Fatal("Error parsing command line:", err)
	}
	if err := checkJobLimit(remotes); err == nil {
		t.Error("Expected a large range to be refused")
	}
}

func TestInvalidAndHugeRangesDoNotPanic(t *testing.T) {
	config.SetDefaults()
	defer config.SetDefaults()

	for _, input := range []string{
		"1..9223372036854775807",
		"9223372036854775807..9223372036854775807",
		"1+999999999999999999999",
		"1+-5",
	} {
		if jobs := parseJobIDs(input); len(jobs) > maxJobsPerRun+1 {
			t.Errorf("%q: parsed too many jobs: %d", input, len(jobs))
		}
	}
}

func TestCreateInt64RangeOverflow(t *testing.T) {
	config.SetDefaults()
	defer config.SetDefaults()

	maximum := int64(math.MaxInt64)

	jobs := createInt64Range(maximum-150, maximum, 0)
	if len(jobs) != maxJobsPerRun+1 || jobs[0] != maximum-150 || jobs[len(jobs)-1] != maximum-50 {
		t.Errorf("Expected capped range %d..%d, got %v", maximum-150, maximum-50, jobs)
	}

	jobs = createInt64Range(maximum-1, math.MinInt64+100, 0)
	if len(jobs) != 0 {
		t.Error("Expected inverted range to be rejected, got", len(jobs), "jobs")
	}
}

func TestParseJobRangesAtInt64Boundary(t *testing.T) {
	config.SetDefaults()
	defer config.SetDefaults()

	maximum := int64(math.MaxInt64)
	tests := []struct {
		input string
		want  []int64
	}{
		{"9223372036854775806..9223372036854775807", []int64{maximum - 1, maximum}},
		{"9223372036854775807..9223372036854775807", []int64{maximum}},
		{"9223372036854775805+2", []int64{maximum - 2, maximum - 1, maximum}},
		{"9223372036854775807+0", []int64{maximum}},
		{"9223372036854775807+1", []int64{}},
		{"9223372036854775757+100", []int64{}},
	}
	for _, tc := range tests {
		if got := parseJobIDs(tc.input); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: expected %v, got %v", tc.input, tc.want, got)
		}
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
