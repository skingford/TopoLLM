package relay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// WriteJSON 将上游非流式响应原样写回客户端，并尽力解析 usage。
func WriteJSON(w http.ResponseWriter, resp *http.Response) (*Usage, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)

	var parsed struct {
		Usage *Usage `json:"usage"`
	}
	_ = json.Unmarshal(body, &parsed)
	return parsed.Usage, nil
}

// WriteStream 将上游 SSE 流逐行转发，并尽力从 data 帧解析 usage。
func WriteStream(w http.ResponseWriter, resp *http.Response) (*Usage, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)

	var usage *Usage
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			_, _ = w.Write(line)
			if flusher != nil {
				flusher.Flush()
			}
			if u := parseUsageLine(line); u != nil {
				usage = u
			}
		}
		if err != nil {
			if err == io.EOF {
				return usage, nil
			}
			return usage, err
		}
	}
}

func parseUsageLine(line []byte) *Usage {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return nil
	}
	payload := bytes.TrimSpace(trimmed[len("data:"):])
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return nil
	}
	var parsed struct {
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil
	}
	return parsed.Usage
}
