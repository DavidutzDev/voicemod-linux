# voicemod-bridge (PoC)

> ⚠️ **EXPERIMENTAL / PROOF OF CONCEPT.** Confirmed only on the author's own
> machine. Not guaranteed to work on other environments. This is the "tier one"
> baseline from the design discussion — a working virtual mic, *not* a hardened
> product.

A low-latency bridge that takes Voicemod's processed audio out of a Windows
[Winboat](https://www.winboat.app/) VM and exposes it on Linux as a **real,
selectable microphone** — the last-mile piece the original `ffplay` shell
script was missing.

## Why this beats the shell script

| | original `.sh` | this PoC |
|---|---|---|
| Transport codec | AAC encode + decode (latency tax) | **raw PCM** (the VM↔host link is a local bridge, no codec needed) |
| Capture API | DirectShow (`dshow`) | **WASAPI** via miniaudio (smaller periods) |
| Linux endpoint | `ffplay` → speakers only | **`module-pipe-source`** → a virtual mic apps can select |
| Loss handling | none | sequence numbers + silence gap-fill |

## Signal path

```
                         WINDOWS (Winboat guest)                 |        LINUX host
 mic → Voicemod → VB-Cable In → "CABLE Output" → transmitter ──UDP──→ receiver → FIFO → module-pipe-source → Discord/OBS
                                  (WASAPI capture)        raw PCM, seq#        s16le        "Voicemod" mic
```

## Latency expectation

- **Floor you can't remove** (~30–60 ms): Voicemod processing + VM audio stack + WASAPI capture.
- **Added by this bridge** (~15–30 ms): small period + ~0 ms local UDP hop + minimal buffering + PipeWire quantum.
- **Realistic mouth→app: ~50–90 ms.** Fine for calls (perceptible lag starts ~150 ms one-way). Not for live self-monitoring / rhythm games.

## Layout

```
shared/packet.go      wire format: 12-byte header (magic+ver+ch+seq+frames) + s16le PCM
transmitter/main.go   Windows: WASAPI capture (malgo) → UDP sender
receiver/main.go      Linux: UDP → gap-fill → FIFO writer + rx stats
scripts/setup-linux.sh / teardown-linux.sh   load/unload the virtual mic
```

## Run it

### 1. Linux host — load the virtual mic, then start the receiver

```bash
./scripts/setup-linux.sh                 # loads module-pipe-source "voicemod"
go build -o bin/receiver ./receiver      # NOT -o receiver: that name collides with the receiver/ dir
./bin/receiver -listen :5000 -fifo /tmp/voicemod.fifo
```

The receiver blocks on the FIFO until the module is loaded — that's expected.

### 2. Windows guest (Winboat) — build & run the transmitter

Needs `CGO_ENABLED=1` and a C toolchain (MinGW-w64) because malgo wraps
miniaudio. In the guest:

PowerShell:
```powershell
gcc --version                  # must print — malgo needs a C compiler (MinGW-w64)
go env -w CGO_ENABLED=1        # persist; PowerShell's `set CGO_ENABLED=1` does NOT work
go build -o transmitter.exe ./transmitter
.\transmitter.exe -list                                 # find the VB-Cable device name
.\transmitter.exe -addr <LINUX_HOST_IP>:5000 -device "CABLE Output"
```

> "undefined: malgo.InitContext" = cgo is OFF. Install MinGW-w64 (via MSYS2:
> `pacman -S mingw-w64-x86_64-gcc`, add `C:\msys64\mingw64\bin` to PATH) and set
> `CGO_ENABLED=1`, then rebuild.

### 3. In Voicemod / VB-Cable

Set Voicemod's output to **VB-Cable Input** so its processed audio lands on
`CABLE Output`, which the transmitter captures. In Discord/OBS, pick the
**Voicemod** input.

### Teardown

```bash
./scripts/teardown-linux.sh
```

## Tuning

- `transmitter -frame 5` — capture period in ms. Lower = less latency, more packets.
- `receiver -maxfill 50` — cap on silence frames inserted per gap after a stall.

## Known gaps (tier-two hardening, not done here)

1. **Clock drift.** Independent crystals on each side drift apart over minutes →
   eventual under/overrun (clicks/dropouts). Fix: adaptive resampling driven by
   FIFO/buffer fill level. *This is the #1 thing that makes naive bridges pass a
   30-second test and fail a real call.*
2. **Mic INTO the VM** is assumed handled by Winboat's own device passthrough —
   this PoC only covers Voicemod's output back out to Linux.
3. **Native PipeWire node** (libpipewire, own the quantum) instead of the FIFO,
   to shave the last few ms.
4. **Real jitter buffer** with reordering (current one is in-order, drop-late —
   fine on a local bridge).
