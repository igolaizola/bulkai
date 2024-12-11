package fhttp

import (
	"fmt"
	"time"

	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

type Client interface {
	tlsclient.HttpClient
}

type client struct {
	tlsclient.HttpClient
}

func NewClient(timeout time.Duration, useJar bool, proxy string) (Client, error) {
	jar := tlsclient.NewCookieJar()
	secs := int(timeout.Seconds())
	if secs <= 0 {
		secs = 30
	}
	options := []tlsclient.HttpClientOption{
		tlsclient.WithTimeoutSeconds(secs),
		tlsclient.WithClientProfile(profiles.DefaultClientProfile),
	}
	if useJar {
		options = append(options, tlsclient.WithCookieJar(jar))
	}
	if proxy != "" {
		options = append(options, tlsclient.WithProxyUrl(proxy))
	}
	c, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("fhttp: couldn't create http client: %w", err)
	}
	return &client{HttpClient: c}, nil
}
