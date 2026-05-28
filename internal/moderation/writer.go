// Package moderation 对网关的输出内容做敏感词审核：
// 流式——增量扫描 delta.content，命中即截断并发出 content_filter 结束帧；
// 非流式——缓冲整体响应，命中则整体替换为拒绝。
package moderation

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// Moderator 持有审核配置，按需包装 ResponseWriter。
type Moderator struct {
	enabled bool
	words   [][]byte
}

// New 构建审核器；未启用或词表为空时视为关闭。
func New(enabled bool, words []string) *Moderator {
	var bw [][]byte
	for _, w := range words {
		if w != "" {
			bw = append(bw, []byte(w))
		}
	}
	return &Moderator{enabled: enabled && len(bw) > 0, words: bw}
}

// Enabled 返回是否启用。
func (m *Moderator) Enabled() bool { return m.enabled }

// Wrap 返回包装后的审核 Writer。
func (m *Moderator) Wrap(w http.ResponseWriter, stream bool) *Writer {
	flusher, _ := w.(http.Flusher)
	return &Writer{ResponseWriter: w, words: m.words, stream: stream, flusher: flusher}
}

// Writer 包装 http.ResponseWriter 做输出审核。实现 http.ResponseWriter 与 http.Flusher。
type Writer struct {
	http.ResponseWriter
	words   [][]byte
	stream  bool
	flusher http.Flusher

	// 流式状态
	acc     []byte
	blocked bool

	// 非流式状态
	buf        bytes.Buffer
	statusCode int
}

// Flush 实现 http.Flusher。
func (w *Writer) Flush() {
	if w.flusher != nil {
		w.flusher.Flush()
	}
}

// Blocked 返回本次响应是否因审核被截断或整体替换。
func (w *Writer) Blocked() bool { return w.blocked }

// WriteHeader：流式立即转发；非流式延迟到 Finalize（以便整体替换）。
func (w *Writer) WriteHeader(status int) {
	if w.stream {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.statusCode = status
}

func (w *Writer) Write(p []byte) (int, error) {
	if !w.stream {
		return w.buf.Write(p)
	}
	if w.blocked {
		return len(p), nil // 已截断，丢弃后续帧
	}
	if content := extractDeltaContent(p); content != "" {
		w.acc = append(w.acc, content...)
		if w.hit(w.acc) {
			w.emitBlocked()
			w.blocked = true
			return len(p), nil // 扣下违规帧，对上游伪装为已写入
		}
	}
	return w.ResponseWriter.Write(p)
}

// Finalize 完成非流式响应的审核与落地（流式已内联处理）。
func (w *Writer) Finalize() {
	if w.stream {
		return
	}
	body := w.buf.Bytes()
	if w.hit([]byte(extractMessageContent(body))) {
		w.blocked = true
		w.ResponseWriter.Header().Set("Content-Type", "application/json")
		w.ResponseWriter.WriteHeader(http.StatusOK)
		_, _ = w.ResponseWriter.Write([]byte(`{"error":{"message":"response blocked by content policy","type":"content_filter"}}`))
		return
	}
	if w.statusCode != 0 {
		w.ResponseWriter.WriteHeader(w.statusCode)
	}
	_, _ = w.ResponseWriter.Write(body)
}

func (w *Writer) hit(text []byte) bool {
	for _, word := range w.words {
		if bytes.Contains(text, word) {
			return true
		}
	}
	return false
}

func (w *Writer) emitBlocked() {
	_, _ = w.ResponseWriter.Write([]byte(`data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}` + "\n\n"))
	_, _ = w.ResponseWriter.Write([]byte("data: [DONE]\n\n"))
	if w.flusher != nil {
		w.flusher.Flush()
	}
}

func extractDeltaContent(line []byte) string {
	t := bytes.TrimSpace(line)
	if !bytes.HasPrefix(t, []byte("data:")) {
		return ""
	}
	payload := bytes.TrimSpace(t[len("data:"):])
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return ""
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(payload, &chunk) != nil || len(chunk.Choices) == 0 {
		return ""
	}
	return chunk.Choices[0].Delta.Content
}

func extractMessageContent(body []byte) string {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &resp) != nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}
