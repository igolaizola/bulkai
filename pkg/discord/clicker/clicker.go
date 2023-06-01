package clicker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/igolaizola/bulkai/pkg/discord"
)

type Client struct {
	proxy   string
	token   string
	guild   string
	channel string
	profile bool
	debug   bool

	ctx     context.Context
	stop    func()
	session string
	fetch   string
}

type Config struct {
	Proxy   string
	Token   string
	Guild   string
	Channel string
	Profile bool
	Debug   bool
}

func New(cfg *Config) *Client {
	return &Client{
		proxy:   cfg.Proxy,
		token:   cfg.Token,
		channel: cfg.Channel,
		guild:   cfg.Guild,
		profile: cfg.Profile,
		debug:   cfg.Debug,
	}
}

func (c *Client) debugLog(v ...interface{}) {
	if !c.debug {
		return
	}
	log.Println(v...)
}

func (c *Client) Stop() {
	c.stop()
}

func (c *Client) Start(ctx context.Context) error {
	log.Println("Starting browser")
	defer log.Println("Browser stopped")

	opts := append(
		chromedp.DefaultExecAllocatorOptions[3:],
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
	)

	if c.proxy != "" {
		opts = append(opts,
			chromedp.ProxyServer(c.proxy),
		)
	}

	if c.profile {
		opts = append(opts,
			// if user-data-dir is set, chrome won't load the default profile,
			// even if it's set to the directory where the default profile is stored.
			// set it to empty to prevent chromedp from setting it to a temp directory.
			chromedp.UserDataDir(""),
			chromedp.Flag("disable-extensions", false),
		)
	}

	ctx, cancelAllocator := chromedp.NewExecAllocator(ctx, opts...)

	// Create chrome instance
	ctx, cancelInstance := chromedp.NewContext(
		ctx,
		// chromedp.WithDebugf(log.Printf),
	)

	// Save context
	c.ctx = ctx
	c.stop = func() {
		cancelInstance()
		cancelAllocator()
	}

	// Disable webdriver
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(cxt context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument("Object.defineProperty(navigator, 'webdriver', { get: () => false, });").Do(cxt)
		if err != nil {
			return err
		}
		return nil
	})); err != nil {
		return fmt.Errorf("could not disable webdriver: %w", err)
	}

	var lck sync.Mutex

	// Capture and update session id using console logs
	sessionRE := regexp.MustCompile(`\[READY\] took \d+ms, as ([a-fA-F0-9]{32}).*`)
	chromedp.ListenTarget(
		ctx,
		func(ev interface{}) {
			e, ok := ev.(*runtime.EventConsoleAPICalled)
			if !ok {
				return
			}

			for _, arg := range e.Args {
				if arg.Type != "string" {
					continue
				}
				var value string
				if err := json.Unmarshal(arg.Value, &value); err != nil {
					continue
				}
				value = strings.Split(value, "\n")[0]
				match := sessionRE.FindStringSubmatch(value)
				if len(match) < 2 {
					continue
				}
				lck.Lock()
				c.session = match[1]
				c.debugLog("session_id:", c.session)
				lck.Unlock()
				return
			}
		},
	)

	// Obtain discord token and other client data
	var gotToken, cookie, xSuperProperties, xDiscordLocale, xDiscordTimezone string
	var language, userAgent, secChUa string

	listen, done := context.WithCancel(ctx)
	defer done()
	chromedp.ListenTarget(
		listen,
		func(ev interface{}) {
			e, ok := ev.(*network.EventRequestWillBeSentExtraInfo)
			if !ok {
				return
			}
			if !strings.HasPrefix(getHeader(e, "origin"), "https://discord.com") {
				return
			}
			if h := getHeader(e, "accept-language"); h != "" {
				lck.Lock()
				if language != h {
					language = h
					c.debugLog("language:", language)
				}
				lck.Unlock()
			}
			if h := getHeader(e, "user-agent"); h != "" {
				lck.Lock()
				if userAgent != h {
					userAgent = h
					c.debugLog("user-agent:", userAgent)
				}
				lck.Unlock()
			}
			if h := getHeader(e, "sec-ch-ua"); h != "" {
				lck.Lock()
				if secChUa != h {
					secChUa = h
					c.debugLog("sec-ch-ua:", secChUa)
				}
				lck.Unlock()
			}
			if h := getHeader(e, "x-discord-locale"); h != "" {
				lck.Lock()
				if xDiscordLocale != h {
					xDiscordLocale = h
					c.debugLog("locale:", xDiscordLocale)
				}
				lck.Unlock()
			}
			if h := getHeader(e, "x-super-properties"); h != "" {
				lck.Lock()
				if xSuperProperties != h {
					xSuperProperties = h
					c.debugLog("super-properties:", xSuperProperties)
				}
				lck.Unlock()
			}
			if h := getHeader(e, "x-discord-timezone"); h != "" {
				lck.Lock()
				if xDiscordTimezone != h {
					xDiscordTimezone = h
					c.debugLog("discord-timezone:", xDiscordTimezone)
				}
				lck.Unlock()
			}
			if h := getHeader(e, "cookie"); h != "" {
				lck.Lock()
				if cookie != h {
					cookie = h
					c.debugLog("cookie:", "...redacted...")
				}
				lck.Unlock()
			}
			if h := getHeader(e, "authorization"); h != "" {
				lck.Lock()
				if gotToken != h {
					gotToken = h
					c.debugLog("token:", "...redacted...")
				}
				lck.Unlock()
			}

			lck.Lock()
			defer lck.Unlock()
			if gotToken != "" && cookie != "" && xSuperProperties != "" && xDiscordLocale != "" &&
				xDiscordTimezone != "" && language != "" && userAgent != "" {
				done()
			}
		},
	)

	// Go to discord login page
	if err := chromedp.Run(ctx,
		// Load google first to have a sane referer
		chromedp.Navigate("https://www.google.com/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Navigate("https://discord.com/login"),
	); err != nil {
		return fmt.Errorf("could not obtain discord data: %w", err)
	}

	// Load token
	loginScript := "function login(token) { " +
		"setInterval(() => { document.body.appendChild(document.createElement `iframe`).contentWindow.localStorage.token = `\"${token}\"`}, 50);" +
		"setTimeout(() => {location.reload();}, 2500);} login('%s');"

	loginScript = fmt.Sprintf(loginScript, c.token)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(loginScript, nil),
	); err != nil {
		return fmt.Errorf("could not set token: %w", err)
	}

	// Wait for data to be obtained
	select {
	case <-listen.Done():
	case <-ctx.Done():
		return fmt.Errorf("clicker: could not obtain token: %w", ctx.Err())
	}
	if gotToken != c.token {
		return fmt.Errorf("clicker: invalid token: %d", len(gotToken))
	}

	// Go to channel
	guild := "@me"
	if c.guild != "" {
		guild = c.guild
	}
	channelURL := fmt.Sprintf("https://discord.com/channels/%s/%s", guild, c.channel)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(channelURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("clicker: couldn't obtain discord data: %w", err)
	}

	// Set fetch script
	if secChUa == "" {
		secChUa = "\"Google Chrome\";v=\"113\", \"Chromium\";v=\"113\", \"Not-A.Brand\";v=\"24\""
	}
	c.fetch = `fetch("https://discord.com/api/v9/interactions", {
		"headers": {
		  "accept": "*/*",
		  "accept-language": "` + language + `",	
		  "authorization": "` + c.token + `",
		  "content-type": "application/json",
		  "sec-ch-ua": "` + secChUa + `",
		  "sec-ch-ua-mobile": "?0",
		  "sec-ch-ua-platform": "\"Windows\"",
		  "sec-fetch-dest": "empty",
		  "sec-fetch-mode": "cors",
		  "sec-fetch-site": "same-origin",
		  "x-debug-options": "bugReporterEnabled",
		  "x-discord-locale": "` + xDiscordLocale + `",
		  "x-discord-timezone": "` + xDiscordTimezone + `",
		  "x-super-properties": "` + xSuperProperties + `",
		},
		"referrer": "` + channelURL + `",
		"referrerPolicy": "strict-origin-when-cross-origin",
		"body": "%s",
		"method": "POST",
		"mode": "cors",
		"credentials": "include"
	  }).then(response => {return response.status})`

	return nil
}

func (c *Client) Interaction(interaction *discord.InteractionComponent) error {
	if c.session == "" {
		return fmt.Errorf("clicker: session not set")
	}

	// Create script to send interaction
	interaction.SessionID = c.session
	b, err := json.Marshal(interaction)
	if err != nil {
		return fmt.Errorf("clicker: couldn't marshal interaction: %w", err)
	}
	body := string(b)
	body = strings.ReplaceAll(body, `"`, `\"`)
	fetch := fmt.Sprintf(c.fetch, body)

	// Send interaction
	var statusCode int
	if err := chromedp.Run(c.ctx,
		chromedp.Evaluate(fetch, &statusCode, func(ep *runtime.EvaluateParams) *runtime.EvaluateParams {
			return ep.WithAwaitPromise(true)
		}),
	); err != nil {
		return fmt.Errorf("clicker: couldn't fetch: %w", err)
	}

	// Check status code
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("clicker: invalid status code: %d", statusCode)
	}
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
