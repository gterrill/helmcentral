# ADR 0247: The passage video is rendered in the browser

## Status

Accepted (2026-11-02). Builds on the replay controller that steps the 3D replay
([ADR 0200](0200-passage-replay-in-3d.md)), the camera shots
([ADR 0206](0206-passage-replay-camera-shots.md)), the signs
([ADR 0204](0204-passage-replay-signs.md)) and the photo moments
([ADR 0248](0248-photo-moments-hold-the-replay.md)). The still images it shares
a closing card with are [ADR 0174](0174-passage-export-is-a-self-contained-card.md).

## Context

The operator wants the passage replay as a video: an MP4 to post, with the
fly-in, the camera shots, the speed-coloured track, the signs and their photos,
and the time of day. The replay already runs in the browser, on a MapLibre map
with a three.js boat, driven by one controller that answers "what does the
replay look like at this moment" as numbers.

Three facts decide where the video is made.

- The box runs on a Raspberry Pi 5, from an image that has neither ffmpeg nor a
  browser. Adding either to the image changes what the box is.
- The replay does not look the same twice when it is recorded. It is a WebGL map
  loading tiles over a flaky link, and a frame that is not ready when the screen
  is sampled has blank tiles in it.
- The operator owns the picture and the file. Nothing about a video needs to live
  on the box once it has been made.

## Decision

**The video is made in the operator's browser, on the replay's own code.** Nothing
changes in the backend or the Docker image. **Export**, then **Video**, asks one
question, landscape (1920 × 1080) or portrait (1080 × 1920), and makes the video
of the replay as it is set: its length, shots, base map, imagery, 3D, signs and
photo moments. The render loop is `renderPassageVideo(input, { onProgress,
signal })`, framework-free, in `lib/passage-video`. It returns an MP4 `Blob` or
throws an error that says what stopped it.

**Frames are stepped, not recorded.** The loop does not watch the replay play. It
asks the shared controller for one moment and builds that frame whole, 30 times a
second of video:

1. The controller gives the frame for a point in time: the camera, the boat, the
   speed colours and wake, the light, the cards and the photo moment.
2. A plain MapLibre map, made in a hidden container at the video's size, takes
   the camera, the projection (the globe for the fly-in), the light and tint, the
   track overlays and the boat. It is not the React `PassageMap`; both are built
   from the same lib modules (`replay-map-style`, `replay-framing`,
   `place-label-layers`, `boat-layer`).
3. The loop waits until the map reports idle: every tile and the terrain loaded,
   every fade done. Then it draws the map's canvas into a 2D canvas, which
   `preserveDrawingBuffer` keeps readable.
4. It lays the signs over it: cards and pins placed by `placeSignCard` from the
   card constants `SignCard` is built from, and the photo of a moment drawn by
   `momentRect`. The photos are loaded as bitmaps before the first frame.
5. The canvas goes to the encoder with its own timestamp, `index / 30` in whole
   microseconds, so no clock is involved.

A slow machine makes a slow render and the same video. The fly-in, the sailing
with its holds, and a closing card held for three seconds make up the schedule,
and the schedule is a function of the replay's timing alone.

**Portrait refits, it does not redesign.** The same shots and camera parameters
apply. An overview shot fits the whole track to the frame it is in, so the
portrait frame frames the track tall. Follow and orbit shots stay as the operator
set them.

**The closing card is the still image's.** `composeImageLayout` and
`paintOverlay`, the PNG export's own, lay out the band with the title, dates,
figures and map credit on the last frame of the map, scaled 1.6 times. One quiet
line over the foot of the map says it was made with Helmcentral. There is no
opening card and no mark on the replay itself.

**Encoding is WebCodecs, and the file is mediabunny's.** A `VideoEncoder` makes
H.264 (`avc1.640028`, High profile level 4.0, which fits 1080p at 30 frames a
second) at 10 Mbps, with a key frame every two seconds. Its chunks go to
mediabunny's `EncodedVideoPacketSource`, which writes an MP4 with the index at
the front into memory. There is no audio. The loop watches `encodeQueueSize` and
waits for the encoder when eight frames are queued, since a frame is about 8 MB.

**It stops rather than make a lesser video.** `VideoEncoder.isConfigSupported` is
asked at the chosen size before anything else, and a "no" stops the render with
"This browser can't make H.264 video." During the render:

- Any map error stops it, except a missing sea-mark tile, which only means there
  are no marks there. The message names what failed: the satellite imagery, the
  3D terrain, the sea marks or the chart, and that the boat may be offline.
- A frame that does not reach idle in 20 seconds stops it, and the message says
  which frame, and that the tab must be in front.
- An encoder error stops it and says so.

There is no WebM fallback, no retry, and no frame with a blank tile in it. A
photo that will not load is not a fault of this kind: the replay shows "Photo not
available" on its card and gives it no moment, and so does the video.

**Cancel frees everything.** An `AbortSignal` ends the loop between frames, and
cuts short a frame that is waiting on the map. The map, the encoder, the output
and the photo bitmaps are all let go, whether the render finished, failed or was
cancelled, and leaving the page cancels it. The modal shows a progress bar of
frames made out of frames in all, and Cancel.

**Nothing is stored on the box.** The finished file is a download. Where the
device can share a file (`navigator.canShare({ files })`, as an iPad can), a
**Share** button opens the share sheet too. The file is named from the passage
title and the day it started, as the other exports are.

## Consequences

- The tab has to stay in front. A browser stops a hidden tab's animation frames,
  and MapLibre draws on them, so a hidden tab times out and the render stops with
  a message that says why.
- The video is as long as the replay set on the page, plus the fly-in, the holds
  and three seconds. A 60 second replay with a few photos is about 75 seconds,
  more than 2,200 frames, and takes minutes to render.
- A 1080p frame is drawn at pixel ratio 1 so the page's cards are scaled up by
  one factor (1.5) to read at that size.
- The path through MapLibre, WebCodecs and the hidden container is checked in a
  real browser. The tests stand in for the encoder, the map and the canvas, and
  cover the schedule, the timestamps, the portrait refit, the support check, a
  source failure, cancel and the closing card.
- The HTML export player has its own timing and does not use any of this.

## Rejected

- **A server-side render with headless Chromium and ffmpeg in a sidecar.** The
  image has no browser and no ffmpeg, and the box has to go on running on a Pi 5.
  A sidecar for an occasional job would mean the box carrying a browser it never
  uses otherwise, and rendering a 3D map on its CPU.
- **ffmpeg.wasm.** It is tens of megabytes to download, runs slowly, and would
  encode in software what the browser's encoder does in hardware.
- **A hand-written MP4 muxer.** The container is simple until it is not: the
  index, the codec description, timestamps. mediabunny does that, and is kept
  up to date.
- **Real-time capture, with `MediaRecorder` on the screen.** It records what the
  display happens to show, in the order and at the speed it shows it. A dropped
  frame is gone, a hidden tab throttles, a tile that arrives late is blank in the
  file, and the result changes from run to run. Stepping the frames makes each one
  whole and the render repeatable.
- **Storing videos on the box.** The box has no use for them after they are
  posted, they are large, and keeping them means a place to list, delete and
  limit them. The browser has the file when it is made.
