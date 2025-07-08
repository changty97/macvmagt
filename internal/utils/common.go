package utils

import (
	"bytes"
	"log"
	"os/exec"
	"strings"
)

// RunCommand executes a shell command and returns its stdout, stderr, and an error.
func RunCommand(name string, arg ...string) (string, string, error) {
	cmd := exec.Command(name, arg...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("Executing command: %s %s", name, strings.Join(arg, " "))
	err := cmd.Run()
	if err != nil {
		log.Printf("Command failed: %s %s, Error: %v, Stdout: %s, Stderr: %s", name, strings.Join(arg, " "), err, stdout.String(), stderr.String())
	} else {
		log.Printf("Command succeeded: %s %s, Stdout: %s", name, strings.Join(arg, " "), stdout.String())
	}
	return stdout.String(), stderr.String(), err
}
