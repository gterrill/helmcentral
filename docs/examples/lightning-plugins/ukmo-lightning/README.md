# ukmo-lightning

Helmcentral's reference **lightning provider**: the chance of at least one
lightning flash in the grid cell holding the boat, for each 3-hour window out
to 14.5 days. Free and keyless.

Lightning probability: © Crown copyright, Met Office, CC BY-SA 4.0.

## Where the data comes from

The Met Office publishes its Blended Probabilistic Forecast (IMPROVER,
blending the Met Office and ECMWF ensembles) in a public S3 bucket. The plugin
reads the `lightning_flash_accumulation` family, 3-hour windows, from:

```
https://met-office-global-gridded-probabilities.s3.eu-west-2.amazonaws.com
```

That is the only host the plugin may reach (`ukmo-lightning.allowed_hosts.json`).
Requests are anonymous. The boat's position never leaves the boat: the plugin
downloads the whole global grid for a window and picks the cell from it.

A run arrives every six hours. Its directory appears before its files do, so
the plugin takes the newest run whose last window (run time plus 348 hours, 116
files) exists. It finds that with two kinds of small bucket listing, never by
listing a whole run, which holds over a thousand keys. A window missing from a
complete run is left out of the answer, not filled in from its neighbours.

## Contract

`fetch_lightning`, input:

```json
{"lat": -27.4, "lon": 153.0, "from": "2026-11-12T12:00:00Z", "to": "2026-11-13T00:00:00Z"}
```

`run` (RFC 3339, optional) names the forecast run to read. When it is there the
plugin reads that run without listing the bucket; when it is not, the plugin
finds the newest complete run itself. The host leaves it out on the first call
of a refresh and passes the run that call returned to the rest. A `run` that is
not a six-hourly run time is an error.

Output, for each window whose **end** falls in `(from, to]`:

```json
{"run": "2026-11-12T00:00:00Z",
 "windows": [{"start": "2026-11-12T09:00:00Z", "end": "2026-11-12T12:00:00Z", "probability": 0.03}]}
```

`probability` is 0 to 1, taken as stored: no bucketing, smoothing or rounding.
At most 8 windows (24 hours) fit in one call, because every window costs a
download of 330 to 440 KB and a decode of a 4.9 MB grid, and a plugin call has
a 60 second background budget. The host asks for 24 hours at a time. See
`backend/lightning_schedule.go` for when it asks.

Any failure other than a missing window file (a listing that does not parse, a
server error, a file that is not the layout this plugin knows, a file whose
`time_bnds` is not the window asked for) is returned as an error.

## The HDF5 reader

The grid files are NetCDF-4, which is HDF5. General NetCDF libraries for Go
fail under TinyGo (`reflect.SliceOf` is not implemented, so
`github.com/batchatco/go-native-netcdf` panics at run time), so `hdf5.go` is a
small reader with no reflection, written for exactly this layout:

- superblock version 2, version 2 object headers;
- the root group's links and the grid's attributes, in the header or in dense
  storage (a fractal heap with a version 2 B-tree name index);
- the grid as one float32 chunk, shuffle then deflate.

It finds the grid by name and by its `long_name` attribute, and checks that the
shape is 960 by 1280, the datatype is little-endian float32, and the latitude
and longitude variables match the grid the cell lookup assumes (latitude
-89.90625 + 0.1875 i, longitude -179.859375 + 0.28125 j). Anything else is an
error that says what it found. A change at the Met Office therefore shows up as
a failed fetch, never as probabilities from the wrong cell.

It does not verify HDF5's internal checksums. The deflate stream's own checksum
is verified, and the file's end-of-file address is checked against the bytes
downloaded.

## Building

Requires only Docker, pinned to `tinygo/tinygo:0.41.1`. From the repo root:

```sh
docker run --rm -e HOME=/tmp -e GOTOOLCHAIN=local -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/lightning-plugins/ukmo-lightning &&
  go mod download &&
  tinygo build -o ukmo-lightning.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory.

## Installing

Copy both files into `plugins/lightning/`:

```sh
cp ukmo-lightning.wasm ukmo-lightning.allowed_hosts.json ../../../../plugins/lightning/
```

Then set `ui.lightning_provider: ukmo-lightning` in `settings.yaml`, or leave
it unset to use the default. Lightning is optional: with no plugin installed
there is no lightning, and nothing else is affected.

## Testing

`main.go` holds only the `//go:wasmexport` wrappers and is gated
`//go:build tinygo`, so the reader and the run and window logic build and test
under the plain host toolchain:

```sh
go test ./...
```

The files in `testdata/` are real, captured from the bucket on 2026-11-12:

- `20261112T0300Z-B20261112T0000Z-lightning_flash_accumulation-PT03H.nc`, a
  window file. The tests check cell values against ones read with h5py.
- `list-day-20261112.xml`, the listing of the day's run directories.
- `list-last-window-T0000Z.xml` and `list-last-window-T0600Z.xml`, the check
  for a run's last window: one for a complete run (`KeyCount` 1) and one for a
  run still being written (`KeyCount` 0, captured at 11:44 UTC).
