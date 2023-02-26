package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/igolaizola/bulkai"
	"github.com/igolaizola/bulkai/pkg/scrapfly"
	"gopkg.in/yaml.v3"
)

func Run(ctx context.Context, profile bool, output, proxy string) error {
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
	var ja3 string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(scrapfly.FPJA3URL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			node, err := dom.GetDocument().Do(ctx)
			if err != nil {
				return err
			}
			res, err := dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
			if err != nil {
				return err
			}
			doc, err := goquery.NewDocumentFromReader(bytes.NewBuffer([]byte(res)))
			if err != nil {
				return err
			}
			body := doc.Find("body").Text()
			if body == "" {
				return errors.New("couldn't obtain fp ja3")
			}
			var fpJA3 scrapfly.FPJA3
			if err := json.Unmarshal([]byte(body), &fpJA3); err != nil {
				return err
			}
			ja3 = fpJA3.JA3
			if ja3 == "" {
				return errors.New("empty ja3")
			}
			log.Println("ja3:", ja3)
			return nil
		}),
	); err != nil {
		return fmt.Errorf("could not obtain ja3: %w", err)
	}
	if ja3 == "" {
		return errors.New("empty ja3")
	}

	// obtain user agent
	var userAgent, acceptLanguage string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(scrapfly.InfoHTTPURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			node, err := dom.GetDocument().Do(ctx)
			if err != nil {
				return err
			}
			res, err := dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
			if err != nil {
				return err
			}
			doc, err := goquery.NewDocumentFromReader(bytes.NewBuffer([]byte(res)))
			if err != nil {
				return err
			}
			body := doc.Find("body").Text()
			if body == "" {
				return errors.New("couldn't obtain info http")
			}
			var infoHTTP scrapfly.InfoHTTP
			if err := json.Unmarshal([]byte(body), &infoHTTP); err != nil {
				return err
			}
			userAgent = infoHTTP.Headers.UserAgent.Payload
			if userAgent == "" {
				return errors.New("empty user agent")
			}
			log.Println("user-agent:", userAgent)
			v, ok := infoHTTP.Headers.ParsedHeaders["Accept-Language"]
			if !ok || len(v) == 0 {
				return errors.New("empty accept language")
			}
			acceptLanguage = v[0]
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

	userAgent = strings.ReplaceAll(userAgent, "\n", "")
	userAgent = strings.ReplaceAll(userAgent, "like  Gecko", "like Gecko")
	cookie = strings.ReplaceAll(cookie, "\n", "")
	cookie = strings.ReplaceAll(cookie, ";  ", "; ")

	// save session
	session := &bulkai.Session{
		JA3:             ja3,
		UserAgent:       userAgent,
		Token:           token,
		SuperProperties: xSuperProperties,
		Locale:          xDiscordLocale,
		Cookie:          cookie,
		Language:        acceptLanguage,
	}
	data, err := yaml.Marshal(session)
	if err != nil {
		return fmt.Errorf("couldn't marshal session: %w", err)
	}
	log.Println("Session successfully obtained: ")
	fmt.Println(string(data))

	// If the file already exists, copy it to a backup file
	if _, err := os.Stat(output); err == nil {
		ext := filepath.Ext(output)
		// Remove the extension from the output
		output = strings.TrimSuffix(output, ext)
		// Add a timestamp to the backup file
		output = fmt.Sprintf("%s_%s%s", output, time.Now().Format("20060102150405"), ext)
		if err := os.Rename(output, output); err != nil {
			return fmt.Errorf("couldn't backup session: %w", err)
		}
		log.Println("Previous session backed up to", output)
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
