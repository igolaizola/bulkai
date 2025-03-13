package session

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/igolaizola/bulkai"
	"gopkg.in/yaml.v3"
)

func Run(ctx context.Context, profile bool, output, proxy string) error {
	if output == "" {
		return errors.New("output file is required")
	}
	if fi, err := os.Stat(output); err == nil && fi.IsDir() {
		return fmt.Errorf("output file is a directory: %s", output)
	}

	log.Println("Starting browser")
	defer log.Println("Browser stopped")

	opts := append(
		chromedp.DefaultExecAllocatorOptions[3:],
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
	)

	if proxy != "" {
		opts = append(opts,
			chromedp.ProxyServer(proxy),
		)
	}

	if profile {
		opts = append(opts,
			// if user-data-dir is set, chrome won't load the default profile,
			// even if it's set to the directory where the default profile is stored.
			// set it to empty to prevent chromedp from setting it to a temp directory.
			chromedp.UserDataDir(""),
			chromedp.Flag("disable-extensions", false),
		)
	}

	ctx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	// create chrome instance
	ctx, cancel = chromedp.NewContext(
		ctx,
		// chromedp.WithDebugf(log.Printf),
	)
	defer cancel()

	// disable webdriver
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(cxt context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument("Object.defineProperty(navigator, 'webdriver', { get: () => false, });").Do(cxt)
		if err != nil {
			return err
		}
		return nil
	})); err != nil {
		return fmt.Errorf("could not disable webdriver: %w", err)
	}

	// check if webdriver is disabled
	/*
		if err := chromedp.Run(ctx,
			chromedp.Navigate("https://intoli.com/blog/not-possible-to-block-chrome-headless/chrome-headless-test.html"),
		); err != nil {
			return fmt.Errorf("could not navigate to test page: %w", err)
		}
		<-time.After(1 * time.Second)
	*/

	// obtain ja3
	/*
		var ja3 string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(scrapfly.FPJA3WebURL),
			chromedp.WaitReady("#ja3", chromedp.ByQuery),
		); err != nil {
			return fmt.Errorf("could not navigate to ja3 page: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
		var fpJA3 scrapfly.FPJA3
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`window.fingerprint`, &fpJA3),
		); err != nil {
			return fmt.Errorf("could not obtain fingerprint: %w", err)
		}
		ja3 = fpJA3.JA3
		if ja3 == "" {
			return errors.New("empty ja3")
		}
		log.Println("ja3:", ja3)
	*/

	// obtain user agent
	/*
		var userAgent, acceptLanguage string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(scrapfly.FPHTTP2WebURL),
			chromedp.WaitReady("#http2_headers_frame pre", chromedp.ByQuery),
			chromedp.ActionFunc(func(ctx context.Context) error {
				node, err := dom.GetDocument().Do(ctx)
				if err != nil {
					return fmt.Errorf("couldn't get document: %w", err)
				}
				res, err := dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
				if err != nil {
					return fmt.Errorf("couldn't get outer html: %w", err)
				}
				doc, err := goquery.NewDocumentFromReader(bytes.NewBuffer([]byte(res)))
				if err != nil {
					return fmt.Errorf("couldn't create document: %w", err)
				}
				body := doc.Find("#http2_headers_frame pre").Text()
				if body == "" {
					return errors.New("couldn't obtain info http")
				}
				var infoHTTP2 scrapfly.InfoHTTP2
				log.Println(body)
				if err := json.Unmarshal([]byte(body), &infoHTTP2); err != nil {
					return fmt.Errorf("couldn't unmarshal info http: %w", err)
				}
				if len(infoHTTP2.Headers) == 0 {
					return errors.New("empty headers")
				}
				if _, ok := infoHTTP2.Headers["user-agent"]; !ok {
					return errors.New("empty user agent")
				}
				userAgent = infoHTTP2.Headers["user-agent"][0]
				if userAgent == "" {
					return errors.New("empty user agent")
				}
				log.Println("user-agent:", userAgent)
				v, ok := infoHTTP2.Headers["accept-language"]
				if !ok || len(v) == 0 {
					return errors.New("empty accept language")
				}
				acceptLanguage = strings.Split(v[0], ",")[0]
				log.Println("language:", acceptLanguage)
				return nil
			}),
		); err != nil {
			return fmt.Errorf("could not obtain user agent: %w", err)
		}
		if userAgent == "" {
			return errors.New("empty user agent")
		}
		if acceptLanguage == "" {
			return errors.New("empty accept language")
		}
	*/
	var lck sync.Mutex

	// Obtain discord token
	var token, cookie, xSuperProperties, xDiscordLocale string
	wait, done := context.WithCancel(context.Background())
	defer done()
	chromedp.ListenTarget(
		ctx,
		func(ev interface{}) {
			if e, ok := ev.(*network.EventRequestWillBeSentExtraInfo); ok {
				if !strings.HasPrefix(getHeader(e, "origin"), "https://discord.com") {
					return
				}

				if h := getHeader(e, "x-discord-locale"); h != "" {
					lck.Lock()
					if xDiscordLocale != h {
						xDiscordLocale = h
						log.Println("locale:", xDiscordLocale)
					}
					lck.Unlock()
				}
				if h := getHeader(e, "x-super-properties"); h != "" {
					lck.Lock()
					if xSuperProperties != h {
						xSuperProperties = h
						log.Println("super-properties:", xSuperProperties)
					}
					lck.Unlock()
				}
				if h := getHeader(e, "cookie"); h != "" {
					lck.Lock()
					if cookie != h {
						cookie = h
						log.Println("cookie:", "...redacted...")
					}
					lck.Unlock()
				}
				if h := getHeader(e, "authorization"); h != "" {
					lck.Lock()
					if token != h {
						token = h
						log.Println("token:", "...redacted...")
					}
					lck.Unlock()
				}

				lck.Lock()
				defer lck.Unlock()
				if token != "" && cookie != "" && xSuperProperties != "" && xDiscordLocale != "" {
					done()
				}
			}
		},
	)

	if err := chromedp.Run(ctx,
		// Load google first to have a sane referer
		chromedp.Navigate("https://www.google.com/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Navigate("https://discord.com/login"),
	); err != nil {
		return fmt.Errorf("could not obtain discord data: %w", err)
	}

	select {
	case <-wait.Done():
	case <-ctx.Done():
		return ctx.Err()
	}

	// userAgent = strings.ReplaceAll(userAgent, "\n", "")
	// userAgent = strings.ReplaceAll(userAgent, "like  Gecko", "like Gecko")
	cookie = strings.ReplaceAll(cookie, "\n", "")
	cookie = strings.ReplaceAll(cookie, ";  ", "; ")

	// save session
	session := &bulkai.Session{
		// JA3:             ja3,
		// UserAgent:       userAgent,
		Token:           token,
		SuperProperties: xSuperProperties,
		Locale:          xDiscordLocale,
		Cookie:          cookie,
		// Language:        acceptLanguage,
	}
	data, err := yaml.Marshal(session)
	if err != nil {
		return fmt.Errorf("couldn't marshal session: %w", err)
	}
	log.Println("Session successfully obtained")

	// If the file already exists, copy it to a backup file
	if _, err := os.Stat(output); err == nil {
		backup := output
		ext := filepath.Ext(backup)
		// Remove the extension from the output
		backup = strings.TrimSuffix(backup, ext)
		// Add a timestamp to the backup file
		backup = fmt.Sprintf("%s_%s%s", backup, time.Now().Format("20060102150405"), ext)
		if err := os.Rename(output, backup); err != nil {
			return fmt.Errorf("couldn't backup session: %w", err)
		}
		log.Println("Previous session backed up to", backup)
	}

	// Write the session to the output file
	if err := os.WriteFile(output, data, 0644); err != nil {
		return fmt.Errorf("couldn't write session: %w", err)
	}
	log.Println("Session saved to", output)
	return nil
}

func getHeader(e *network.EventRequestWillBeSentExtraInfo, k string) string {
	v := e.Headers[k]
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
