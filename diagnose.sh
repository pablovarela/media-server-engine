#!/usr/bin/env bash
{
echo "===== UPTIME / LOAD ====="
uptime

echo; echo "===== MEMORY (human) ====="
free -h

echo; echo "===== MEMORY (raw kB, for %) ====="
free

echo; echo "===== SWAP DEVICES ====="
swapon --show
cat /proc/swaps

echo; echo "===== SWAPPINESS ====="
cat /proc/sys/vm/swappiness

echo; echo "===== dphys-swapfile CONFIG ====="
grep -v '^#' /etc/dphys-swapfile 2>/dev/null | grep -v '^$' || echo "(no dphys-swapfile config)"

echo; echo "===== SWAP ACTIVITY (vmstat, watch si/so) ====="
vmstat 1 5

echo; echo "===== TOP 10 BY MEMORY (RSS) ====="
ps -eo pid,comm,%mem,rss --sort=-rss | head -n 11

echo; echo "===== TOP 10 BY SWAP USAGE ====="
for f in /proc/[0-9]*/status; do
  awk '/^VmSwap:/{s=$2} /^Name:/{n=$2} END{if(s>0) printf "%8d kB  %s\n", s, n}' "$f" 2>/dev/null
done | sort -rn | head -n 10

echo; echo "===== DISK / SSD USAGE ====="
df -h

echo; echo "===== BLOCK DEVICES ====="
lsblk -o NAME,SIZE,TYPE,MOUNTPOINT,ROTA

echo; echo "===== MODEL / RAM ====="
cat /proc/device-tree/model 2>/dev/null; echo
grep MemTotal /proc/meminfo

echo; echo "===== PRESSURE STALL (memory), if available ====="
cat /proc/pressure/memory 2>/dev/null || echo "(PSI not available)"

echo; echo "===== DONE ====="
} 2>&1
