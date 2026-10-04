#!/bin/bash
ENGINE_DIR=${ENGINE_DIR:-$(cd "$(dirname "$0")/.." && pwd)}
DATA_DIR=${DATA_DIR:-$(dirname "$ENGINE_DIR")/data}

LOGDIR="$DATA_DIR/logs/diskwatch"
INTERVAL=${DISKWATCH_INTERVAL:-5}
READ_BURST_MB_PER_INTERVAL=80

mkdir -p "$LOGDIR"

exec 9>"$LOGDIR/.lock"
flock -n 9 || exit 0

declare -A prev_read
prev_iowait=0
prev_total=0
iowait_percent=0
day=""
samples_since_reader_history_reset=0

sample_iowait() {
  local user nice system idle iowait total
  read -r _ user nice system idle iowait _ < /proc/stat
  total=$((user + nice + system + idle + iowait))
  if [ "$((total - prev_total))" -gt 0 ]; then
    iowait_percent=$(( (iowait - prev_iowait) * 100 / (total - prev_total) ))
  fi
  prev_iowait=$iowait
  prev_total=$total
}

sample_memory() {
  local key value
  while read -r key value _; do
    case $key in
      MemAvailable:) memavail_mb=$((value / 1024)) ;;
      SwapFree:) swapfree_mb=$((value / 1024)) ;;
    esac
  done < /proc/meminfo
}

read_bytes_of() {
  local key value
  { while read -r key value; do
      if [ "$key" = read_bytes: ]; then read_bytes=$value; return 0; fi
    done < "$1/io"; } 2>/dev/null
  return 1
}

sample_iowait

while :; do
  now=$(date '+%F %H:%M:%S')
  today=${now%% *}
  log="$LOGDIR/diskwatch-$today.log"

  if [ "$today" != "$day" ]; then
    day="$today"
    find "$LOGDIR" -name 'diskwatch-*.log' -mtime +7 -delete 2>/dev/null
  fi

  samples_since_reader_history_reset=$((samples_since_reader_history_reset+1))
  if [ "$samples_since_reader_history_reset" -ge 720 ]; then unset prev_read; declare -A prev_read; samples_since_reader_history_reset=0; fi

  read -r load1 _ < /proc/loadavg
  sample_memory
  sample_iowait

  readers=""
  for p in /proc/[0-9]*; do
    pid=${p#/proc/}
    read_bytes_of "$p" || continue
    d=$(( read_bytes - ${prev_read[$pid]:-$read_bytes} ))
    prev_read[$pid]=$read_bytes
    if [ "$d" -ge $((READ_BURST_MB_PER_INTERVAL*1000000)) ]; then
      read -r comm < "$p/comm" 2>/dev/null
      readers+=" ${comm:-?}:$((d/1000000))MB"
    fi
  done

  printf '%s load=%s memavail=%sMB swapfree=%sMB iowait=%s%% readers=[%s]\n' \
    "$now" "$load1" "$memavail_mb" "$swapfree_mb" "$iowait_percent" "$readers" >> "$log"

  sleep "$INTERVAL"
done
