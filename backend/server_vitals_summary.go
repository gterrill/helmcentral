package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// runVitalsSummaryCommand is `helmcentral vitals-summary <files...>`: it
// merges the minute files server vitals writes and prints one table, so a
// day's (or a week's) numbers read as one picture. It reads files only and
// needs no running server.
func runVitalsSummaryCommand(args []string) int {
	if err := writeVitalsSummary(os.Stdout, args); err != nil {
		fmt.Fprintf(os.Stderr, "vitals-summary: %v\n", err)
		return 1
	}
	return 0
}

// writeVitalsSummary merges every line of every file in paths. A line it
// cannot read stops the summary and names its file and line, since a table
// quietly missing a minute would mislead.
func writeVitalsSummary(w io.Writer, paths []string) error {
	if len(paths) == 0 {
		return errors.New("usage: helmcentral vitals-summary <server-vitals-YYYY-MM-DD.jsonl>...")
	}
	timings := map[string]*histogram{}
	counts := map[string]int64{}
	var (
		minutes                         int
		first, last                     time.Time
		heapSum, heapMax, goroutinesMax float64
		pauseMax, cpuSum, allocSum      float64
		gaugeMinutes                    int
		cpuMinutes, allocMinutes        int
	)
	for _, p := range paths {
		if err := readVitalsLines(p, func(n int, line vitalsLine) error {
			for k, hj := range line.Timings {
				h := timings[k]
				if h == nil {
					h = &histogram{}
					timings[k] = h
				}
				if err := h.mergeJSON(hj); err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
			}
			for k, c := range line.Counts {
				counts[k] += c
			}
			minutes++
			if first.IsZero() || line.At.Before(first) {
				first = line.At
			}
			if line.At.After(last) {
				last = line.At
			}
			if g, ok := line.Gauges["runtime.heap_live_bytes"]; ok {
				gaugeMinutes++
				heapSum += g
				heapMax = max(heapMax, g)
			}
			if g, ok := line.Gauges["runtime.cpu_cores"]; ok {
				cpuMinutes++
				cpuSum += g
			}
			if g, ok := line.Gauges["runtime.alloc_bytes_per_s"]; ok {
				allocMinutes++
				allocSum += g
			}
			goroutinesMax = max(goroutinesMax, line.Gauges["runtime.goroutines"])
			pauseMax = max(pauseMax, line.Gauges["runtime.gc_pause_p99_ms"])
			return nil
		}); err != nil {
			return err
		}
	}

	keys := make([]string, 0, len(timings))
	for k := range timings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := timings[keys[i]], timings[keys[j]]
		if a.sum != b.sum {
			return a.sum > b.sum
		}
		return keys[i] < keys[j]
	})

	if minutes == 0 {
		return fmt.Errorf("no vitals lines in %s", strings.Join(paths, ", "))
	}
	fmt.Fprintf(w, "%d minutes, %s to %s\n\n", minutes, first.Format(time.RFC3339), last.Format(time.RFC3339))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tCOUNT\tP50 ms\tP95 ms\tP99 ms\tMAX ms\tTOTAL s")
	for _, k := range keys {
		h := timings[k]
		fmt.Fprintf(tw, "%s\t%d\t%.1f\t%.1f\t%.1f\t%.1f\t%.2f\n", k, h.n,
			durationMs(h.quantile(0.50)), durationMs(h.quantile(0.95)), durationMs(h.quantile(0.99)),
			durationMs(time.Duration(h.max)), time.Duration(h.sum).Seconds())
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	var countKeys []string
	for k, n := range counts {
		if n != 0 {
			countKeys = append(countKeys, k)
		}
	}
	sort.Strings(countKeys)
	if len(countKeys) > 0 {
		fmt.Fprintln(w)
		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "COUNTER\tTOTAL")
		for _, k := range countKeys {
			fmt.Fprintf(tw, "%s\t%d\n", k, counts[k])
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	if gaugeMinutes > 0 {
		fmt.Fprintf(w, "\nheap live: average %.1f MB, highest %.1f MB\n", heapSum/float64(gaugeMinutes)/1e6, heapMax/1e6)
		fmt.Fprintf(w, "GC: %d cycles, worst minute's pause p99 %.2f ms\n", counts["runtime.gc_cycles"], pauseMax)
		cpu, alloc := "CPU not recorded", "allocation not recorded"
		if cpuMinutes > 0 {
			cpu = fmt.Sprintf("process CPU: average %.2f cores", cpuSum/float64(cpuMinutes))
		}
		if allocMinutes > 0 {
			alloc = fmt.Sprintf("allocation: average %.1f MB/s", allocSum/float64(allocMinutes)/1e6)
		}
		fmt.Fprintf(w, "goroutines: highest %.0f; %s; %s\n", goroutinesMax, cpu, alloc)
	}
	return nil
}

func readVitalsLines(path string, fn func(n int, line vitalsLine) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var line vitalsLine
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if line.Format != vitalsFormat {
			return fmt.Errorf("%s:%d: line format is %d, this build reads format %d (its buckets cannot be read against these edges)", path, n, line.Format, vitalsFormat)
		}
		if err := fn(n, line); err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
	}
	return sc.Err()
}
