package utils

import (
	"log"
	"os/exec"
)

// ExecuteCommand runs a shell command and returns its output.
func ExecuteCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error executing command '%s %v': %s, Error: %v", name, args, string(output), err)
		return "", err
	}
	return string(output), nil
}
