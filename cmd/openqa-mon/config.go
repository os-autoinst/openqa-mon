package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DefaultRemote string // Default remote to take, if not otherwise defined
	Continuous    int    // If >0, set continuous monitoring with this interval in seconds
	Bell          bool   // bell enabled by default
	Notify        bool   // notify enabled by default
	Follow        bool   // follow jobs by default
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

// readConfig reads file configuration from filename (if exists) and sets the values in config accordingly
func readConfig(filename string, config *Config) error {
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
			config.DefaultRemote = value
		case "bell":
			config.Bell, err = strBool(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		case "notification", "notify":
			config.Notify, err = strBool(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		case "follow":
			config.Follow, err = strBool(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		case "continuous":
			config.Continuous, err = strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s (Line %d)", err, iLine)
			}
		default:
			return fmt.Errorf("Config file illegal entry (Line %d)", iLine)
		}
	}

	return scanner.Err()
}
