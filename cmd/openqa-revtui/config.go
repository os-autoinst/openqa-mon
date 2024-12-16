package main

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

/* Group is a single configurable monitoring unit. A group contains all parameters that will be queried from openQA */
type Group struct {
	Name        string
	Params      map[string]string // Default parameters for query
	MaxLifetime int64             // Ignore entries that are older than this value in seconds from now
}

/* Program configuration parameters */
type Config struct {
	Name            string            // Configuration name, if set
	Instance        string            // Instance URL to be used
	RabbitMQ        string            // RabbitMQ url to be used
	RabbitMQTopic   string            // Topic to subscribe to
	DefaultParams   map[string]string // Default parameters
	HideStatus      []string          // Status to hide
	Notify          bool              // Notify on job status change
	RefreshInterval int64             // Periodic refresh delay in seconds
	Groups          []Group           // Groups that will be monitord
	MaxJobs         int               // Maximum number of jobs per group to consider
	GroupBy         string            // Display group mode: "none", "groups"
	RequestJobLimit int               // Maximum number of jobs in a single request
}

func (cf Config) Validate() error {
	if len(cf.Groups) == 0 {
		return fmt.Errorf("no review groups defined")
	}
	return nil
}

func (cf *Config) LoadToml(filename string) error {
	if _, err := toml.DecodeFile(filename, cf); err != nil {
		return err
	}
	// Apply default parameters to group after loading
	for i, group := range cf.Groups {
		for k, v := range cf.DefaultParams {
			if _, exists := group.Params[k]; exists {
				continue
			} else {
				group.Params[k] = v
			}
		}
		// Apply parameter macros
		for k, v := range group.Params {
			param := parseParameter(v)
			if strings.Contains(param, "%") {
				return fmt.Errorf("invalid parameter macro in %s", param)
			}
			group.Params[k] = param
		}
		cf.Groups[i] = group
	}
	// Apply filename as name, if no name is set
	if cf.Name == "" {
		cf.Name = extractFilename(filename)
	}
	return cf.Validate()
}

/* Create configuration instance and set default vaules */
func CreateConfig() Config {
	var cf Config
	cf.Instance = "https://openqa.opensuse.org"
	cf.RabbitMQ = "amqps://opensuse:opensuse@rabbit.opensuse.org"
	cf.RabbitMQTopic = "opensuse.openqa.job.done"
	cf.HideStatus = make([]string, 0)
	cf.Notify = true
	cf.RefreshInterval = 30
	cf.DefaultParams = make(map[string]string, 0)
	cf.Groups = make([]Group, 0)
	cf.MaxJobs = 20
	cf.RequestJobLimit = 100
	return cf
}
