#!/bin/bash

# Interval in seconds between each datapoint
interval=1
# Output file
OUTPUT_FILE="./output.json"

cpu_tmp_file=$(mktemp)
ram_tmp_file=$(mktemp)
disk_tmp_file=$(mktemp)

# Disk is either /dev/root if its available, or the largest disk
if [ -b /dev/root ]; then
  disk="/dev/root"
else
  disk=$(lsblk -lnbdo NAME,SIZE | sort -k2 -nr | awk 'NR==1{print "/dev/" $1}')
fi

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "jq could not be found. Please install jq to run this script."
    exit 1
fi

# Check if mpstat is installed
if ! command -v mpstat &> /dev/null; then
    echo "mpstat could not be found. Please install sysstat to run this script."
    exit 1
fi

hasReportedStarted=false
hasReportedStopped=false
while true; do
    if [ ! -f "./run" ]; then
      hasReportedStarted=false
      if [ "$hasReportedStopped" = false ]; then
        echo "The script is not running. Create a file named 'run' to start the script."
        hasReportedStopped=true
      fi

      sleep $interval
      continue
    fi

    hasReportedStopped=false
    if [ "$hasReportedStarted" = false ]; then
      echo "The script is running. Delete the file named 'run' to stop the script."
      hasReportedStarted=true
    fi

    (mpstat -P ALL "$interval" 1 | awk '/^[0-9]/ {if (NR>1) print $3/100}' | jq -R -s -c 'split("\n")[1:-1]' | jq '[.[] | gsub(","; ".") | tonumber]' > "$cpu_tmp_file") &

    (free | awk '/Mem:/ {print $3/$2}' | sed 's/,/./' > "$ram_tmp_file") &

    (iostat -dx "$disk" 1 2 | awk '/Device/{flag=1; next} flag && /^[^ ]/ {util=$NF} END{print util/100}' | sed 's/,/./' > "$disk_tmp_file") &
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    wait

    name=$(hostname)
    cpu_core_utilization=$(cat "$cpu_tmp_file")
    cpu_utilization_avg=$(echo "$cpu_core_utilization" | jq 'add/length')
    ram_utilization=$(cat "$ram_tmp_file")
    disk_utilization=$(cat "$disk_tmp_file")

    # Create the JSON object for the current datapoint
    datapoint=$(jq -n \
                  --argjson cpuCores "$cpu_core_utilization" \
                  --argjson cpuAverage "$cpu_utilization_avg" \
                  --arg name "$name" \
                  --arg ramUtilization "$ram_utilization" \
                  --arg diskUtilization "$disk_utilization" \
                  --arg timestamp "$timestamp" \
                  '{name: $name, cpu: {cores: $cpuCores, average: $cpuAverage}, ram: ($ramUtilization | tonumber), disk: ($diskUtilization | tonumber), timestamp: $timestamp}')

    # Initialize the output file if it does not exist
    if [ ! -f "$OUTPUT_FILE" ]; then
        echo "[]" > "$OUTPUT_FILE"
    fi

    # Append the datapoint to the output file
    jq --argjson newData "$datapoint" '. += [$newData]' "$OUTPUT_FILE" > tmp.$$.json && mv tmp.$$.json "$OUTPUT_FILE"

    sleep $interval
done