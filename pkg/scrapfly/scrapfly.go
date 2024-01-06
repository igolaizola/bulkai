package scrapfly

const (
	// JA3URL is the URL to the JA3 API
	FPJA3URL = "https://tools.scrapfly.io/api/fp/ja3"
	// FPAkamaiURL is the URL to the HTTP2 API
	FPAkamaiURL = "https://tools.scrapfly.io/api/fp/akamai"
)

type FPJA3 struct {
	JA3    string `json:"ja3"`
	Digest string `json:"digest"`
}

type InfoHTTP2 struct {
	Headers map[string]string `json:"headers"`
}
