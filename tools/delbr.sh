#!/bin/bash

# Get a list of all bridges starting with "br-"
bridges=$(brctl show | awk '{print $1}' | grep '^br-')

# Loop through each bridge and delete it
for bridge in $bridges; do
  echo "Deleting bridge: $bridge"
  ip link set $bridge down  # Bring the bridge down
  brctl delbr $bridge       # Delete the bridge
done

echo "All br- bridges have been deleted."