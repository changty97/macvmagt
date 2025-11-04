#!/bin/bash
go build -o macvmagt ./cmd/macvmagt
# ./macvmagt # This will start the agent

# MACVMORX_AGENT_NODE_ID="mac-mini-001" MACVMORX_ORCHESTRATOR_URL="http://localhost:8080" ./macvmagt