# vmbridge

> ⚠️ **WIP — Work In Progress.** Functional but unfinished. **not** guaranteed to work on other environments than mines.
> Known unfinished item: clock-drift compensation (long sessions may develop
> clicks/dropouts). Use at your own risk.

Run **Voicemod** (Windows-only) on Linux by bridging its processed audio out of a
[Qemu](https://www.qemu.org/) VM and exposing it as a **real, selectable
Linux microphone**. The virtual mic is registered on start and torn down on exit
automatically — no manual `pactl` scripts.

Successor to the original proof-of-concept, archived in [`poc/`](./poc).

## How it works

```
 mic → Voicemod → VB-Cable In → "CABLE Output" → vmbridge-tx ──UDP──→ vmbridge-rx → virtual mic → Discord/OBS
        (Windows / QEMU guest)      WASAPI capture     raw PCM        (Linux host)   "Voicemod"
```

The QEMU guest and Linux host share a local virtual bridge (effectively
localhost), so audio is sent as **raw PCM** — no codec, no encode/decode latency.

## Project layout

```
cmd/
  vmbridge-rx/      Linux receiver + virtual-mic lifecycle
  vmbridge-tx/      Windows transmitter
internal/
  wire/             UDP packet format (12B header + s16le PCM)
  source/           virtual mic: load/unload module-pipe-source in-process
  receiver/         UDP ingest: gap-fill with silence + write to mic + stats
  capture/          WASAPI capture via malgo (Windows-only; stubbed elsewhere)
poc/                archived proof-of-concept (separate go.mod)
```

---

## Prerequisites

**Linux host**
- Go 1.26+
- PipeWire or PulseAudio with `pactl` (`pipewire-pulse` or `pulseaudio-utils`)
- A working [QEMU](https://www.winboat.app/) install with Windows running

**Windows guest (inside QEMU)**
- [Voicemod](https://www.voicemod.net/) installed
- [VB-Cable](https://vb-audio.com/Cable/) installed
- [Go for Windows](https://go.dev/dl/)
- A C toolchain — malgo uses cgo. Easiest:
  [w64devkit](https://github.com/skeeto/w64devkit/releases) (unzip, add
  `…\w64devkit\bin` to `PATH`). Verify with `gcc --version`.

---

## Build & run

### 1. Linux host — receiver

```bash
go build -o bin/vmbridge-rx ./cmd/vmbridge-rx
./bin/vmbridge-rx
```

On start it registers a mic named **Voicemod** and listens on UDP `:5000`.
On Ctrl-C (or SIGTERM) it unregisters the mic and cleans up. **Leave it running**
the whole time you use Voicemod.

Open the UDP port if a firewall is active:
```bash
sudo firewall-cmd --add-port=5000/udp     # firewalld
# or
sudo ufw allow 5000/udp                    # ufw
```

Find the host IP the guest sends to:
```bash
ip -4 addr | grep inet | grep -v 127.0.0.1
```

Flags: `-listen :5000  -name voicemod  -desc Voicemod  -rate 48000  -channels 2  -maxfill 50  -fifo <path>`

### 2. Windows guest — transmitter

In PowerShell, inside the copied project folder:

```powershell
go env -w CGO_ENABLED=1
go mod tidy
go build -o vmbridge-tx.exe ./cmd/vmbridge-tx

.\vmbridge-tx.exe -list                                     # find the VB-Cable capture device
.\vmbridge-tx.exe -addr <LINUX_IP>:5000 -device "CABLE Output"
```

> `undefined: malgo.*` at build = cgo is off. Confirm `gcc --version` works and
> `go env CGO_ENABLED` prints `1`.

Flags: `-addr host:port  -device "CABLE Output"  -rate 48000  -channels 2  -frame 5  -queue 8  -list`

### 3. Voicemod routing (Windows)

1. Voicemod → Settings → **Output = CABLE Input (VB-Audio Virtual Cable)**.
2. Enable monitoring ("Hear myself").
3. Voice Changer **ON**, pick a voice.

### 4. Use it (Linux)

In Discord/OBS, select input **Voicemod**.

- Apps enumerate devices at launch — start `vmbridge-rx` **first**, then
  (re)start the app. Discord: fully **Quit** from tray, not just close the window.

Live-monitor your own changed voice (headset only — speakers cause feedback):
```bash
pactl load-module module-loopback source=voicemod sink=@DEFAULT_SINK@ latency_msec=20
```

---

## Verify

`vmbridge-rx` logs `rx pkts=` every 5s — a rising count means audio is flowing.
Or capture directly from the virtual mic:
```bash
pw-record --target voicemod /tmp/t.wav   # talk, Ctrl-C
pw-play /tmp/t.wav
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `rx pkts=0` | Wrong `-addr` IP or UDP 5000 blocked. Check firewall. |
| `package cmd/... is not in std` | Use `./cmd/...` (with leading `./`). |
| `undefined: malgo.*` (Windows) | cgo off — install gcc, `go env -w CGO_ENABLED=1`. |
| Mic missing in Discord/OBS | Start rx first, then fully restart the app. |
| `no device matching` (Windows) | Run `-list`, copy the exact substring into `-device`. |
| Choppy after several minutes | Clock drift — known WIP gap, not yet fixed. |
| Robotic/garbled | Sample-rate mismatch — keep 48000 everywhere. |

## Known limitations (WIP)

- **No clock-drift compensation** — independent capture/playback clocks diverge
  over time → eventual clicks/dropouts on long sessions. Planned: adaptive
  resampling driven by FIFO fill level.
- Native libpipewire source node (lower latency than the FIFO) not yet done.
- Mic *into* the VM is assumed handled by QEMU's own passthrough.
