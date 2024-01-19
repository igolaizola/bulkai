package scrapfly

const (
	// JA3URL is the URL to the JA3 API
	FPJA3URL = "https://tools.scrapfly.io/api/fp/ja3"
	// JA3WebURL is the URL to the web JA3 tool
	FPJA3WebURL = "https://scrapfly.io/web-scraping-tools/ja3-fingerprint"
	// FPAkamaiURL is the URL to the HTTP2 API
	FPAkamaiURL = "https://tools.scrapfly.io/api/fp/akamai"
	// FPHTTP2WebURL is the URL to the web HTTP2 tool
	FPHTTP2WebURL = "https://scrapfly.io/web-scraping-tools/http2-fingerprint"
)

type FPJA3 struct {
	JA3    string `json:"ja3"`
	Digest string `json:"digest"`
}

type InfoHTTP2 struct {
	Headers map[string][]string `json:"headers"`
}
