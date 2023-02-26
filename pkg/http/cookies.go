package http

import (
	"fmt"
	"net/url"
	"strings"

	http "github.com/Danny-Dasilva/fhttp"
	"github.com/Danny-Dasilva/fhttp/cookiejar"
)

func SetCookies(c *http.Client, rawURL string, rawCookies string) error {
	if c.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return fmt.Errorf("http: failed to create cookie jar: %w", err)
		}
		c.Jar = jar
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
		if len(parts) != 2 {
			return fmt.Errorf("http: invalid cookie: %v", cookie)
		}
		cookies = append(cookies, &http.Cookie{Name: parts[0], Value: parts[1]})
	}
	c.Jar.SetCookies(u, cookies)
	return nil
}

func GetCookies(c *http.Client, rawURL string) (string, error) {
	if c.Jar == nil {
		return "", fmt.Errorf("http: missing cookie jar")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("http: invalid url: %v", rawURL)
	}
	var cookies []string
	for _, cookie := range c.Jar.Cookies(u) {
		cookies = append(cookies, cookie.String())
	}
	return strings.Join(cookies, "; "), nil
}
