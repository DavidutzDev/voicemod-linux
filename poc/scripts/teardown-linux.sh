#!/usr/bin/env bash
# Unload the virtual microphone and remove the FIFO.
set -euo pipefail

FIFO="${FIFO:-/tmp/voicemod.fifo}"
SOURCE_NAME="${SOURCE_NAME:-voicemod}"

# Unload every module-pipe-source matching our source name.
pactl list modules short \
    | awk -v name="source_name=$SOURCE_NAME" '$0 ~ name {print $1}' \
    | while read -r id; do
        pactl unload-module "$id" && echo "[+] unloaded module id=$id"
    done

rm -f "$FIFO" && echo "[+] removed $FIFO"
