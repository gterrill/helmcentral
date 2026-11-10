package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// fluxCommandTimeout bounds one query. The client's own HTTP timeout is set to
// the same figure: its default is 20 s, which would cut a slow query short
// whatever the context allows.
const fluxCommandTimeout = 120 * time.Second

// fluxPingTimeout bounds the reachability check before the query.
const fluxPingTimeout = 5 * time.Second

// fluxWritePattern matches the Flux names that write: any whole word that is
// or ends in "to" (to, influxdb.to, experimental.to, sql.to, wideTo, and a bare
// reference such as `w = to`, which a call pattern would miss), delete, post,
// requests (requests.do) and api( (influxdb.api reaches any endpoint). It errs
// towards refusing, so a query with such a word inside a string (a tag value
// "photo", say) has to be reworded. toFloat, stop: and "deleted" pass, because
// the word must end at the boundary.
//
// This is a best-effort guard against ACCIDENTAL writes, not a guarantee: a
// word list cannot make Flux safe, and the query runs with the server's
// write-capable token.
var fluxWritePattern = regexp.MustCompile(`(?i)\b\w*to\b|\bdelete\b|\bpost\b|\bapi\s*\(|\brequests\b`)

// fluxLineComment matches a // comment at the start of a line or after
// whitespace, so "http://host" inside a string is not taken for one.
var fluxLineComment = regexp.MustCompile(`(?m)(^|\s)//.*$`)

// fluxURLOverride returns the trimmed INFLUX_URL value. "none" is refused: it
// means something only to scripts/preview-backend.sh.
func fluxURLOverride(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") {
		return "", errors.New(`INFLUX_URL=none only means something to scripts/preview-backend.sh; give an address such as http://<host>:8086`)
	}
	return v, nil
}

// requireFluxFile fails with a named error when path does not exist. The
// settings and secrets readers treat a missing file as "not configured" or
// create it, neither of which a diagnostic run should do.
func requireFluxFile(what, path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s not found: %s", what, path)
		}
		return fmt.Errorf("%s %s: %w", what, path, err)
	}
	return nil
}

// checkFluxReadOnly refuses a script that could write to or delete from the
// database, best effort (see fluxWritePattern). Comments are ignored.
func checkFluxReadOnly(script string) error {
	if m := fluxWritePattern.FindString(fluxLineComment.ReplaceAllString(script, "$1")); m != "" {
		return fmt.Errorf("script contains %q; this command only reads", strings.TrimSpace(m))
	}
	return nil
}

// readFluxScript returns the script named by args: exactly one argument, a file
// path or "-" for stdin.
func readFluxScript(args []string, stdin io.Reader) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%w: usage: flux <file|->", errFluxUsage)
	}
	var data []byte
	var err error
	if args[0] == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(args[0])
	}
	if err != nil {
		return "", fmt.Errorf("reading script: %w", err)
	}
	script := strings.TrimSpace(string(data))
	if script == "" {
		return "", fmt.Errorf("%w: script is empty", errFluxUsage)
	}
	return script, nil
}

// runFluxCommand is the `flux` subcommand: it runs one read-only Flux script
// against the InfluxDB the settings name, with the token from the secrets
// store, and prints the result as CSV. INFLUX_URL replaces the settings URL.
// It exists so a query can be run against the real database before it is
// handed to anyone or shipped.
func runFluxCommand(args []string) int {
	if err := fluxCommand(args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "flux: %v\n", err)
		if errors.Is(err, errFluxUsage) {
			return 2
		}
		return 1
	}
	return 0
}

var errFluxUsage = errors.New("bad arguments")

func fluxCommand(args []string, stdin io.Reader, out io.Writer) error {
	script, err := readFluxScript(args, stdin)
	if err != nil {
		return err
	}
	if err := checkFluxReadOnly(script); err != nil {
		return err
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	if err := requireFluxFile("settings file", settingsPath); err != nil {
		return err
	}
	if _, err := readSettings(settingsPath); err != nil {
		return fmt.Errorf("settings %s: %w", settingsPath, err)
	}

	// Opening the store creates a missing database or key, so name them first.
	// scripts/flux.sh points these at a copy, never the running server's files.
	if err := requireFluxFile("secrets database", secretsDBPath()); err != nil {
		return err
	}
	if err := requireFluxFile("secrets key", secretsKeyPath()); err != nil {
		return err
	}
	ss, err := newSecretsStore(secretsDBPath(), secretsKeyPath())
	if err != nil {
		return fmt.Errorf("secrets store: %w", err)
	}
	globalSecretsStore = ss

	cfg, ok, err := loadInfluxConfig(settingsPath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("InfluxDB is not configured in %s: need influxdb.enabled, url, org, bucket and the INFLUXDB_TOKEN secret", settingsPath)
	}
	override, err := fluxURLOverride(os.Getenv("INFLUX_URL"))
	if err != nil {
		return err
	}
	if override != "" {
		cfg.URL = override
	}

	opts := influxdb2.DefaultOptions().SetHTTPRequestTimeout(uint(fluxCommandTimeout / time.Second))
	client := influxdb2.NewClientWithOptions(cfg.URL, cfg.Token, opts)
	defer client.Close()

	// The ping gets its own short budget so a wrong address fails in seconds;
	// only the query gets the full one.
	pingCtx, cancelPing := context.WithTimeout(context.Background(), fluxPingTimeout)
	defer cancelPing()
	if _, err := client.Ping(pingCtx); err != nil {
		return fmt.Errorf("InfluxDB at %s is unreachable (INFLUX_URL overrides the settings address): %w", cfg.URL, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), fluxCommandTimeout)
	defer cancel()

	dialect := api.DefaultDialect()
	dialect.Annotations = nil
	csv, err := client.QueryAPI(cfg.Org).QueryRaw(ctx, script, dialect)
	if err != nil {
		return fmt.Errorf("query against %s: %w", cfg.URL, err)
	}
	_, err = io.WriteString(out, csv)
	return err
}
