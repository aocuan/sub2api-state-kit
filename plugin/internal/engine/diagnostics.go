package engine

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxDiagnosticEvents = 240

// diagnosticEvent intentionally contains fingerprints and classifications only.
// Raw STATE values, OAuth tokens, proxy credentials, and request headers are
// never copied into runtime logs.
type diagnosticEvent struct {
	Seq           uint64 `json:"seq"`
	Timestamp     string `json:"time"`
	AccountID     int64  `json:"account_id,omitempty"`
	Model         string `json:"model,omitempty"`
	Stage         string `json:"stage"`
	Attempt       int    `json:"attempt,omitempty"`
	UpstreamProxy string `json:"upstream_proxy,omitempty"`
	TargetProxy   string `json:"target_proxy,omitempty"`
	CaptureEgress string `json:"capture_egress,omitempty"`
	FixedEgress   string `json:"fixed_egress,omitempty"`
	EgressMatch   *bool  `json:"egress_match,omitempty"`
	StateLength   int    `json:"state_length,omitempty"`
	StateClass    string `json:"state_class,omitempty"`
	ResponseModel string `json:"response_model,omitempty"`
	HTTPStatus    int    `json:"http_status,omitempty"`
	Outcome       string `json:"outcome"`
	Error         string `json:"error,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
}

func (e *Engine) diagnosticsEnabled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config.DiagnosticLogEnabled
}

func (e *Engine) recordDiagnostic(event diagnosticEvent) {
	if event.Stage == "" || event.Outcome == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.config.DiagnosticLogEnabled {
		return
	}
	e.diagnosticSeq++
	event.Seq = e.diagnosticSeq
	event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	event.Model = safeDiagnosticModel(event.Model)
	event.UpstreamProxy = diagnosticProxyEndpoint(event.UpstreamProxy)
	event.TargetProxy = diagnosticProxyEndpoint(event.TargetProxy)
	event.CaptureEgress = safeEgressIP(event.CaptureEgress)
	event.FixedEgress = safeEgressIP(event.FixedEgress)
	event.StateClass = safeDiagnosticCode(event.StateClass, 16)
	event.ResponseModel = safeDiagnosticModel(event.ResponseModel)
	event.Outcome = safeDiagnosticCode(event.Outcome, 64)
	event.Error = safeDiagnosticCode(event.Error, 80)
	e.diagnostics = append(e.diagnostics, event)
	if len(e.diagnostics) > maxDiagnosticEvents {
		copy(e.diagnostics, e.diagnostics[len(e.diagnostics)-maxDiagnosticEvents:])
		e.diagnostics = e.diagnostics[:maxDiagnosticEvents]
	}
}

func diagnosticProxyEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	replaced := strings.NewReplacer("{sid}", "123456", "{random}", "123456").Replace(raw)
	u, err := parseProxyURL(replaced)
	if err != nil {
		return "invalid"
	}
	return u.Scheme + "://" + u.Host
}

func stateDiagnosticClass(state string) string {
	switch {
	case state == "":
		return "empty"
	case validState(state, 292):
		return "292"
	case validState(state, 312):
		return "312"
	case validState(state, 332):
		return "332"
	default:
		return "other"
	}
}

func safeDiagnosticModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if modelPattern.MatchString(model) {
		return model
	}
	return "unknown"
}

func safeDiagnosticCode(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	result := make([]rune, 0, len(value))
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			result = append(result, r)
		} else {
			result = append(result, '_')
		}
		if len(result) == limit {
			break
		}
	}
	return string(result)
}

func safeEgressIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return ""
	}
	return ip.String()
}

func egressMatch(capture, fixed string) *bool {
	if capture == "" || fixed == "" {
		return nil
	}
	match := capture == fixed
	return &match
}

func (e *Engine) lookupEgressIP(ctx context.Context, proxyURL, upstreamProxyURL string) string {
	if !e.diagnosticsEnabled() {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org?format=json", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "sub2api-state-kit-diagnostics/1")
	client, err := freshProbeClient(proxyURL, upstreamProxyURL)
	if err != nil {
		return ""
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil || response == nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return ""
	}
	var payload struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(body, &payload) == nil {
		return safeEgressIP(payload.IP)
	}
	return safeEgressIP(string(body))
}
