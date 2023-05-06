package discord

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	http "github.com/Danny-Dasilva/fhttp"
	"github.com/andybalholm/brotli"
	"github.com/bwmarrin/discordgo"
)

type Client struct {
	Referer string

	token           string
	userID          string
	superProperties *SuperProperties
	locale          string
	userAgent       string
	client          *http.Client
	session         *discordgo.Session
	callbacks       []func(*discordgo.Event)
	dm              map[string]string
	debug           bool

	callbackLck *sync.Mutex
	doLck       *sync.Mutex
	downloadLck *sync.Mutex
}

type Config struct {
	Token           string
	SuperProperties string
	Locale          string
	UserAgent       string
	Referer         string
	HTTPClient      *http.Client
	Dialer          func(ctx context.Context, network, addr string) (net.Conn, error)
	Debug           bool
}

type SuperProperties struct {
	OS                  string      `json:"os"`
	Browser             string      `json:"browser"`
	Device              string      `json:"device"`
	SystemLocale        string      `json:"system_locale"`
	BrowserUserAgent    string      `json:"browser_user_agent"`
	BrowserVersion      string      `json:"browser_version"`
	OSVersion           string      `json:"os_version"`
	Referrer            string      `json:"referrer"`
	ReferringDomain     string      `json:"referring_domain"`
	ReferrerCurrent     string      `json:"referrer_current"`
	ReferringDomainCurr string      `json:"referring_domain_current"`
	ReleaseChannel      string      `json:"release_channel"`
	ClientBuildNumber   int         `json:"client_build_number"`
	ClientEventSource   interface{} `json:"client_event_source"`
	raw                 string
}

func (s *SuperProperties) Unmarshal() error {
	if s.raw == "" {
		return fmt.Errorf("discord: super properties are empty")
	}
	b, err := base64.StdEncoding.DecodeString(s.raw)
	if err != nil {
		return fmt.Errorf("discord: couldn't decode super properties: %w", err)
	}
	if err := json.Unmarshal(b, s); err != nil {
		return fmt.Errorf("discord: couldn't unmarshal super properties: %w", err)
	}
	return nil
}

func (s *SuperProperties) Marshal() error {
	js, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("discord: couldn't marshal super properties: %w", err)
	}
	s.raw = base64.StdEncoding.EncodeToString(js)
	return nil
}

func New(ctx context.Context, cfg *Config) (*Client, error) {
	// Parse super properties
	superProperties := &SuperProperties{
		raw: cfg.SuperProperties,
	}
	if err := superProperties.Unmarshal(); err != nil {
		return nil, err
	}
	split := strings.SplitN(cfg.Token, ".", 2)
	if len(split) != 2 {
		return nil, fmt.Errorf("discord: couldn't parse token")
	}
	userID, err := base64.RawStdEncoding.DecodeString(split[0])
	if err != nil {
		return nil, fmt.Errorf("discord: couldn't decode user id %s: %w", string(split[0]), err)
	}

	session, err := newSession(cfg.Dialer, cfg.Token, cfg.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("discord: couldn't create session: %w", err)
	}

	c := &Client{
		token:           cfg.Token,
		userID:          string(userID),
		superProperties: superProperties,
		locale:          cfg.Locale,
		userAgent:       cfg.UserAgent,
		Referer:         cfg.Referer,
		client:          cfg.HTTPClient,
		callbacks:       []func(*discordgo.Event){},
		session:         session,
		dm:              make(map[string]string),
		debug:           cfg.Debug,
		callbackLck:     &sync.Mutex{},
		doLck:           &sync.Mutex{},
		downloadLck:     &sync.Mutex{},
	}
	return c, nil
}

func (c *Client) Session() string {
	return c.session.State.SessionID
}

func (c *Client) OnEvent(callback func(*discordgo.Event)) {
	c.callbackLck.Lock()
	defer c.callbackLck.Unlock()
	c.callbacks = append(c.callbacks, callback)
}

func (c *Client) DM(userID string) string {
	return c.dm[userID]
}

func (c *Client) Start(ctx context.Context) error {
	c.session.AddHandler(func(s *discordgo.Session, e interface{}) {
		evt, ok := e.(*discordgo.Event)
		if !ok {
			return
		}
		c.callbackLck.Lock()
		defer c.callbackLck.Unlock()
		for _, callback := range c.callbacks {
			callback(evt)
		}
	})
	if err := c.session.Open(); err != nil {
		return fmt.Errorf("discord: couldn't open session: %w", err)
	}
	for _, p := range c.session.State.PrivateChannels {
		if len(p.Recipients) != 1 {
			continue
		}
		c.dm[p.Recipients[0].ID] = p.ID
	}

	return nil
}

func (c *Client) Stop() error {
	return c.session.Close()
}

func (c *Client) Do(ctx context.Context, method string, path string, body interface{}) ([]byte, error) {
	var data []byte
	err := retry(ctx, 3, func() error {
		b, err := c.do(method, path, body)
		if err != nil {
			return err
		}
		data = b
		return nil
	})
	return data, err
}

var errBadGateway = errors.New("discord: bad gateway")

type Error struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	temporary bool
}

func (e Error) Error() string {
	return fmt.Sprintf("discord: %s (%d)", e.Message, e.Code)
}

func (e Error) Temporary() bool {
	return e.temporary
}

var ErrMessageNotFound = &Error{Message: "Unknown Message", Code: 10008, temporary: false}

func parseError(raw string) error {
	var err Error
	if err := json.Unmarshal([]byte(raw), &err); err != nil {
		return nil
	}
	err.temporary = true
	switch err.Code {
	case 10008:
		return ErrMessageNotFound
	default:
		return err
	}
}

func (c *Client) do(method string, path string, body interface{}) ([]byte, error) {
	// Rate limit
	c.doLck.Lock()
	defer func() {
		rnd, _ := rand.Int(rand.Reader, big.NewInt(1000))
		ms := time.Duration(int(rnd.Int64())) * time.Millisecond
		time.Sleep(2*time.Second + ms)
		c.doLck.Unlock()
	}()

	// Create request
	path = strings.TrimPrefix(path, "/")
	u := fmt.Sprintf("https://discord.com/api/v9/%s", path)
	var r io.Reader

	logMsg := fmt.Sprintf("REQ %s\n", u)

	var webkitID string
	if body != nil {
		js, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("discord: couldn't marshal body: %w", err)
		}
		if path == "interactions" {
			js, webkitID = webkitForm(js)
		}
		r = bytes.NewReader(js)
		logMsg += fmt.Sprintf("%s\n", js)
	}
	req, err := http.NewRequest(method, u, r)
	if err != nil {
		return nil, fmt.Errorf("discord: couldn't create request: %w", err)
	}
	c.addHeaders(req)
	if webkitID != "" {
		req.Header.Set("content-type", fmt.Sprintf("multipart/form-data; boundary=----WebKitFormBoundary%s", webkitID))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: couldn't do request %s: %w", path, err)
	}
	defer resp.Body.Close()

	// Handle compression
	var respBody io.Reader
	respBody = resp.Body
	switch resp.Header.Get("content-encoding") {
	case "br":
		respBody = brotli.NewReader(resp.Body)
	case "gzip":
		respBody, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("discord: couldn't create gzip reader: %w", err)
		}
	case "deflate":
		respBody, err = zlib.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("discord: couldn't create zlib reader: %w", err)
		}
	}

	data, err := io.ReadAll(respBody)
	if err != nil {
		return nil, fmt.Errorf("discord: couldn't read response body: %w", err)
	}
	logMsg += fmt.Sprintf("%d %s", resp.StatusCode, string(data))
	if c.debug {
		log.Println(logMsg)
	}
	if resp.StatusCode == http.StatusBadGateway {
		return nil, errBadGateway
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if err := parseError(string(data)); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("discord: request %s returned status code %d (%s)", path, resp.StatusCode, string(data))
	}
	return data, nil
}

func (c *Client) Download(ctx context.Context, u string, output string) error {
	return retry(ctx, 5, func() error {
		return c.download(ctx, u, output)
	})
}

func (c *Client) download(ctx context.Context, u string, output string) error {
	// Rate limit
	c.downloadLck.Lock()
	defer func() {
		rnd, _ := rand.Int(rand.Reader, big.NewInt(1000))
		ms := time.Duration(int(rnd.Int64())) * time.Millisecond
		time.Sleep(1*time.Second + ms)
		c.downloadLck.Unlock()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("discord: couldn't create request: %w", err)
	}
	c.addHeaders(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: couldn't do request %s: %w", u, err)
	}
	defer resp.Body.Close()

	// Handle compression
	var respBody io.Reader
	respBody = resp.Body
	switch resp.Header.Get("content-encoding") {
	case "br":
		respBody = brotli.NewReader(resp.Body)
	case "gzip":
		respBody, err = gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("discord: couldn't create gzip reader: %w", err)
		}
	case "deflate":
		respBody, err = zlib.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("discord: couldn't create zlib reader: %w", err)
		}
	}

	if resp.StatusCode == http.StatusBadGateway {
		return errBadGateway
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, err := io.ReadAll(respBody)
		if err != nil {
			return fmt.Errorf("discord: couldn't read response body: %w", err)
		}
		return fmt.Errorf("discord: request %s returned status code %d (%s)", u, resp.StatusCode, string(respBody))
	}
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("discord: couldn't create file %s: %w", output, err)
	}
	defer f.Close()
	if _, err = io.Copy(f, respBody); err != nil {
		return fmt.Errorf("discord: couldn't write to file %s: %w", output, err)
	}
	return nil
}

var backoff = []time.Duration{
	10 * time.Minute,
	30 * time.Minute,
	60 * time.Minute,
}

func retry(ctx context.Context, maxAttempts int, fn func() error) error {
	attempts := 0
	for {
		err := fn()
		if err == nil {
			return nil
		}
		// Increase attempts and check if we should stop
		attempts++
		if attempts >= maxAttempts {
			return err
		}
		// If the error is not temporary, we stop
		var discordErr Error
		if errors.As(err, &discordErr) && !discordErr.Temporary() {
			return err
		}
		// Bad gateway usually means discord is down, so we wait before retrying
		if errors.Is(err, errBadGateway) {
			idx := attempts - 1
			if idx >= len(backoff) {
				idx = len(backoff) - 1
			}
			wait := backoff[idx]
			log.Printf("discord seems to be down, waiting %s before retrying\n", wait)
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
			}
		}
		log.Println("retrying...", err)
	}
}

func webkitForm(input []byte) ([]byte, string) {
	id := webkitID(16)
	output := fmt.Sprintf(`------WebKitFormBoundary%s
Content-Disposition: form-data; name="payload_json"

%s
------WebKitFormBoundary%s--`, id, string(input), id)
	return []byte(output), id
}

var webkitChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"

func webkitID(length int) string {
	b := make([]byte, length)
	rand.Read(b) // generates len(b) random bytes
	for i := 0; i < length; i++ {
		b[i] = webkitChars[int(b[i])%len(webkitChars)]
	}
	return string(b)
}

func (c *Client) addHeaders(req *http.Request) {
	// Add headers
	switch req.URL.Host {
	case "cdn.discordapp.com":
		req.Header = http.Header{
			"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9"},
			"accept-encoding":           {"gzip, deflate, br"},
			"sec-fetch-dest":            {"document"},
			"sec-fetch-mode":            {"navigate"},
			"sec-fetch-site":            {"none"},
			"sec-fetch-user":            {"?1"},
			"upgrade-insecure-requests": {"1"},
		}
	case "discord.com":
		referer := "https://discord.com/channels/@me"
		if c.Referer != "" {
			referer = fmt.Sprintf("https://discord.com/%s", strings.TrimPrefix(c.Referer, "/"))
		}
		switch req.URL.Path {
		case "/api/v9/interactions":
			req.Header = http.Header{
				"accept":             {"*/*"},
				"accept-encoding":    {"gzip, deflate, br"},
				"authorization":      {c.token},
				"origin":             {"https://discord.com"},
				"referer":            {referer},
				"sec-fetch-dest":     {"empty"},
				"sec-fetch-mode":     {"cors"},
				"sec-fetch-site":     {"same-origin"},
				"x-debug-options":    {"bugReporterEnabled"},
				"x-discord-locale":   {c.locale},
				"x-super-properties": {c.superProperties.raw},
			}

		default:
			req.Header = http.Header{
				"accept":             {"*/*"},
				"accept-encoding":    {"gzip, deflate, br"},
				"authorization":      {c.token},
				"referer":            {referer},
				"sec-fetch-dest":     {"empty"},
				"sec-fetch-mode":     {"cors"},
				"sec-fetch-site":     {"same-origin"},
				"x-debug-options":    {"bugReporterEnabled"},
				"x-discord-locale":   {c.locale},
				"x-super-properties": {c.superProperties.raw},
			}
		}
	}
	// Add headers order
	req.Header[http.HeaderOrderKey] = []string{
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
		"x-debug-options",
		"x-discord-locale",
		"x-super-properties",
	}
	req.Header[http.PHeaderOrderKey] = []string{
		":authority",
		":method",
		":path",
		":scheme",
	}
}
