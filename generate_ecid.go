package main

import (
	"bytes"
	crand "crypto/rand" // Aliased to crand to avoid conflict with math/rand if it were used
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"runtime"
	"text/template"
)

func main() {
	// 1. Generate a random 64-bit integer
	// The Python script generates a random integer N such that 1 <= N < 2**63 - 1.
	// This means N is in the range [1, 2**63 - 2].
	// In Go, math.MaxInt64 is 2**63 - 1.
	// We need a random uint64 in the range [1, math.MaxInt64 - 1].
	
	// Set the upper bound for crypto/rand.Int.
	// crand.Int(reader, N) generates a random value in [0, N-1].
	// To get a number in [0, math.MaxInt64 - 2], N should be math.MaxInt64 - 1.
	upperBoundForRandInt := big.NewInt(0).Sub(big.NewInt(math.MaxInt64), big.NewInt(1)) // This is math.MaxInt64 - 1

	randomBigInt, err := crand.Int(crand.Reader, upperBoundForRandInt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating random number: %v\n", err)
		os.Exit(1)
	}
	// Add 1 to shift the range from [0, math.MaxInt64 - 2] to [1, math.MaxInt64 - 1].
	randomECID := randomBigInt.Uint64() + 1

	// 2. Create the XML plist content with the generated ECID
	plistTemplate := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>ECID</key>
    <integer>{{.ECID}}</integer>
</dict>
</plist>`

	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing plist template: %v\n", err)
		os.Exit(1)
	}

	var xmlBuffer bytes.Buffer
	err = tmpl.Execute(&xmlBuffer, struct{ ECID uint64 }{ECID: randomECID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing plist template: %v\n", err)
		os.Exit(1)
	}
	plistXMLContent := xmlBuffer.String()

	// 3. Determine plist conversion command based on OS
	var plistCommandName string
	var plistCommandArgs []string

	switch runtime.GOOS {
	case "darwin": // macOS
		plistCommandName = "plutil"
		plistCommandArgs = []string{"-convert", "binary1", "-o", "-", "-"}
	case "linux":
		plistCommandName = "plistutil"
		plistCommandArgs = []string{"-i", "-", "-o", "-", "-f", "bin"}
	default:
		fmt.Fprintf(os.Stderr, "ERROR: OS '%s' not supported.\n", runtime.GOOS)
		os.Exit(1)
	}

	// 4. Convert XML plist to binary plist
	cmd := exec.Command(plistCommandName, plistCommandArgs...)
	cmd.Stdin = bytes.NewReader([]byte(plistXMLContent)) // Provide XML content as stdin

	binaryPlistOutput, err := cmd.Output() // Run the command and capture its stdout
	if err != nil {
		// Attempt to get more detailed error from stderr if the command failed
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(os.Stderr, "Error converting plist (command: %s %v): %v\nStderr: %s\n", plistCommandName, plistCommandArgs, err, exitErr.Stderr)
		} else {
			fmt.Fprintf(os.Stderr, "Error converting plist (command: %s %v): %v\n", plistCommandName, plistCommandArgs, err)
		}
		os.Exit(1)
	}

	// 5. Base64 encode it without wrapping
	// Go's base64.StdEncoding.EncodeToString does not add line breaks by default.
	generatedMachineID := base64.StdEncoding.EncodeToString(binaryPlistOutput)

	// Print the result
	fmt.Println(generatedMachineID)
}

