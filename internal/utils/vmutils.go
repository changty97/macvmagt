// macvmagt/internal/utils/vmutils.go (Conceptual with Tart)
package utils

import (
	// For parsing tart list output
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// GetVMIPAddress attempts to get the IP address of a running Tart VM.
// This is a simplified example. In a real scenario, you might need to
// parse `tart list --json` output or use Bonjour/mDNS.
func GetVMIPAddress(vmID string) (string, error) {
	// For tart, typically the VM gets an IP on the 'bridge' network.
	// You might need to query `tart list` and parse its JSON output
	// to find the assigned IP. This is a placeholder.
	// Example: `tart list --json` and look for NetworkInterfaces[0].IPAddress
	stdout, stderr, err := RunCommand("tart", "list", "--json")
	if err != nil {
		return "", fmt.Errorf("failed to list tart VMs: %v, stderr: %s", err, stderr)
	}

	// This is a simplified JSON parsing. For robust parsing, use encoding/json
	// and unmarshal into a struct matching tart's output.
	// Assuming a simple output where IP is easily extractable for the given VMID.
	// A more robust solution would involve unmarshaling the JSON output.
	// For now, let's simulate by looking for a pattern.
	// This is a very fragile parsing, a real implementation needs proper JSON unmarshaling.
	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		if strings.Contains(line, vmID) && strings.Contains(line, "IPAddress") {
			// This is a naive attempt; proper JSON parsing is required.
			// Example: "IPAddress": "192.168.64.2"
			if ipIndex := strings.Index(line, `"IPAddress": "`); ipIndex != -1 {
				ipStart := ipIndex + len(`"IPAddress": "`)
				ipEnd := strings.Index(line[ipStart:], `"`)
				if ipEnd != -1 {
					ip := line[ipStart : ipStart+ipEnd]
					log.Printf("Found VM IP for %s: %s", vmID, ip)
					return ip, nil
				}
			}
		}
	}

	return "", fmt.Errorf("VM IP address not found for VMID: %s", vmID)
}

// WaitForSSHReady waits for the SSH server in the VM to be ready.
func WaitForSSHReady(ip string, port string, user string, keyPath string, timeout time.Duration) error {
	addr := net.JoinHostPort(ip, port)
	log.Printf("Waiting for SSH on %s...", addr)

	signer, err := getSSHSigner(keyPath)
	if err != nil {
		return fmt.Errorf("failed to get SSH signer: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // WARNING: Insecure for production, use ssh.FixedHostKey or known_hosts
		Timeout:         5 * time.Second,             // Timeout for individual dial attempts
	}

	startTime := time.Now()
	for time.Since(startTime) < timeout {
		client, err := ssh.Dial("tcp", addr, config)
		if err == nil {
			client.Close()
			log.Printf("SSH on %s is ready.", addr)
			return nil
		}
		log.Printf("SSH not ready on %s: %v. Retrying...", addr, err)
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("SSH not ready on %s after %s timeout", addr, timeout)
}

// ExecuteSSHCommand executes a command over SSH on the VM.
func ExecuteSSHCommand(ip string, port string, user string, keyPath string, command string) (string, string, error) {
	addr := net.JoinHostPort(ip, port)
	log.Printf("Executing SSH command on %s: %s", addr, command)

	signer, err := getSSHSigner(keyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to get SSH signer: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // WARNING: Insecure for production
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", "", fmt.Errorf("failed to dial SSH: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	err = session.Run(command)
	if err != nil {
		return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("SSH command failed: %w, stdout: %s, stderr: %s", err, stdoutBuf.String(), stderrBuf.String())
	}
	log.Printf("SSH command success. Stdout: %s", stdoutBuf.String())
	return stdoutBuf.String(), stderrBuf.String(), nil
}

// getSSHSigner loads the private key for SSH authentication.
func getSSHSigner(keyPath string) (ssh.Signer, error) {
	key, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("unable to parse private key: %w", err)
	}
	return signer, nil
}
