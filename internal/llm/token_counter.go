package llm

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"

	"go_study/internal/metrics"

	"github.com/pkoukk/tiktoken-go"
)

var tke *tiktoken.Tiktoken

func InitTiktoken() {
	var err error
	tke, err = tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		panic(err)
	}
}

func CountTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(tke.Encode(text, nil, nil))
}

type TokenCounter struct {
	reader      io.ReadCloser
	scanner     *bufio.Scanner
	apikey      string
	quota       *TokenQuota
	totalTokens int

	startTime      time.Time
	firstTokenTime time.Time
	modelName      string
}

func NewTokenCounter(reader io.ReadCloser, apikey string, quota *TokenQuota, startTime time.Time) *TokenCounter {
	return &TokenCounter{
		reader:    reader,
		scanner:   bufio.NewScanner(reader),
		apikey:    apikey,
		quota:     quota,
		startTime: startTime,
		modelName: "mock-llm",
	}
}

func (tc *TokenCounter) Close() error {
	return tc.reader.Close()
}

func (tc *TokenCounter) Read(p []byte) (int, error) {
	if tc.scanner.Scan() {
		line := tc.scanner.Text()
		if strings.HasPrefix(line, "data:") {
			dataStr := strings.TrimPrefix(line, "data:")
			if dataStr != "[DONE]" {
				var payload struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal([]byte(dataStr), &payload); err == nil {
					tokens := CountTokens(payload.Text)
					if tokens > 0 {
						if tc.firstTokenTime.IsZero() {
							tc.firstTokenTime = time.Now()
							ttft := tc.firstTokenTime.Sub(tc.startTime).Seconds()
							metrics.LlmTimeToFirstToken.WithLabelValues(tc.modelName).Observe(ttft)
						}

						tc.totalTokens += tokens
						tc.quota.AddUsage(tc.apikey, tokens)
						metrics.LlmTotalTokensGenerated.WithLabelValues(tc.modelName).Add(float64(tokens))

						if tc.quota.IsExceeded(tc.apikey) {
							errData := `data:{"error":"quota exceeded"}` + "\n\n"
							copy(p, []byte(errData))
							return len(errData), io.EOF
						}
					}
				}
			}
		}
		copy(p, []byte(line+"\n\n"))
		return len(line) + 2, nil
	}
	if err := tc.scanner.Err(); err != nil {
		return 0, err
	}
	return 0, io.EOF
}
