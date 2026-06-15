#!/usr/bin/env bash
# Create the FIFO and load a PipeWire/PulseAudio virtual microphone that reads
# from it. Run this BEFORE starting the receiver (the receiver blocks until the
# module opens the FIFO's read end).
#
# Works on PipeWire via its pulse compat layer (pactl) — the default on modern
# distros. Verify with: pactl info | grep 'Server Name'
set -euo pipefail

FIFO="${FIFO:-/tmp/voicemod.fifo}"
RATE="${RATE:-48000}"
CHANNELS="${CHANNELS:-2}"
SOURCE_NAME="${SOURCE_NAME:-voicemod}"

if ! command -v pactl >/dev/null; then
    echo "pactl not found — install pipewire-pulse (or pulseaudio-utils)." >&2
    exit 1
fi

[ -p "$FIFO" ] || { rm -f "$FIFO"; mkfifo "$FIFO"; echo "[+] created FIFO $FIFO"; }

# Avoid stacking duplicate modules across re-runs.
if pactl list modules short | grep -q "source_name=$SOURCE_NAME"; then
    echo "[=] source '$SOURCE_NAME' already loaded"
else
    MODULE_ID=$(pactl load-module module-pipe-source \
        source_name="$SOURCE_NAME" \
        file="$FIFO" \
        format=s16le rate="$RATE" channels="$CHANNELS" \
        source_properties=device.description="Voicemod")
    echo "[+] loaded module-pipe-source id=$MODULE_ID  source='$SOURCE_NAME'  ($RATE Hz, ${CHANNELS}ch, s16le)"
fi

echo
echo "Virtual mic ready. In Discord/OBS/etc. pick input 'Voicemod'."
echo "Next: ./receiver -listen :5000 -fifo $FIFO"
