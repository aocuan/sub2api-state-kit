package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/zhang2580384/sub2api-state-kit/plugin/internal/pluginapi/v1"
)

type diagnosticStatus struct {
	DiagnosticsEnabled bool              `json:"diagnostics_enabled"`
	Diagnostics        []diagnosticEvent `json:"diagnostics"`
}

func diagnosticHealth(t *testing.T, e *Engine) diagnosticStatus {
	t.Helper()
	response, err := e.Health(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var status diagnosticStatus
	if err := json.Unmarshal([]byte(response.StatusJson), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestDiagnosticLogIsOptInBoundedAndRedacted(t *testing.T) {
	e := newEngine(nil, "https://example.invalid", time.Second)
	defer e.Close()

	rawState := testState(292)
	e.recordDiagnostic(diagnosticEvent{Stage: "capture", Outcome: "accepted", StateClass: stateDiagnosticClass(rawState),
		TargetProxy: "socks5://proxy-user:proxy-secret@proxy.example:1080", CaptureEgress: "203.0.113.18"})
	if status := diagnosticHealth(t, e); status.DiagnosticsEnabled || len(status.Diagnostics) != 0 {
		t.Fatalf("diagnostics were active while disabled: %+v", status)
	}

	config, err := ParseConfig([]byte(`{"diagnostic_log_enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyConfig(context.Background(), &pluginv1.ApplyConfigRequest{ConfigJson: []byte(jsonText(config))}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxDiagnosticEvents+10; i++ {
		e.recordDiagnostic(diagnosticEvent{Stage: "capture", Outcome: "accepted", StateClass: stateDiagnosticClass(rawState),
			TargetProxy: "socks5://proxy-user:proxy-secret@proxy.example:1080", CaptureEgress: "203.0.113.18"})
	}
	response, err := e.Health(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	status := diagnosticHealth(t, e)
	if !status.DiagnosticsEnabled {
		t.Fatal("diagnostic switch was not reported")
	}
	if len(status.Diagnostics) != maxDiagnosticEvents {
		t.Fatalf("diagnostic log length = %d, want %d", len(status.Diagnostics), maxDiagnosticEvents)
	}
	if status.Diagnostics[0].Seq != 11 || status.Diagnostics[len(status.Diagnostics)-1].Seq != 250 {
		t.Fatalf("unexpected retained sequence range: %d..%d", status.Diagnostics[0].Seq, status.Diagnostics[len(status.Diagnostics)-1].Seq)
	}
	if response.StatusJson == "" || strings.Contains(response.StatusJson, "proxy-secret") || strings.Contains(response.StatusJson, rawState) {
		t.Fatalf("diagnostic status leaked sensitive data: %s", response.StatusJson)
	}
	if !strings.Contains(response.StatusJson, "socks5://proxy.example:1080") {
		t.Fatalf("redacted proxy endpoint missing: %s", response.StatusJson)
	}
}

func TestDiagnosticStateAndEgressClassification(t *testing.T) {
	if got := stateDiagnosticClass(testState(292)); got != "292" {
		t.Fatalf("292 classification = %q", got)
	}
	if got := stateDiagnosticClass(testState(312)); got != "312" {
		t.Fatalf("312 classification = %q", got)
	}
	if got := stateDiagnosticClass(""); got != "empty" {
		t.Fatalf("empty classification = %q", got)
	}
	if got := stateDiagnosticClass("opaque"); got != "other" {
		t.Fatalf("other classification = %q", got)
	}
	match := egressMatch("203.0.113.18", "203.0.113.18")
	if match == nil || !*match {
		t.Fatal("equal egress IPs did not match")
	}
	mismatch := egressMatch("203.0.113.18", "198.51.100.24")
	if mismatch == nil || *mismatch {
		t.Fatal("different egress IPs incorrectly matched")
	}
	if egressMatch("", "198.51.100.24") != nil {
		t.Fatal("unknown egress should not be reported as a match")
	}
	if safeEgressIP("not-an-ip") != "" {
		t.Fatal("invalid egress value was accepted")
	}
}
