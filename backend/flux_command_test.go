package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFluxReadOnlyRefusesWrites(t *testing.T) {
	refused := []string{
		`from(bucket:"a") |> range(start:-1h) |> to(bucket:"b")`,
		`from(bucket:"a") |> range(start:-1h) |> to (bucket:"b")`,
		`import "influxdata/influxdb/schema"
from(bucket:"a") |> range(start:-1h)
  |> experimental.to(bucket:"b")`,
		`import "influxdata/influxdb"
influxdb.delete(bucket:"a", start:-1h, stop:now())`,
		`from(bucket:"a") |> range(start:-1h) |> DELETE()`,
		`import "influxdata/influxdb"
from(bucket:"a") |> range(start:-1h) |> influxdb.wideTo(bucket:"b")`,
		`w = to
from(bucket:"a") |> range(start:-1h) |> w(bucket:"b")`,
		`import "sql"
from(bucket:"a") |> range(start:-1h) |> sql.to(driverName:"sqlite3", dataSourceName:"x", table:"t")`,
		`import "http"
from(bucket:"a") |> range(start:-1h) |> map(fn:(r)=>({r with s: http.post(url:"http://x")}))`,
		`import "http"
p = http.post
x = 1`,
		`import "influxdata/influxdb"
influxdb.api(method:"patch", path:"/api/v2/buckets/x", body: bytes(v:"{}"))`,
		`import "experimental/http/requests"
requests.do(method:"POST", url:"http://x")`,
		`from(bucket:"a") |> range(start:-1h) // convert to knots
  |> to(bucket:"b")`,
	}
	for _, script := range refused {
		if err := checkFluxReadOnly(script); err == nil {
			t.Errorf("script was accepted, want refused: %q", script)
		}
	}
}

func TestCheckFluxReadOnlyAllowsReads(t *testing.T) {
	allowed := []string{
		`from(bucket:"SignalK_Data") |> range(start:-5m) |> limit(n:3)`,
		`from(bucket:"a") |> range(start:-1h, stop:now()) |> aggregateWindow(every:1m, fn:mean)`,
		`from(bucket:"a") |> range(start:-1h) |> toFloat() |> map(fn:(r)=>({r with v: float(v:r._value)}))`,
		`from(bucket:"a") |> range(start:-1h) |> filter(fn:(r)=>r.topic=="x" and r.host!="stop")`,
		`from(bucket:"a") |> range(start:-1h) |> filter(fn:(r)=>r.state=="deleted")`,
		`// convert to knots
from(bucket:"a") |> range(start:-1h) // then delete nothing
  |> limit(n:3)`,
		`import "http"
from(bucket:"a") |> range(start:-1h) |> map(fn:(r)=>({r with u:"http://host/x"}))`,
	}
	for _, script := range allowed {
		if err := checkFluxReadOnly(script); err != nil {
			t.Errorf("script was refused: %v: %q", err, script)
		}
	}
}

func TestRequireFluxFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	err := requireFluxFile("settings file", path)
	if err == nil || !strings.Contains(err.Error(), "settings file not found: "+path) {
		t.Fatalf("missing file: got %v", err)
	}
	if err := os.WriteFile(path, []byte("x: 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireFluxFile("settings file", path); err != nil {
		t.Fatalf("existing file: %v", err)
	}
}

func TestReadFluxScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.flux")
	if err := os.WriteFile(path, []byte("from(bucket:\"a\")"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readFluxScript([]string{path}, strings.NewReader("ignored"))
	if err != nil || got != "from(bucket:\"a\")" {
		t.Fatalf("file: got %q, %v", got, err)
	}

	got, err = readFluxScript([]string{"-"}, strings.NewReader("from(bucket:\"b\")"))
	if err != nil || got != "from(bucket:\"b\")" {
		t.Fatalf("stdin: got %q, %v", got, err)
	}

	for _, args := range [][]string{nil, {"a", "b"}} {
		if _, err := readFluxScript(args, strings.NewReader("")); !errors.Is(err, errFluxUsage) {
			t.Errorf("args %v: want a usage error, got %v", args, err)
		}
	}

	if _, err := readFluxScript([]string{"-"}, strings.NewReader("  \n")); !errors.Is(err, errFluxUsage) {
		t.Errorf("empty script: want a usage error, got %v", err)
	}

	// An unreadable file is a runtime failure, not a usage mistake.
	_, err = readFluxScript([]string{filepath.Join(t.TempDir(), "missing.flux")}, strings.NewReader(""))
	if err == nil || errors.Is(err, errFluxUsage) {
		t.Errorf("missing file: want a non-usage error, got %v", err)
	}
}

func TestFluxURLOverride(t *testing.T) {
	if got, err := fluxURLOverride(" http://h:8086 "); err != nil || got != "http://h:8086" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := fluxURLOverride(""); err != nil || got != "" {
		t.Errorf("empty: got %q, %v", got, err)
	}
	if _, err := fluxURLOverride("none"); err == nil || !strings.Contains(err.Error(), "preview-backend.sh") {
		t.Errorf("none: got %v", err)
	}
}
