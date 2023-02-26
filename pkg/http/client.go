package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	httpgo "net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	http "github.com/Danny-Dasilva/fhttp"
	http2 "github.com/Danny-Dasilva/fhttp/http2"
	utls "github.com/Danny-Dasilva/utls"
	"golang.org/x/net/proxy"
)

func NewDialer(ja3, userAgent, lang, proxyURL string) (func(ctx context.Context, network, addr string) (net.Conn, error), error) {
	var dialer proxy.ContextDialer
	dialer = &ctxDialer{Dialer: proxy.Direct}
	rt := newRoundTripper(ja3, userAgent, lang, dialer).(*roundTripper)
	return rt.dialer.DialContext, nil
}

func NewGoClient(ja3, userAgent, lang, proxyURL string) (*httpgo.Client, error) {
	var dialer proxy.ContextDialer
	dialer = &ctxDialer{Dialer: proxy.Direct}
	if proxyURL != "" {
		var err error
		dialer, err = newConnectDialer(proxyURL)
		if err != nil {
			return nil, err
		}
	}
	rt := newRoundTripper(ja3, userAgent, lang, dialer).(*roundTripper)
	tr := httpgo.DefaultTransport.(*httpgo.Transport).Clone()
	tr.DialContext = rt.dialer.DialContext
	return &httpgo.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}, nil
}

func NewClient(ja3, userAgent, lang, proxyURL string) (*http.Client, error) {
	var dialer proxy.ContextDialer
	dialer = &ctxDialer{Dialer: proxy.Direct}
	if proxyURL != "" {
		var err error
		dialer, err = newConnectDialer(proxyURL)
		if err != nil {
			return nil, err
		}
	}
	return &http.Client{
		Transport: newRoundTripper(ja3, userAgent, lang, dialer),
		Timeout:   30 * time.Second,
	}, nil
}

type ctxDialer struct {
	proxy.Dialer
}

func (d *ctxDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return proxy.Direct.Dial(network, addr)
}

var errProtocolNegotiated = errors.New("protocol negotiated")

type roundTripper struct {
	sync.Mutex

	JA3       string
	UserAgent string
	Language  string

	cachedConnections map[string]net.Conn
	cachedTransports  map[string]http.RoundTripper

	dialer proxy.ContextDialer
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	chromeVersion := "109"
	if idx := strings.Index(rt.UserAgent, "Chrome/"); idx != -1 {
		candidate := strings.Split(rt.UserAgent[idx+7:], ".")[0]
		if _, err := strconv.Atoi(chromeVersion); err == nil {
			chromeVersion = candidate
		}
	}
	req.Header.Set("accept-language", rt.Language)
	req.Header.Set("sec-ch-ua", fmt.Sprintf(`"Not A;Brand";v="99", "Google Chrome";v="%s", "Chromium";v="%s"`, chromeVersion, chromeVersion))
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("user-agent", rt.UserAgent)

	addr := rt.getDialTLSAddr(req)
	if _, ok := rt.cachedTransports[addr]; !ok {
		if err := rt.getTransport(req, addr); err != nil {
			return nil, err
		}
	}
	return rt.cachedTransports[addr].RoundTrip(req)
}

func (rt *roundTripper) getTransport(req *http.Request, addr string) error {
	switch strings.ToLower(req.URL.Scheme) {
	case "http":
		rt.cachedTransports[addr] = &http.Transport{DialContext: rt.dialer.DialContext, DisableKeepAlives: true}
		return nil
	case "https":
	default:
		return fmt.Errorf("invalid URL scheme: [%v]", req.URL.Scheme)
	}

	_, err := rt.dialTLS(context.Background(), "tcp", addr)
	switch err {
	case errProtocolNegotiated:
	case nil:
		// Should never happen.
		panic("dialTLS returned no error when determining cachedTransports")
	default:
		return err
	}

	return nil
}

func (rt *roundTripper) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	rt.Lock()
	defer rt.Unlock()

	// If we have the connection from when we determined the httpS
	// cachedTransports to use, return that.
	if conn := rt.cachedConnections[addr]; conn != nil {
		delete(rt.cachedConnections, addr)
		return conn, nil
	}
	rawConn, err := rt.dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	var host string
	if host, _, err = net.SplitHostPort(addr); err != nil {
		host = addr
	}
	//////////////////

	spec, err := StringToSpec(rt.JA3, rt.UserAgent)
	if err != nil {
		return nil, err
	}

	conn := utls.UClient(rawConn, &utls.Config{ServerName: host, InsecureSkipVerify: true}, // MinVersion:         tls.VersionTLS10,
		// MaxVersion:         tls.VersionTLS13,

		utls.HelloCustom)

	if err := conn.ApplyPreset(spec); err != nil {
		return nil, err
	}

	if err = conn.Handshake(); err != nil {
		_ = conn.Close()

		if err.Error() == "tls: CurvePreferences includes unsupported curve" {
			//fix this
			return nil, fmt.Errorf("conn.Handshake() error for tls 1.3 (please retry request): %+v", err)
		}
		return nil, fmt.Errorf("uTlsConn.Handshake() error: %+v", err)
	}

	//////////
	if rt.cachedTransports[addr] != nil {
		return conn, nil
	}

	// No http.Transport constructed yet, create one based on the results
	// of ALPN.
	negotiatedProtocol := conn.ConnectionState().NegotiatedProtocol
	switch {
	case negotiatedProtocol == http2.NextProtoTLS && addr != "gateway.discord.gg:443":
		parsedUserAgent := parseUserAgent(rt.UserAgent)
		t2 := http2.Transport{DialTLS: rt.dialTLShttp2,
			PushHandler: &http2.DefaultPushHandler{},
			Navigator:   parsedUserAgent,
		}
		rt.cachedTransports[addr] = &t2
	default:
		// Assume the remote peer is speaking http 1.x + TLS.
		rt.cachedTransports[addr] = &http.Transport{DialTLSContext: rt.dialTLS}

	}

	// Stash the connection just established for use servicing the
	// actual request (should be near-immediate).
	rt.cachedConnections[addr] = conn

	return nil, errProtocolNegotiated
}

func (rt *roundTripper) dialTLShttp2(network, addr string, _ *utls.Config) (net.Conn, error) {
	return rt.dialTLS(context.Background(), network, addr)
}

func (rt *roundTripper) getDialTLSAddr(req *http.Request) string {
	host, port, err := net.SplitHostPort(req.URL.Host)
	if err == nil {
		return net.JoinHostPort(host, port)
	}
	return net.JoinHostPort(req.URL.Host, "443") // we can assume port is 443 at this point
}

func newRoundTripper(ja3, userAgent, lang string, dialer ...proxy.ContextDialer) http.RoundTripper {
	if len(dialer) > 0 {
		return &roundTripper{
			dialer: dialer[0],

			JA3:               ja3,
			UserAgent:         userAgent,
			Language:          lang,
			cachedTransports:  make(map[string]http.RoundTripper),
			cachedConnections: make(map[string]net.Conn),
		}
	}

	return &roundTripper{
		dialer: &ctxDialer{Dialer: proxy.FromEnvironment()},

		JA3:               ja3,
		UserAgent:         userAgent,
		Language:          lang,
		cachedTransports:  make(map[string]http.RoundTripper),
		cachedConnections: make(map[string]net.Conn),
	}
}

const (
	chrome  = "chrome"  //chrome User agent enum
	firefox = "firefox" //firefox User agent enum
)

func parseUserAgent(userAgent string) string {
	switch {
	case strings.Contains(strings.ToLower(userAgent), "chrome"):
		return chrome
	case strings.Contains(strings.ToLower(userAgent), "firefox"):
		return firefox
	default:
		return chrome
	}
}

type contextDialer struct {
	proxy.Dialer
}

func (d *contextDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.Dial(network, addr)
}
