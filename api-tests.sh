#!/bin/bash

TARGET_SYSTEM="192.168.1.25"

curl -X GET http://$TARGET_SYSTEM:8081/vms

VM_ID="test-runner-12345"

curl -X POST http://$TARGET_SYSTEM:8081/provision-vm \
     -H "Content-Type: application/json" \
     -d '{
        "vmId": "'"${VM_ID}"'",
        "imageName": "ventura-golden",
        "runnerRegistrationToken": "GH-TOKEN-ABCDEFG",
        "runnerName": "macvm-runner-001",
        "runnerLabels": ["macos", "arm64", "2cpu", "8gb"],
        "localImagePath": "/path/to/your/image.img"
     }'

curl -X GET http://$TARGET_SYSTEM:8081/vms

VM_ID="test-runner-12345"

curl -X POST http://$TARGET_SYSTEM:8081/delete-vm \
    -H "Content-Type: application/json" \
    -d '{
        "vmId": "'"${VM_ID}"'"
    }'

curl -X GET http://$TARGET_SYSTEM:8081/vms
