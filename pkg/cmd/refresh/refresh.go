package refresh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/igolaizola/bulkai/pkg/discord"
	"github.com/igolaizola/bulkai/pkg/fhttp"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Debug       bool          `yaml:"debug"`
	Proxy       string        `yaml:"proxy"`
	Wait        time.Duration `yaml:"wait"`
	Input       string        `yaml:"input"`
	Output      string        `yaml:"output"`
	SessionFile string        `yaml:"session"`
	Session     Session       `yaml:"-"`
}

type Session struct {
	JA3             string `yaml:"ja3"`
	UserAgent       string `yaml:"user-agent"`
	Language        string `yaml:"language"`
	Token           string `yaml:"token"`
	SuperProperties string `yaml:"super-properties"`
	Locale          string `yaml:"locale"`
	Cookie          string `yaml:"cookie"`
}

func Run(ctx context.Context, cfg *Config) error {
	if cfg.Session.Token == "" {
		return errors.New("missing token")
	}
	if cfg.Session.JA3 == "" {
		return errors.New("missing ja3")
	}
	if cfg.Session.UserAgent == "" {
		return errors.New("missing user agent")
	}
	if cfg.Session.Cookie == "" {
		return errors.New("missing cookie")
	}
	if cfg.Session.Language == "" {
		return errors.New("missing language")
	}
	if cfg.Input == "" {
		return errors.New("missing input file")
	}
	if cfg.Output == "" {
		return errors.New("missing output file")
	}

	// Load input file
	b, err := os.ReadFile(cfg.Input)
	if err != nil {
		return err
	}

	// Create output directory
	if err := os.MkdirAll(filepath.Dir(cfg.Output), 0755); err != nil {
		return err
	}

	// Find all CDN URLs
	urls := cdnReg.FindAllString(string(b), -1)
	if len(urls) == 0 {
		log.Println("no URLs found")
		return nil
	}

	// Create a unique list of URLs
	var unique []string
	lookup := map[string]struct{}{}
	for _, url := range urls {
		if _, ok := lookup[url]; !ok {
			lookup[url] = struct{}{}
			unique = append(unique, url)
		}
	}

	// Create http client
	httpClient, err := fhttp.NewClient(1*time.Minute, true, cfg.Proxy)
	if err != nil {
		return fmt.Errorf("couldn't create http client: %w", err)
	}

	// Set proxy
	if cfg.Proxy != "" {
		p := strings.TrimPrefix(cfg.Proxy, "http://")
		p = strings.TrimPrefix(p, "https://")
		os.Setenv("HTTPS_PROXY", p)
		os.Setenv("HTTP_PROXY", p)
	}

	if err := fhttp.SetCookies(httpClient, "https://discord.com", cfg.Session.Cookie); err != nil {
		return fmt.Errorf("couldn't set cookies: %w", err)
	}
	defer func() {
		cookie, err := fhttp.GetCookies(httpClient, "https://discord.com")
		if err != nil {
			log.Printf("couldn't get cookies: %v\n", err)
		}
		cfg.Session.Cookie = strings.ReplaceAll(cookie, "\n", "")
		// TODO: save session to common method
		data, err := yaml.Marshal(cfg.Session)
		if err != nil {
			log.Println(fmt.Errorf("couldn't marshal session: %w", err))
		}
		if err := os.WriteFile(cfg.SessionFile, data, 0644); err != nil {
			log.Println(fmt.Errorf("couldn't write session: %w", err))
		}
	}()

	// Create discord client
	client, err := discord.New(ctx, &discord.Config{
		Token:           cfg.Session.Token,
		SuperProperties: cfg.Session.SuperProperties,
		Locale:          cfg.Session.Locale,
		UserAgent:       cfg.Session.UserAgent,
		HTTPClient:      httpClient,
		Debug:           cfg.Debug,
	})
	if err != nil {
		return fmt.Errorf("couldn't create discord client: %w", err)
	}

	// Start discord client
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("couldn't start discord client: %w", err)
	}

	// Refresh URLs
	wait := 10 * time.Millisecond
	for _, cdnURL := range unique {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = cfg.Wait

		// Refresh URL
		u := "attachments/refresh-urls"
		shortURL := strings.Split(cdnURL, "?")[0]
		req := &refreshURLsRequest{
			AttachmentURLs: []string{shortURL},
		}
		resp, err := client.Do(ctx, "POST", u, req)
		if err != nil {
			return fmt.Errorf("couldn't refresh URL (%s): %w", cdnURL, err)
		}
		var refreshResp refreshURLsResponse
		if err := json.Unmarshal(resp, &refreshResp); err != nil {
			return fmt.Errorf("couldn't unmarshal response %s: %w", string(resp), err)
		}
		if len(refreshResp.RefreshedURLs) == 0 {
			return fmt.Errorf("no refreshed URLs found (%s)", cdnURL)
		}
		refURL := refreshResp.RefreshedURLs[0]
		if refURL.Refreshed == "" {
			return fmt.Errorf("no refreshed URL found (%s)", cdnURL)
		}
		// Replace URL in input file
		b = []byte(strings.ReplaceAll(string(b), cdnURL, refURL.Refreshed))
	}

	// Save output file
	if err := os.WriteFile(cfg.Output, b, 0644); err != nil {
		return err
	}
	return nil
}

var cdnReg = regexp.MustCompile(`https:\/\/cdn\.discordapp\.com\/attachments\/[^"\s,]+`)

type refreshURLsRequest struct {
	AttachmentURLs []string `json:"attachment_urls"`
}

type refreshURLsResponse struct {
	RefreshedURLs []struct {
		Original  string `json:"original"`
		Refreshed string `json:"refreshed"`
	} `json:"refreshed_urls"`
}
