# macvmagt: macOS Virtual Machine Agent
macvmagt is the client-side component of the macvmorx orchestration system. It runs on individual Mac Mini machines and is responsible for reporting node health, managing local VM image caches, and provisioning/deleting macOS virtual machines as instructed by the macvmorx orchestrator. It leverages tart for robust VM operations.

🌟 Features
Heartbeat Reporting: Periodically collects and sends system metrics (CPU, memory, disk usage, running VMs, cached images) to the macvmorx orchestrator.

VM Lifecycle Management: Creates and deletes ephemeral macOS virtual machines using tart.

Intelligent Image Caching: Manages a local cache of macOS VM images (DMG/IPSW) downloaded from GCP Cloud Storage, employing an LRU (Least Recently Used) eviction policy.

On-Demand Image Downloads: Downloads required VM images from GCP in the background, waiting for completion before provisioning a VM if the image is not already cached.

GitHub Runner Integration: Designed to run a post-provisioning script inside new VMs to install and configure GitHub Actions self-hosted runner software with unique names.

Robust System Information: Gathers accurate system resource data for reporting.

🏗️ Architecture Overview
macvmagt is one part of a larger system:
```
+-------------------+       +-------------------+       +-------------------+
|   GitHub Actions  |       |   macvmorx Agent  |       |   macvmorx Agent  |
|     Workflow      |       |  (Mac Mini #1)    |       |  (Mac Mini #N)    |
|                   |       |                   |       |                   |
|  - Request VM     | <---> | - Send Heartbeats | <---> | - Send Heartbeats |
|  - Job Completion |       | - Manage VMs      |       | - Manage VMs      |
|                   |       | - Cache Images    |       | - Cache Images    |
+---------^---------+       +---------^---------+       +---------^---------+
          |                           |                           |
          |                           |                           |
          |                           |                           |
          |                           V                           V
          |             +-------------------------------------------------+
          |             |             macvmorx Orchestrator             |
          |             |        (Go Application - Main Repo)           |
          |             |                                                 |
          |             |  - Receives Heartbeats                          |
          |             |  - Node State Management (In-memory/DB)         |
          |             |  - VM Scheduling Logic                           |
          |             |  - REST API for Agents & Clients                 |
          |             |  - Serves Web Dashboard                          |
          |             +-------------------------------------------------+
          |                           ^
          |                           |
          +---------------------------+
          (VM Provisioning/Deletion Commands)

+-----------------------------------------------------------------------------+
|                            GCP Cloud Storage                                |
|                        (Central VM Image Repository)                        |
+-----------------------------------------------------------------------------+
          ^
          |
          +-----------------------------------------------------------------+
          (Image Downloads by macvmorx Agents)
```

🚀 Getting Started
Prerequisites
Go 1.22+ installed on the Mac Mini.

Git installed.

tart installed on the Mac Mini. You can download releases from Cirrus Labs Tart GitHub. It's recommended to pin a specific version.

GCP Cloud Storage Bucket: A bucket containing your macOS VM images (DMG/IPSW files).

GCP Service Account Key: A JSON key file with "Storage Object Viewer" permissions for your bucket, or configured Application Default Credentials.

SSH Server in VM Images: Your base macOS VM images should have an SSH server enabled and a user configured with SSH keys for the agent to run post-provisioning scripts.

Installation
Clone the repository:
```
git clone https://github.com/your-username/macvmagt.git
cd macvmagt
```

Initialize Go modules:
```
go mod tidy
```

Build the agent executable:
```
go build -o macvmagt ./cmd/macvmagt
```

Create necessary directories:
```
sudo mkdir -p /var/macvmorx/images_cache
sudo mkdir -p /var/macvmorx/vms
# Adjust permissions as needed for the user running the agent
sudo chmod 777 /var/macvmorx/images_cache
sudo chmod 777 /var/macvmorx/vms
```

Install tart: Download the tart binary and place it in your system's PATH (e.g., /usr/local/bin).
```
# Example for a specific version (adjust as needed)
curl -L -o /tmp/tart.zip https://github.com/cirruslabs/tart/releases/download/0.38.0/tart-0.38.0-darwin-arm64.zip # For M-series Macs
# For Intel Macs, use: https://github.com/cirruslabs/tart/releases/download/0.38.0/tart-0.38.0-darwin-x64.zip
sudo unzip /tmp/tart.zip -d /usr/local/bin/
sudo chmod +x /usr/local/bin/tart
```

Deploy the GitHub Runner Post-Script: Copy your install_github_runner.sh.template (or similar) to a known location on the agent machine, e.g., /opt/macvmagt/scripts/.

Configuration
macvmagt can be configured using environment variables or command-line flags. Command-line flags take precedence.

Environment Variable

Flag

Default Value

Description

MACVMORX_AGENT_NODE_ID

--node-id

mac-mini-default

Unique identifier for this specific Mac Mini. Required.

MACVMORX_ORCHESTRATOR_URL

--orchestrator-url

http://localhost:8080

URL of the macvmorx orchestrator.

MACVMORX_HEARTBEAT_INTERVAL

--heartbeat-interval

15s

How often the agent sends heartbeats to the orchestrator.

MACVMORX_IMAGE_CACHE_DIR

--image-cache-dir

/var/macvmorx/images_cache

Directory where VM images will be cached locally.

MACVMORX_MAX_CACHED_IMAGES

--max-cached-images

5

Maximum number of VM images to keep in the local cache (LRU eviction).

MACVMORX_GCS_BUCKET_NAME

--gcs-bucket-name

macvmorx-vm-images

Name of the GCP Cloud Storage bucket containing VM images.

MACVMORX_GCP_CREDENTIALS_PATH

--gcp-credentials-path

""

Path to your GCP service account key JSON file (optional, uses ADC if empty).

Example using environment variables:
```
MACVMORX_AGENT_NODE_ID="mac-mini-001" \
MACVMORX_ORCHESTRATOR_URL="http://your-orchestrator-ip:8080" \
MACVMORX_GCS_BUCKET_NAME="my-vm-images-bucket" \
MACVMORX_GCP_CREDENTIALS_PATH="/path/to/your/gcp-key.json" \
./macvmagt
```

Example using command-line flags:
```
./macvmagt --node-id mac-mini-001 --orchestrator-url http://your-orchestrator-ip:8080 --gcs-bucket-name my-vm-images-bucket
```

Running the Agent
To start the macvmagt:

```
./macvmagt
```

The agent will start sending heartbeats to the orchestrator and listening for VM provisioning/deletion commands on port 8081 (by default).

Running as a launchd Service (Recommended for Production)
For automatic startup on boot and robust process management, you should configure the agent as a launchd service.

Create a plist file (e.g., /Library/LaunchDaemons/com.yourcompany.macvmagt.plist):
```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.yourcompany.macvmagt</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/macvmagt</string> <!-- Adjust path to your executable -->
        <string>--node-id</string>
        <string>mac-mini-001</string> <!-- Set unique Node ID per machine -->
        <string>--orchestrator-url</string>
        <string>http://your-orchestrator-ip:8080</string>
        <string>--gcs-bucket-name</string>
        <string>my-vm-images-bucket</string>
        <!-- Add other flags as needed -->
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>MACVMORX_GCP_CREDENTIALS_PATH</key>
        <string>/path/to/your/gcp-key.json</string> <!-- Or leave empty for ADC -->
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/macvmagt.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/macvmagt.log</string>
    <key>SoftResourceLimits</key>
    <dict>
        <key>NumberOfFiles</key>
        <integer>1024</integer>
    </dict>
</dict>
</plist>
```

Important:

ProgramArguments: Adjust the path to your macvmagt executable.

node-id: Crucially, ensure this is unique for each Mac Mini.

orchestrator-url: Point to your running macvmorx orchestrator.

EnvironmentVariables: Set any necessary environment variables like MACVMORX_GCP_CREDENTIALS_PATH.

Load the launchd service:

sudo cp /path/to/com.yourcompany.macvmagt.plist /Library/LaunchDaemons/
sudo chown root:wheel /Library/LaunchDaemons/com.yourcompany.macvmagt.plist
sudo chmod 644 /Library/LaunchDaemons/com.yourcompany.macvmagt.plist
sudo launchctl load /Library/LaunchDaemons/com.yourcompany.macvmagt.plist

To check status: sudo launchctl list | grep macvmagt
To unload: sudo launchctl unload /Library/LaunchDaemons/com.yourcompany.macvmagt.plist

⚙️ GitHub Runner Post-Script (scripts/install_github_runner.sh.template)
This script is designed to be executed inside the newly provisioned macOS VM. It will download and configure the GitHub Actions self-hosted runner.

#!/bin/bash
# scripts/install_github_runner.sh.template

# This script is meant to be run inside the newly provisioned macOS VM.
# It will download and configure the GitHub Actions self-hosted runner.

# Usage: This script is typically executed remotely via SSH by the macvmagt
# Example: ssh user@<VM_IP_ADDRESS> "bash -s" < /path/to/install_github_runner.sh.template <unique_runner_name>

RUNNER_NAME="$1"
if [ -z "$RUNNER_NAME" ]; then
    echo "Error: Runner name not provided."
    exit 1
fi

GITHUB_OWNER="your-github-org-or-user" # e.g., my-company
GITHUB_REPO="your-github-repo"         # e.g., my-project
RUNNER_HOME="/Users/runner/actions-runner" # Or /opt/actions-runner

echo "Installing GitHub Actions runner with name: ${RUNNER_NAME}"

# 1. Download the latest runner package
# Get the latest runner version URL from GitHub API
RUNNER_VERSION=$(curl -s https://api.github.com/repos/actions/runner/releases/latest | grep -oP '"tag_name": "\Kv\d+\.\d+\.\d+' | head -n 1)
RUNNER_ARCH="arm64" # For Apple Silicon Mac Minis
if [[ $(uname -m) == "x86_64" ]]; then
    RUNNER_ARCH="x64" # For Intel Mac Minis
fi
RUNNER_TARBALL="actions-runner-osx-${RUNNER_ARCH}-${RUNNER_VERSION}.tar.gz"
RUNNER_DOWNLOAD_URL="https://github.com/actions/runner/releases/download/${RUNNER_VERSION}/${RUNNER_TARBALL}"

echo "Downloading runner from: ${RUNNER_DOWNLOAD_URL}"
mkdir -p "${RUNNER_HOME}"
curl -o "${RUNNER_HOME}/${RUNNER_TARBALL}" -L "${RUNNER_DOWNLOAD_URL}"

# 2. Extract the runner
tar xzf "${RUNNER_HOME}/${RUNNER_TARBALL}" -C "${RUNNER_HOME}"
rm "${RUNNER_HOME}/${RUNNER_TARBALL}"

# 3. Configure the runner
cd "${RUNNER_HOME}"

# Get a registration token (requires GitHub PAT with 'repo' scope)
# This token should ideally be passed securely (e.g., from an environment variable
# set by the agent, or fetched from a secrets manager within the VM).
# For simplicity, you might hardcode it here for testing, but NOT for production.
GITHUB_RUNNER_TOKEN="YOUR_GITHUB_RUNNER_REGISTRATION_TOKEN" # REPLACE THIS WITH A SECURE TOKEN!

echo "Configuring runner..."
./config.sh --url "https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}" \
            --token "${GITHUB_RUNNER_TOKEN}" \
            --name "${RUNNER_NAME}" \
            --labels "macos,${RUNNER_ARCH},ephemeral" \
            --unattended \
            --replace # Important for ephemeral runners to replace existing with same name

# 4. Install and start as a service (optional, but good for consistent behavior)
# This will set up a launchd service inside the VM.
echo "Installing runner as a service..."
sudo ./svc.sh install
sudo ./svc.sh start

echo "GitHub Actions runner '${RUNNER_NAME}' configured and started."

# Important: The agent needs to know when the GitHub job is truly "done"
# so it can signal the orchestrator to delete the VM. This typically involves
# the GitHub workflow itself signaling back to the orchestrator's API
# or the agent monitoring the runner's status (e.g., by checking GitHub API).

🤝 Contributing
Contributions are welcome! Please feel free to open issues, submit pull requests, or suggest improvements.

📄 License
This project is licensed under the MIT License - see the LICENSE file for details.


GCP Credentials (Important!):

Ensure your Mac Minis have access to GCP Cloud Storage. The easiest way is to create a GCP Service Account key (JSON file) with "Storage Object Viewer" and "Storage Object Creator" roles for your bucket.

Set the ```MACVMORX_GCP_CREDENTIALS_PATH``` environment variable to the path of this JSON file. If left empty, it will attempt to use Application Default Credentials (e.g., gcloud auth application-default login).

Create Image Cache Directory:

```bash
sudo mkdir -p /var/macvmorx/images_cache
sudo chmod 777 /var/macvmorx/images_cache # Adjust permissions as needed for the agent user
sudo mkdir -p /var/macvmorx/vms
sudo chmod 777 /var/macvmorx/vms # Adjust permission
```