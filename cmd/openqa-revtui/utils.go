package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"
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
