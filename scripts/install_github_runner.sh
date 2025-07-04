#!/bin/bash
# scripts/install_github_runner.sh.template

# This script is meant to be run inside the newly provisioned macOS VM.
# It will download and configure the GitHub Actions self-hosted runner.

# Usage: ./install_github_runner.sh.template <unique_runner_name>

RUNNER_NAME="$1"
if [ -z "$RUNNER_NAME" ]; then
    echo "Usage: $0 <unique_runner_name>"
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
# This token should ideally be passed securely or fetched from a secrets manager.
# For simplicity, you might hardcode it here for testing, but NOT for production.
# Or, the orchestrator could fetch it and pass it to the agent, which then passes to VM.
GITHUB_RUNNER_TOKEN="YOUR_GITHUB_RUNNER_REGISTRATION_TOKEN" # REPLACE THIS!

echo "Configuring runner..."
./config.sh --url "https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}" \
            --token "${GITHUB_RUNNER_TOKEN}" \
            --name "${RUNNER_NAME}" \
            --labels "macos,${RUNNER_ARCH},ephemeral" \
            --unattended \
            --replace # Important for ephemeral runners to replace existing with same name

# 4. Install and start as a service (optional, but good for consistent behavior)
# This will set up a launchd service.
echo "Installing runner as a service..."
sudo ./svc.sh install
sudo ./svc.sh start

echo "GitHub Actions runner '${RUNNER_NAME}' configured and started."

# Important: The agent needs to know when the GitHub job is truly "done"
# so it can signal the orchestrator to delete the VM. This typically involves
# the GitHub workflow itself signaling back to the orchestrator's API
# or the agent monitoring the runner's status.