# Vendored copy of github.com/ledongthuc/pdf

Upstream: https://github.com/ledongthuc/pdf
Vendored from module version `v0.0.0-20260907135840-6c8c28e0e8a0`
(upstream commit `6c8c28e0e8a0`, per the Go pseudo-version's own encoding of
the commit it was cut from - there is no tagged release at this commit).

This is the whole upstream module (`LICENSE` included, unmodified; the
original upstream `README.md` is kept as `UPSTREAM_README.md`), plus its own
tests and testdata, with two small local patches applied directly to the
source below. It is pulled in by `backend/go.mod`'s
`replace github.com/ledongthuc/pdf => ./third_party/ledongthuc-pdf` rather
than left on the upstream version, because both patched bugs are outright
panics on well-formed PDFs that real Helmcentral operators upload (builder
drawings and manuals), and there is no newer upstream release that fixes
them.

## Patch 1: `read.go`, `applyFilter` - Predictor 1 ("no prediction")

ISO 32000-2:2020 §7.4.4.4 (Table 8) defines `/Predictor 1` as "no
prediction": the FlateDecode output should be used as-is, with no per-row
filtering. Upstream's `applyFilter` only special-cased an *absent*
`/Predictor` key (`pred.Kind() == Null`) as "no prediction" and treated every
other value, including an explicit `1`, as unknown - `panic("pred")`. A
number of real-world PDF writers do emit `/DecodeParms << /Predictor 1 >>`
explicitly, and every one of them panicked the whole document, not just the
stream in question (`applyFilter` runs during `Page.GetPlainText`'s lazy
decompression, with no recover of its own upstream).

Fixed by treating `Predictor 1` the same as an absent `/Predictor`: return the
zlib reader unchanged.

While in there, `/Predictor 2` (TIFF) and PNG predictors other than "Up"
(`10`, `11`, `13`, `14`, `15`) were split out of the same `default: panic
("pred")` case into their own case with a clearer, explicit
"unsupported predictor" panic message. This is not new support - `pngUpReader`
below only ever implemented the PNG "Up" filter type (it assumes every row's
leading filter-type byte is `2` and errors otherwise) - it only makes the
"we don't decode this" case say so plainly instead of reading as "malformed
PDF" (`panic("pred")`, indistinguishable from an actually-corrupt predictor
value). Both still surface to the caller as an ordinary panic, recovered by
Helmcentral's own `extractPDFPage` (`backend/documents_extract.go`) into a
per-page error, same as before.

## Patch 2: `lex.go`, `readLiteralString` - unrecognised backslash escapes

ISO 32000-2:2020 §7.3.4.2: "If the character following the REVERSE SOLIDUS is
not one of those shown [in the escape-sequence table] ... the reverse
solidus shall be ignored." Upstream's `default` case for an unrecognised
escape called `b.errorf(...)`, which - unlike its name suggests - panics
(`lex.go`'s `errorf` is `panic(fmt.Errorf(...))`, not a returned error), and
then appended the backslash and the character anyway. A real builder PDF in
the wild contains a content-stream literal string with an escape sequence
that is not one of the ones the spec defines (for example an escaped letter
that needed no escaping at all), which panicked the whole document on a
single stray backslash.

Fixed to match the spec: keep just the character, drop the backslash
silently, no error.

While fixing that, the same function's octal-escape handling
(`\ddd`, one to three octal digits) was checked against the same section of
the spec: "high-order overflow shall be ignored." Upstream additionally
called `b.errorf` (panicking, as above) whenever three octal digits added up
to more than 255 (`\777` = 511, for instance) - again a spec-legal case
upstream had turned into a crash. The overflow itself was already handled
correctly (`byte(x)` truncates to the low 8 bits); only the panic on the way
there was removed. Backslash followed by an end-of-line marker (line
continuation - CR, LF or CRLF) was checked too and was already correct; no
change was needed there.

## What was not changed

- PNG predictors other than "Up" (`10`, `11`, `13`, `14`, `15`) and TIFF
  predictor 2 remain explicit, unimplemented errors - not silently
  misdecoded, and not claimed as supported.
- Nothing else in this module was modified.
