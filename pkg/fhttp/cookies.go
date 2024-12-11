package fhttp

import (
	"fmt"
	"net/url"
	"strings"

	http "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/fhttp/cookiejar"
)

func SetCookies(c Client, rawURL string, rawCookies string) error {
	if c.GetCookieJar() == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return fmt.Errorf("http: failed to create cookie jar: %w", err)
		}
		c.SetCookieJar(jar)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("http: invalid url: %v", rawURL)
	}
	var cookies []*http.Cookie
	for _, cookie := range strings.Split(rawCookies, ";") {
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			continue
		}
		parts := strings.SplitN(cookie, "=", 2)
		switch strings.ToLower(parts[0]) {
		case "path", "domain", "httponly", "secure", "samesite":
			continue
		}
		if len(parts) != 2 {
			return fmt.Errorf("http: invalid cookie: %v", cookie)
		}
		cookies = append(cookies, &http.Cookie{Name: parts[0], Value: parts[1]})
	}
	c.GetCookieJar().SetCookies(u, cookies)
	return nil
}

func GetCookies(c Client, rawURL string) (string, error) {
	if c.GetCookieJar() == nil {
		return "", fmt.Errorf("http: missing cookie jar")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("http: invalid url: %v", rawURL)
	}
	var cookies []string
	for _, cookie := range c.GetCookieJar().Cookies(u) {
		cookies = append(cookies, cookie.String())
	}
	return strings.Join(cookies, "; "), nil
}
