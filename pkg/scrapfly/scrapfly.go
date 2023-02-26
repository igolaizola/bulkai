package scrapfly

const (
	// JA3URL is the URL to the JA3 API
	FPJA3URL = "https://tools.scrapfly.io/api/fp/ja3"
	// HTTPURL is the URL to the HTTP API
	InfoHTTPURL = "https://tools.scrapfly.io/api/info/http"
)

type FPJA3 struct {
	JA3    string `json:"ja3"`
	Digest string `json:"digest"`
}

type InfoHTTP struct {
	Headers Headers `json:"headers"`
}

type Headers struct {
	UserAgent     UserAgent           `json:"user_agent"`
	RawHeaders    []string            `json:"raw_headers"`
	ParsedHeaders map[string][]string `json:"parsed_headers"`
}

type UserAgent struct {
	Payload string `json:"payload"`
}
