package http

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	http "github.com/Danny-Dasilva/fhttp"
	"github.com/igolaizola/bulkai/pkg/scrapfly"
)

func TestBrowser(t *testing.T) {
	t.Skip("TODO: fix this test")

	ja3 := "772,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,23-16-51-27-10-11-35-17513-18-65281-0-45-43-5-13,29-23-24,0"
	userAgent := "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36"
	lang := "en-US,en;q=0.9,es;q=0.8"

	client, err := NewGoClient(ja3, userAgent, lang, "")
	if err != nil {
		t.Fatal(err)
	}

	// Obtain ja3
	u := strings.Replace(scrapfly.FPJA3URL, "https", "http", 1)
	resp, err := client.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var fpJA3 scrapfly.FPJA3
	if err := json.NewDecoder(resp.Body).Decode(&fpJA3); err != nil {
		t.Fatal(err)
	}
	if fpJA3.JA3 != ja3 {
		t.Fatalf("got: %s, want: %s", fpJA3.JA3, ja3)
	}

	// Obtain http info
	u = strings.Replace(scrapfly.FPAkamaiURL, "https", "http", 1)
	resp, err = client.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var infoHTTP2 scrapfly.InfoHTTP2
	if err := json.Unmarshal(raw, &infoHTTP2); err != nil {
		t.Fatal(err)
	}
	headers := infoHTTP2.Headers

	want := map[string]string{
		":method":    "GET",
		":authority": "tools.scrapfly.io",
		":scheme":    "https",
		":path":      "/api/info/http",
		"user-agent": userAgent,
	}
	for k, v := range want {
		if headers[k] != v {
			t.Errorf("header %s got %s, want %s", k, headers[k], v)
		}
	}

	// Obtain http info without proxy
	directClient, err := NewClient(ja3, userAgent, lang, "")
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := directClient.Get(scrapfly.FPAkamaiURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	raw2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(raw2) {
		t.Errorf("proxy \n%s\nno proxy\n%s", string(raw), string(raw2))
	}
}

func TestHeaders(t *testing.T) {
	t.Skip("TODO: fix this test")

	ja3 := "772,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,23-16-51-27-10-11-35-17513-18-65281-0-45-43-5-13,29-23-24,0"
	userAgent := "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36"
	lang := "en-US,en;q=0.9,es;q=0.8"

	want := `:authority: tools.scrapfly.io :method: GET :path: /api/info/http :scheme: https ccc: ccc aaa: aaa bbb: bbb accept-language: en-US,en;q=0.9,es;q=0.8 sec-ch-ua: \"Not A;Brand\";v=\"99\", \"Google Chrome\";v=\"109\", \"Chromium\";v=\"109\ sec-ch-ua-mobile: ?0 sec-ch-ua-platform: \"Windows\ user-agent: Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36`

	client, err := NewClient(ja3, userAgent, lang, "")
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodGet, scrapfly.FPAkamaiURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("aaa", "aaa")
	req.Header.Set("ccc", "ccc")
	req.Header.Set("bbb", "bbb")

	// Add headers order
	req.Header[http.HeaderOrderKey] = []string{
		"ccc",
		"aaa",
		"bbb",
		"accept",
		"accept-encoding",
		"accept-language",
		"cookie",
		"origin",
		"referer",
		"sec-ch-ua",
		"sec-ch-ua-mobile",
		"sec-ch-ua-platform",
		"sec-fetch-dest",
		"sec-fetch-mode",
		"sec-fetch-site",
		"sec-fetch-user",
		"upgrade-insecure-requests",
		"user-agent",
	}
	req.Header[http.PHeaderOrderKey] = []string{
		":authority",
		":method",
		":path",
		":scheme",
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var infoHTTP2 scrapfly.InfoHTTP2
	if err := json.Unmarshal(raw, &infoHTTP2); err != nil {
		t.Fatal(err)
	}
	var got string
	for k, v := range infoHTTP2.Headers {
		got += k + ": " + v + " "
	}
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
