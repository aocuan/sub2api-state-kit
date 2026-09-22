package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	pluginv1 "github.com/zhang2580384/sub2api-state-kit/plugin/internal/pluginapi/v1"
)

const (
	fenjueUserAgent = "codex_cli_rs/0.155.0"
	fenjueVersion   = "0.155.0"
	fenjueBeta      = "responses_websockets=2026-02-06"
	fenjueLite      = "X-OpenAI-Internal-Codex-Responses-Lite"
)

var errMintRejected = errors.New("mint pair rejected")

// goodState accepts both personal (292) and team (332) tickets. 312 is degraded.
func goodState(s string) bool {
	return validState(s, 292) || validState(s, 332)
}

func fenjueBody(model string) []byte {
	payload := map[string]any{
		"model":        model,
		"instructions": "Reply with OK. Do not call tools.",
		"input": []any{
			map[string]any{
				"type": "additional_tools",
				"role": "developer",
				"tools": []any{map[string]any{
					"type":        "namespace",
					"name":        "codex",
					"description": "local tools",
					"tools": []any{map[string]any{
						"type":        "function",
						"name":        "noop",
						"description": "Do nothing.",
						"strict":      false,
						"parameters": map[string]any{
							"type":                 "object",
							"properties":           map[string]any{},
							"additionalProperties": false,
						},
					}},
				}},
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{map[string]any{
					"type": "input_text",
					"text": "Reply with OK. Do not call tools.",
				}},
			},
		},
		"stream":              true,
		"store":               false,
		"parallel_tool_calls": false,
		"include":             []string{"reasoning.encrypted_content"},
		"reasoning":           map[string]any{"context": "all_turns"},
	}
	body, _ := json.Marshal(payload)
	return body
}

func cookiePair(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	found := map[string]string{}
	for _, cookie := range resp.Cookies() {
		if cookie == nil || (cookie.Name != "__cflb" && cookie.Name != "__oailb") {
			continue
		}
		value := strings.TrimSpace(cookie.Value)
		if value == "" || strings.ContainsAny(value, "\r\n;") {
			continue
		}
		found[cookie.Name] = value
	}
	if found["__cflb"] == "" || found["__oailb"] == "" {
		return ""
	}
	return "__cflb=" + found["__cflb"] + "; __oailb=" + found["__oailb"]
}

func (e *Engine) fenjueRequest(ctx context.Context, identity *pluginv1.ResolveOutboundIdentityResponse, model, state, cookie string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.probeURL, bytes.NewReader(fenjueBody(model)))
	if err != nil {
		return nil, errors.New("probe construction failed")
	}
	for name, values := range identity.Headers {
		if values == nil || strings.EqualFold(name, "Cookie") {
			continue
		}
		for _, value := range values.Values {
			req.Header.Add(name, value)
		}
	}
	req.Header.Set("Authorization", "Bearer "+identity.Token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", fenjueBeta)
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("User-Agent", fenjueUserAgent)
	req.Header.Set("Version", fenjueVersion)
	req.Header.Set(fenjueLite, "true")
	for key := range req.Header {
		if strings.EqualFold(key, StateHeader) || strings.EqualFold(key, "Cookie") {
			delete(req.Header, key)
		}
	}
	if state != "" {
		req.Header.Set(StateHeader, state)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	return req, nil
}

func readProbe(model string, response *http.Response) (state, cookie, actual string, status int, err error) {
	status = response.StatusCode
	state = strings.TrimSpace(response.Header.Get(StateHeader))
	cookie = cookiePair(response)
	if status != http.StatusOK {
		return state, cookie, "", status, errors.New("probe request rejected")
	}
	observer := newCompletionObserver(model)
	buf := make([]byte, 16*1024)
	total := 0
	for {
		n, readErr := response.Body.Read(buf)
		if n > 0 {
			total += n
			if total > 4*1024*1024 {
				return state, cookie, observer.ActualModel(), status, errors.New("probe response too large")
			}
			observer.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return state, cookie, observer.ActualModel(), status, errors.New("probe response interrupted")
		}
	}
	observer.Finish()
	complete, matches := observer.Result()
	if !complete || !matches {
		return state, cookie, observer.ActualModel(), status, errors.New("probe did not complete with requested model")
	}
	return state, cookie, observer.ActualModel(), status, nil
}

func (e *Engine) exchange(ctx context.Context, client *http.Client, identity *pluginv1.ResolveOutboundIdentityResponse, model, state, cookie string) (string, string, string, int, error) {
	req, err := e.fenjueRequest(ctx, identity, model, state, cookie)
	if err != nil {
		return "", "", "", 0, err
	}
	response, err := client.Do(req)
	if err != nil {
		return "", "", "", 0, errors.New("probe transport failed")
	}
	defer response.Body.Close()
	return readProbe(model, response)
}

// mintAndConfirm mints on one connection, keeps the cookie pair from that
// response, and confirms the ticket on the same connection before it is stored.
func (e *Engine) mintAndConfirm(ctx context.Context, identity *pluginv1.ResolveOutboundIdentityResponse, model, proxyURL, upstreamProxyURL string) (string, string, int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	client, err := makeHTTPClient(proxyURL, upstreamProxyURL, false)
	if err != nil {
		return "", "", 0, "", errors.New("probe transport unavailable")
	}
	defer client.CloseIdleConnections()
	state, cookie, actual, status, err := e.exchange(ctx, client, identity, model, "", "")
	if err != nil {
		return state, "", status, actual, err
	}
	if !goodState(state) || cookie == "" {
		return state, "", status, actual, errMintRejected
	}
	confirmState, _, confirmModel, confirmStatus, err := e.exchange(ctx, client, identity, model, state, cookie)
	if err != nil || validState(confirmState, 312) {
		if err == nil {
			err = errMintRejected
		}
		return state, "", confirmStatus, confirmModel, err
	}
	return state, cookie, confirmStatus, confirmModel, nil
}
