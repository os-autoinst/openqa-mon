package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"
)

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

func homeDir() string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return usr.HomeDir
}

// Split a NAME=VALUE string
func splitNV(v string) (string, string, error) {
	i := strings.Index(v, "=")
	if i < 0 {
		return "", "", fmt.Errorf("no separator")
	}
	return v[:i], v[i+1:], nil
}

// Parse additional parameter macros
func parseParameter(param string) string {
	if strings.Contains(param, "%today%") {
		today := time.Now().Format("20060102")
		param = strings.ReplaceAll(param, "%today%", today)
	}
	if strings.Contains(param, "%yesterday%") {
		today := time.Now().AddDate(0, 0, -1).Format("20060102")
		param = strings.ReplaceAll(param, "%yesterday%", today)
	}

	return param
}

// Returns the remote host from a RabbitMQ URL
func rabbitRemote(remote string) string {
	i := strings.Index(remote, "@")
	if i > 0 {
		return remote[i+1:]
	}
	return remote
}
