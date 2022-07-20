package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DefaultRemote string   // Default remote to take, if not otherwise defined
	Continuous    int      // If >0, set continuous monitoring with this interval in seconds
	Bell          bool     // bell enabled by default
	Notify        bool     // notify enabled by default
	Follow        bool     // follow jobs by default
	Hierarchy     bool     // show hierarchy
	HideStates    []string // hide the following job states
	Quit          bool     // quit program, once all jobs are completed
	Paused        bool     // Continuous monitoring pased
	RabbitMQ      bool     // Use rabbitmq if possible
	RabbitMQFiles []string // Additional RabbitMQ configuration files to be loaded
}

type RabbitConfig struct {
	Hostname string // OpenQA host this configuration belongs to
	Remote   string // RabbitMQ server
	Queue    string // Queue topic to subscribe on
	Username string // RabbitMQ username
	Password string // RabbitMQ password
}

func strBool(text string) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(text))
	trueValues := []string{"true", "1", "on", "yes", "positive"}
	falseValues := []string{"false", "0", "off", "no", "negative"}
	for _, v := range trueValues {
		if value == v {
			return true, nil
		}
	}
	for _, v := range falseValues {
		if value == v {
			return false, nil
		}
	}
	return true, fmt.Errorf("Illegal bool value")
}

func (cf *Config) SetDefaults() {
	cf.Continuous = 0
	cf.Notify = false
	cf.Bell = false
	cf.Follow = true
	cf.Hierarchy = false
	cf.HideStates = make([]string, 0)
	cf.Quit = false
	cf.RabbitMQ = false // Disabled by default for now
	cf.RabbitMQFiles = make([]string, 0)
}

// readConfig reads file configuration from filename (if exists) and sets the values accordingly
func (cf *Config) ReadFile(filename string) error {
	var err error

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	iLine := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		iLine++
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		i := strings.Index(line, "=")
		if i < 0 {
			return fmt.Errorf("Config file syntax error (Line %d)", iLine)
		}
		name := strings.ToLower(strings.TrimSpace(line[:i]))
		value := strings.TrimSpace(line[i+1:])

		switch name {
		case "defaultremote":
			cf.DefaultRemote = value
		case "bell":
			cf.Bell, err = strBool(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		case "notification", "notify":
			cf.Notify, err = strBool(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		case "follow":
			cf.Follow, err = strBool(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		case "continuous":
			cf.Continuous, err = strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		case "rabbitmq":
			cf.RabbitMQ, err = strBool(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		default:
			return fmt.Errorf("Config file illegal entry (Line %d)", iLine)
		}
	}

	return scanner.Err()
}

// Reads the RabbitMQ configuration from the given filename. Returns an empty slice and nil if the file doesn't exists.
func ReadRabbitMQ(filename string) ([]RabbitConfig, error) {
	ret := make([]RabbitConfig, 0)
	var err error
	var rabbit RabbitConfig // Current rabbit config

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return ret, nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return ret, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	iLine := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		iLine++
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if len(line) > 2 && (line[0] == '[' && line[len(line)-1] == ']') {
			if rabbit.Hostname != "" {
				if rabbit.Remote == "" {
					return ret, fmt.Errorf("empty remote for '%s'", rabbit.Hostname)
				}
				ret = append(ret, rabbit)
			}
			hostname := line[1 : len(line)-1]
			rabbit = RabbitConfig{Hostname: hostname, Remote: hostname}
		} else if line[0] == '[' || line[len(line)-1] == ']' {
			return ret, fmt.Errorf("%s (Line %d)", "Invalid section header", iLine)
		} else {
			// Assume it's a key=value line for the current config

			i := strings.Index(line, "=")
			if i < 0 {
				return ret, fmt.Errorf("Config file syntax error (Line %d)", iLine)
			}
			name := strings.ToLower(strings.TrimSpace(line[:i]))
			value := strings.TrimSpace(line[i+1:])

			switch name {
			case "remote":
				rabbit.Remote = value
			case "queue":
				rabbit.Queue = value
			case "username":
				rabbit.Username = value
			case "password":
				rabbit.Password = value
			}
		}
	}
	if rabbit.Hostname != "" {
		if rabbit.Remote == "" {
			return ret, fmt.Errorf("empty remote for '%s'", rabbit.Hostname)
		}
		ret = append(ret, rabbit)
	}

	return ret, nil
}
