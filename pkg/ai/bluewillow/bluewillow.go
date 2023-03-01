package bluewillow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/bwmarrin/snowflake"
	"github.com/igolaizola/bulkai/pkg/ai"
	"github.com/igolaizola/bulkai/pkg/discord"
)

const (
	botID         = "1049413890276077690"
	upscaleTerm   = "Upscaling by"
	variationTerm = "Variations by"
	upscaleID     = "UPSCALE:"
	variationID   = "VARIATION:"
)

type Client struct {
	c         *discord.Client
	debug     bool
	node      *snowflake.Node
	callback  map[search][]func(*discord.Message)
	cache     map[string]struct{}
	lck       sync.Mutex
	channelID string
	guildID   string
	cmd       *discordgo.ApplicationCommand
}

func New(client *discord.Client, channelID string, debug bool) (ai.Client, error) {
	node, err := snowflake.NewNode(0)
	if err != nil {
		return nil, fmt.Errorf("bluewillow: couldn't create snowflake node")
	}

	if channelID == "" {
		channelID = client.DM(botID)
		if channelID == "" {
			return nil, fmt.Errorf("bluewillow: couldn't find dm channel for bot")
		}
	}

	guildID := ""
	if split := strings.SplitN(channelID, "/", 2); len(split) == 2 {
		guildID = split[0]
		channelID = split[1]
	}
	if guildID != "" {
		client.Referer = fmt.Sprintf("channels/%s/%s", guildID, channelID)
	} else {
		client.Referer = fmt.Sprintf("channels/@me/%s", channelID)
	}

	c := &Client{
		c:         client,
		debug:     debug,
		node:      node,
		callback:  make(map[search][]func(*discord.Message)),
		cache:     make(map[string]struct{}),
		channelID: channelID,
		guildID:   guildID,
	}

	c.c.OnEvent(func(e *discordgo.Event) {
		switch e.Type {
		case discord.MessageCreateEvent, discord.MessageUpdateEvent:
			var msg discord.Message
			if err := json.Unmarshal(e.RawData, &msg); err != nil {
				log.Println("bluewillow: couldn't unmarshal message: %w", err)
			}
			c.debugLog(e.Type, e.RawData)

			// Ignore messages that don't have an attachment
			if len(msg.Attachments) == 0 {
				return
			}
			// Ignore message already in the cache
			c.lck.Lock()
			_, ok := c.cache[msg.Attachments[0].URL]
			c.lck.Unlock()
			if ok {
				return
			}

			// Search prompt
			split := strings.SplitN(msg.Content, "**", 3)
			if len(split) != 3 {
				return
			}
			prompt := split[1]
			rest := split[2]
			var key search
			switch {
			case strings.Contains(rest, upscaleTerm):
				// Ignore messages that don't have an attachment
				if len(msg.Attachments) == 0 {
					return
				}
				key = upscaleSearch(prompt)
			case strings.Contains(rest, variationTerm):
				// Ignore messages that don't have preview data
				if len(msg.Attachments) == 0 {
					return
				}
				if len(msg.Components) == 0 {
					return
				}
				key = variationSearch(prompt)
			default:
				// Ignore messages that don't have preview data
				if len(msg.Attachments) == 0 {
					return
				}
				if len(msg.Components) == 0 {
					return
				}
				key = previewSearch(prompt)
			}
			// Search for matching callbacks
			c.lck.Lock()
			callbacks := c.callback[key]
			if len(callbacks) == 0 {
				c.lck.Unlock()
				return
			}
			// Get and remove the first callback
			f := callbacks[0]
			c.callback[key] = callbacks[1:]

			// Add the message to the cache
			c.cache[msg.Attachments[0].URL] = struct{}{}
			c.lck.Unlock()

			// Launch the callback
			f(&msg)
		}
	})
	return c, nil
}

func (c *Client) Concurrency() int {
	return 5
}

func (c *Client) debugLog(t string, v interface{}) {
	if !c.debug {
		return
	}
	if v == nil {
		log.Println(t)
		return
	}
	js, _ := json.MarshalIndent(v, "", "  ")
	log.Println(t, string(js))
}

type search interface {
	value() string
}

type previewSearch string

func (s previewSearch) value() string {
	return string(s)
}

type upscaleSearch string

func (s upscaleSearch) value() string {
	return string(s)
}

type variationSearch string

func (s variationSearch) value() string {
	return string(s)
}

func (c *Client) receiveMessage(parent context.Context, key search) (*discord.Message, error) {
	msgChan := make(chan *discord.Message)
	defer close(msgChan)
	c.lck.Lock()
	c.callback[key] = append(c.callback[key], func(m *discord.Message) {
		msgChan <- m
	})
	c.lck.Unlock()

	// Add a timeout to receive the message
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-msgChan:
		return msg, nil
	}
}

func (c *Client) Start(ctx context.Context) error {
	u := fmt.Sprintf("users/%s/profile?with_mutual_guilds=false&with_mutual_friends_count=false", botID)
	var user discord.User
	resp, err := c.c.Do(ctx, "GET", u, nil)
	if err != nil {
		return fmt.Errorf("bluewillow: couldn't get user %s: %w", botID, err)
	}
	if err := json.Unmarshal(resp, &user); err != nil {
		return fmt.Errorf("bluewillow: couldn't unmarshal user %s: %w", string(resp), err)
	}
	applicationID := user.Application.ID
	if applicationID == "" {
		return fmt.Errorf("bluewillow: couldn't find application id for user %s", botID)
	}

	u = fmt.Sprintf("channels/%s/application-commands/search?type=1&application_id=%s&include_applications=true", c.channelID, applicationID)
	var appSearch discord.ApplicationCommandSearch
	resp, err = c.c.Do(ctx, "GET", u, nil)
	if err != nil {
		return fmt.Errorf("bluewillow: couldn't get application command search: %w", err)
	}
	if err := json.Unmarshal(resp, &appSearch); err != nil {
		return fmt.Errorf("bluewillow: couldn't unmarshal application command search %s: %w", string(resp), err)
	}

	var cmd *discordgo.ApplicationCommand
	for _, c := range appSearch.Commands {
		if c.Name != "imagine" {
			continue
		}
		cmd = c
		break
	}
	if cmd == nil {
		return fmt.Errorf("bluewillow: couldn't find imagine command")
	}
	c.cmd = cmd
	return nil
}

func (c *Client) Imagine(ctx context.Context, prompt string) (*ai.Preview, error) {
	nonce := c.node.Generate().String()
	imagine := &discord.InteractionCommand{
		Type:          2,
		ApplicationID: c.cmd.ApplicationID,
		ChannelID:     c.channelID,
		GuildID:       c.guildID,
		SessionID:     c.c.Session(),
		Data: discord.InteractionCommandData{
			Version: c.cmd.Version,
			ID:      c.cmd.ID,
			Name:    c.cmd.Name,
			Type:    1,
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{
					Type:  discordgo.ApplicationCommandOptionString,
					Name:  "prompt",
					Value: prompt,
				},
			},
			ApplicationCommand: c.cmd,
		},
		Nonce: nonce,
	}
	c.debugLog("IMAGINE", imagine)
	if _, err := c.c.Do(ctx, "POST", "interactions", imagine); err != nil {
		return nil, fmt.Errorf("bluewillow: couldn't send imagine interaction: %w", err)
	}

	preview, err := c.receiveMessage(ctx, previewSearch(prompt))
	if err != nil {
		return nil, fmt.Errorf("bluewillow: couldn't receive links message: %w", err)
	}

	var imageIDs []string
	for _, comps := range preview.Components {
		if len(comps.Components) < 4 {
			continue
		}
		if !strings.HasPrefix(comps.Components[0].CustomID, upscaleID) {
			continue
		}
		for _, comp := range comps.Components {
			if comp.CustomID == "" {
				continue
			}
			imageIDs = append(imageIDs, strings.TrimPrefix(comp.CustomID, upscaleID))
		}
	}
	if len(imageIDs) == 0 {
		return nil, fmt.Errorf("bluewillow: message has no image ids")
	}
	return &ai.Preview{
		URL:       preview.Attachments[0].URL,
		Prompt:    prompt,
		MessageID: preview.ID,
		ImageIDs:  imageIDs,
	}, nil
}

func (c *Client) Upscale(ctx context.Context, preview *ai.Preview, index int) (string, error) {
	if index < 0 || index >= len(preview.ImageIDs) {
		return "", fmt.Errorf("bluewillow: invalid index %d", index)
	}
	customID := fmt.Sprintf("%s%s", upscaleID, preview.ImageIDs[index])
	nonce := c.node.Generate().String()
	upscale := &discord.InteractionComponent{
		Type:          3,
		ApplicationID: c.cmd.ApplicationID,
		ChannelID:     c.channelID,
		GuildID:       c.guildID,
		SessionID:     c.c.Session(),
		Data: discord.InteractionComponentData{
			ComponentType: 2,
			CustomID:      customID,
		},
		Nonce:     nonce,
		MessageID: preview.MessageID,
	}
	c.debugLog("UPSCALE", upscale)
	if _, err := c.c.Do(ctx, "POST", "interactions", upscale); err != nil {
		return "", fmt.Errorf("bluewillow: couldn't send upscale interaction: %w", err)
	}

	msg, err := c.receiveMessage(ctx, upscaleSearch(preview.Prompt))
	if err != nil {
		return "", fmt.Errorf("bluewillow: couldn't receive links message: %w", err)
	}
	return msg.Attachments[0].URL, nil
}

func (c *Client) Variation(ctx context.Context, preview *ai.Preview, index int) (*ai.Preview, error) {
	if index < 0 || index >= len(preview.ImageIDs) {
		return nil, fmt.Errorf("bluewillow: invalid index %d", index)
	}
	customID := fmt.Sprintf("%s%s", variationID, preview.ImageIDs[index])
	nonce := c.node.Generate().String()
	variation := &discord.InteractionComponent{
		Type:          3,
		ApplicationID: c.cmd.ApplicationID,
		ChannelID:     c.channelID,
		GuildID:       c.guildID,
		SessionID:     c.c.Session(),
		Data: discord.InteractionComponentData{
			ComponentType: 2,
			CustomID:      customID,
		},
		Nonce:     nonce,
		MessageID: preview.MessageID,
	}
	c.debugLog("VARIATION", variation)
	if _, err := c.c.Do(ctx, "POST", "interactions", variation); err != nil {
		return nil, fmt.Errorf("bluewillow: couldn't send variation interaction: %w", err)
	}

	msg, err := c.receiveMessage(ctx, variationSearch(preview.Prompt))
	if err != nil {
		return nil, fmt.Errorf("bluewillow: couldn't receive variant message: %w", err)
	}

	var imageIDs []string
	for _, comps := range msg.Components {
		if len(comps.Components) < 4 {
			continue
		}
		if !strings.HasPrefix(comps.Components[0].CustomID, upscaleID) {
			continue
		}
		for _, comp := range comps.Components {
			if !strings.HasPrefix(comp.CustomID, upscaleID) {
				continue
			}
			imageIDs = append(imageIDs, strings.TrimPrefix(comp.CustomID, upscaleID))
		}
	}
	if len(imageIDs) == 0 {
		return nil, fmt.Errorf("bluewillow: message has no image ids")
	}
	return &ai.Preview{
		URL:       msg.Attachments[0].URL,
		Prompt:    preview.Prompt,
		MessageID: msg.ID,
		ImageIDs:  imageIDs,
	}, nil
}
