package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func engineSamples(instance string, revolutionsHz float64, ts time.Time) map[string]telemetrySample {
	prefix := "propulsion." + instance
	return map[string]telemetrySample{
		prefix + ".revolutions":           {Present: true, Value: revolutionsHz},
		prefix + ".revolutions.timestamp": {Present: true, Value: ts.UTC().Format(time.RFC3339)},
	}
}

func mergeSamples(parts ...map[string]telemetrySample) fakeTelemetry {
	out := map[string]telemetrySample{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return fakeTelemetry{Samples: out}
}

func boolPtrValue(p *bool) string {
	if p == nil {
		return "nil"
	}
	if *p {
		return "true"
	}
	return "false"
}

func TestEnginesRunningIsNilWhenNoEnginesAreConfigured(t *testing.T) {
	reader := mergeSamples(engineSamples("port", 20, time.Now()))
	if got := readEnginesRunning(reader, nil); got != nil {
		t.Fatalf("got %s, want nil", boolPtrValue(got))
	}
}

func TestEnginesRunningIsNilWhenNoConfiguredEngineIsReporting(t *testing.T) {
	// Instances 0 and 1 report, but the operator configured 2 and 3.
	reader := mergeSamples(engineSamples("0", 20, time.Now()), engineSamples("1", 20, time.Now()))
	engines := []vesselEngineSetting{{Instance: "2"}, {Instance: "3"}}
	if got := readEnginesRunning(reader, engines); got != nil {
		t.Fatalf("got %s, want nil: only configured instances count", boolPtrValue(got))
	}
}

func TestEnginesRunningReadsTheConfiguredInstanceNotAGuessedName(t *testing.T) {
	reader := mergeSamples(engineSamples("7", 20, time.Now()))
	got := readEnginesRunning(reader, []vesselEngineSetting{{Instance: "7"}})
	if got == nil || !*got {
		t.Fatalf("got %s, want true", boolPtrValue(got))
	}
}

func TestEnginesRunningIsFalseWhenReportingEnginesAreStopped(t *testing.T) {
	reader := mergeSamples(engineSamples("port", 0, time.Now()), engineSamples("starboard", 0, time.Now()))
	got := readEnginesRunning(reader, []vesselEngineSetting{{Instance: "port"}, {Instance: "starboard"}})
	if got == nil || *got {
		t.Fatalf("got %s, want false", boolPtrValue(got))
	}
}

func TestEnginesRunningSkipsAStaleEngineAndStillReadsTheNext(t *testing.T) {
	stale := time.Now().Add(-10 * time.Minute)
	reader := mergeSamples(engineSamples("port", 20, stale), engineSamples("starboard", 20, time.Now()))
	got := readEnginesRunning(reader, []vesselEngineSetting{{Instance: "port"}, {Instance: "starboard"}})
	if got == nil || !*got {
		t.Fatalf("got %s, want true from the fresh second engine", boolPtrValue(got))
	}
}

func TestEnginesRunningIsNilWhenEveryConfiguredEngineIsStale(t *testing.T) {
	stale := time.Now().Add(-10 * time.Minute)
	reader := mergeSamples(engineSamples("port", 20, stale))
	if got := readEnginesRunning(reader, []vesselEngineSetting{{Instance: "port"}}); got != nil {
		t.Fatalf("got %s, want nil", boolPtrValue(got))
	}
}

func TestEnginesRunningFallsBackToTheRpmLeaf(t *testing.T) {
	reader := fakeTelemetry{Samples: map[string]telemetrySample{
		"propulsion.0.rpm":           {Present: true, Value: 1450.0},
		"propulsion.0.rpm.timestamp": {Present: true, Value: time.Now().UTC().Format(time.RFC3339)},
	}}
	got := readEnginesRunning(reader, []vesselEngineSetting{{Instance: "0"}})
	if got == nil || !*got {
		t.Fatalf("got %s, want true", boolPtrValue(got))
	}
}

const runningEngineTree = `{"propulsion":{"2":{"revolutions":{"value":20,"timestamp":"%s"}}}}`

// fetchSignalKVesselState feeds the process-wide GNSS validator; reset it so
// these ticks do not leak a "no position" reading into later tests.
func resetGNSSForTest(t *testing.T) {
	t.Helper()
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)
}

func TestVesselStateEnginesRunningFollowsVesselSettings(t *testing.T) {
	resetGNSSForTest(t)
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselFixture(t, settingsPath, vesselView{Engines: []vesselEngineSetting{{Instance: "2", Name: "Port"}}}); err != nil {
		t.Fatal(err)
	}
	seedSelfTree(t, strings.Replace(runningEngineTree, "%s", time.Now().UTC().Format(time.RFC3339), 1))

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatal(err)
	}
	if state.EnginesRunning == nil || !*state.EnginesRunning {
		t.Fatalf("got %s, want true", boolPtrValue(state.EnginesRunning))
	}
	payload := buildVesselStatePayload()
	if running, ok := payload["engines_running"].(*bool); !ok || running == nil || !*running {
		t.Fatalf("wire engines_running: got %v", payload["engines_running"])
	}
	if _, ok := payload["engine_0_rpm"]; ok {
		t.Fatalf("engine_0_rpm must be gone from the wire")
	}
}

func TestVesselStateWithNoEnginesConfiguredReportsNoEngineState(t *testing.T) {
	resetGNSSForTest(t)
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	seedSelfTree(t, strings.Replace(runningEngineTree, "%s", time.Now().UTC().Format(time.RFC3339), 1))

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatal(err)
	}
	if state.EnginesRunning != nil {
		t.Fatalf("got %s, want nil", boolPtrValue(state.EnginesRunning))
	}
}

func TestVesselStateLogsUnreadableSettingsAndReadsNoEngines(t *testing.T) {
	resetGNSSForTest(t)
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("roles: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetVesselReadLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, strings.Replace(runningEngineTree, "%s", time.Now().UTC().Format(time.RFC3339), 1))

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatal(err)
	}
	if state.EnginesRunning != nil {
		t.Fatalf("got %s, want nil", boolPtrValue(state.EnginesRunning))
	}
	if !strings.Contains(buf.String(), "engines unreadable") {
		t.Fatalf("expected a log line, got %q", buf.String())
	}
}
