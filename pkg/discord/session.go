package discord

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

func newSession(dialer func(ctx context.Context, network, addr string) (net.Conn, error), token, userAgent string) (*discordgo.Session, error) {
	s, err := discordgo.New(token)
	if err != nil {
		return nil, err
	}
	s.Dialer = &websocket.Dialer{
		Proxy: http.ProxyFromEnvironment,
	}
	if dialer != nil {
		s.Dialer.NetDialContext = dialer
	}
	s.Client = &http.Client{
		Transport: &roundTripper{},
	}
	s.UserAgent = userAgent
	return s, nil
}

type roundTripper struct {
}

func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var data []byte
	switch req.URL.String() {
	case "https://discord.com/api/v9/gateway":
		data = []byte(`{"url": "wss://gateway.discord.gg"}`)
	default:
		data = []byte{}
	}

	body := io.NopCloser(bytes.NewBuffer(data))
	return &http.Response{
		StatusCode: 200,
		Body:       body,
	}, nil
}
