package midjourney

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/bwmarrin/snowflake"
	"github.com/igolaizola/askimg"
	"github.com/igolaizola/bulkai/pkg/ai"
	"github.com/igolaizola/bulkai/pkg/discord"
)

const (
	botID               = "936929561302675456"
	upscaleTerm         = "Upscaled by"
	imageNumberTerm     = "Image #"
	variationTerm       = "Variations by"
	variationSubtleTerm = "Variations (Subtle) by"
	variationStrongTerm = "Variations (Strong) by"
	upscaleID           = "MJ::JOB::upsample::"
	variationID         = "MJ::JOB::variation::"
)

type Client struct {
	c              *discord.Client
	debug          bool
	node           *snowflake.Node
	callback       map[search][]func(*discord.Message) bool
	cache          map[string]struct{}
	lck            sync.Mutex
	channelID      string
	guildID        string
	cmd            *discordgo.ApplicationCommand
	validator      Validator
	replicateToken string
	dumps          []string
	dumpLock       sync.Mutex
	timeout        time.Duration
	queuedTimeout  time.Duration
}

type Config struct {
	Debug          bool
	ChannelID      string
	ReplicateToken string
	Timeout        time.Duration
	QueuedTimeout  time.Duration
}

func New(client *discord.Client, cfg *Config) (ai.Client, error) {
	node, err := snowflake.NewNode(0)
	if err != nil {
		return nil, fmt.Errorf("midjourney: couldn't create snowflake node")
	}

	channelID := cfg.ChannelID
	if channelID == "" {
		channelID = client.DM(botID)
		if channelID == "" {
			return nil, fmt.Errorf("midjourney: couldn't find dm channel for bot")
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

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	queuedTimeout := cfg.QueuedTimeout
	if queuedTimeout == 0 {
		queuedTimeout = 20 * time.Minute
	}

	c := &Client{
		c:              client,
		debug:          cfg.Debug,
		node:           node,
		callback:       make(map[search][]func(*discord.Message) bool),
		cache:          make(map[string]struct{}),
		channelID:      channelID,
		guildID:        guildID,
		validator:      NewValidator(),
		replicateToken: cfg.ReplicateToken,
		timeout:        timeout,
		queuedTimeout:  queuedTimeout,
	}

	c.c.OnEvent(func(e *discordgo.Event) {
		switch e.Type {
		case discord.MessageCreateEvent, discord.MessageUpdateEvent:
			var msg discord.Message
			if err := json.Unmarshal(e.RawData, &msg); err != nil {
				log.Println("midjourney: couldn't unmarshal message: %w", err)
			}
			// Ignore messages from other channels
			if msg.ChannelID != c.channelID {
				return
			}
			c.debugLog(e.Type, e.RawData)

			// Check action
			ok, err := c.checkAction(&msg)
			if err != nil {
				js, _ := json.Marshal(msg)
				log.Println(string(js))
				log.Println(err)
				c.debugLog("ERR", err)
				c.saveDump()
				panic("❌ midjourney: action required")
			}
			if ok {
				return
			}

			var key search
			var cacheID string

			switch {
			case len(msg.Attachments) > 0:
				// Ignore messages that don't have components
				if len(msg.Components) == 0 {
					return
				}

				// Attachment based message
				cacheID = cleanURL(msg.Attachments[0].URL)

				// Ignore message already in the cache
				c.lck.Lock()
				_, ok := c.cache[cacheID]
				c.lck.Unlock()
				if ok {
					return
				}

				// Parse prompt
				prompt, rest, ok := parseContent(msg.Content)
				if !ok {
					return
				}

				// Remove links from the prompt
				prompt = replaceLinks(prompt)

				switch {
				case strings.Contains(rest, upscaleTerm) || strings.Contains(rest, imageNumberTerm):
					key = upscaleSearch(prompt)
				case strings.Contains(rest, variationTerm) || strings.Contains(rest, variationSubtleTerm) || strings.Contains(rest, variationStrongTerm):
					key = variationSearch(prompt)
				default:
					key = previewSearch(prompt)
				}
			case msg.Nonce != "":
				// Nonce based message
				cacheID = msg.Nonce

				// Ignore message already in the cache
				c.lck.Lock()
				_, ok := c.cache[cacheID]
				c.lck.Unlock()
				if ok {
					return
				}

				// Parse prompt
				if _, _, ok := parseContent(msg.Content); !ok {
					// Check if there is an error message
					if err := parseError(&msg); err == nil {
						// Check if there is interaction data
						if msg.Interaction == nil || msg.Interaction.ID == "" {
							return
						}
					}
				}

				key = nonceSearch(msg.Nonce)
			case msg.Interaction != nil && msg.Interaction.ID != "" && msg.Interaction.Name == "imagine":
				// Interaction based message
				cacheID = msg.Interaction.ID

				// Ignore message already in the cache
				c.lck.Lock()
				_, ok := c.cache[cacheID]
				c.lck.Unlock()
				if ok {
					return
				}

				key = interactionSearch(msg.Interaction.ID)
			}

			// Search for matching callbacks
			for {
				c.lck.Lock()
				callbacks := c.callback[key]
				if len(callbacks) == 0 {
					c.lck.Unlock()
					return
				}
				// Get and remove the first callback
				f := callbacks[0]
				c.callback[key] = callbacks[1:]
				c.lck.Unlock()

				// Launch the callback
				if ok := f(&msg); !ok {
					// If returns false, it means it was expired
					continue
				}

				// Add the message to the cache
				c.lck.Lock()
				c.cache[cacheID] = struct{}{}
				c.lck.Unlock()
				return
			}
		}
	})
	return c, nil
}

func (c *Client) Concurrency() int {
	return 12
}

func (c *Client) debugLog(t string, v interface{}) {
	if v == nil {
		if c.debug {
			log.Println(t)
		}
		return
	}
	js, _ := json.Marshal(v)

	// Save dump
	c.dumpLock.Lock()
	c.dumps = append(c.dumps, string(js))
	if len(c.dumps) > 100 {
		c.dumps = c.dumps[len(c.dumps)-100:]
	}
	c.dumpLock.Unlock()

	if c.debug {
		log.Println(t, string(js))
	}
}

func (c *Client) saveDump() {
	c.dumpLock.Lock()
	defer c.dumpLock.Unlock()
	var output string
	for _, dump := range c.dumps {
		output += dump + "\n"
	}
	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("logs", 0755); err != nil {
		log.Println("midjourney: couldn't create logs directory:", err)
		return
	}
	// Save dump using the current time
	now := time.Now()
	filename := fmt.Sprintf("logs/dump_%s.txt", now.Format("20060102_150405"))
	if err := os.WriteFile(filename, []byte(output), 0644); err != nil {
		log.Println("midjourney: couldn't save dump:", err)
		return
	}
}

func parseContent(content string) (string, string, bool) {
	// Search prompt
	split := strings.SplitN(content, "**", 3)
	if len(split) != 3 {
		return "", "", false
	}
	prompt := split[1]
	rest := split[2]
	return prompt, rest, true
}

func parseEmbedFooter(prompt string, msg *discord.Message) (string, error) {
	if len(msg.Embeds) == 0 {
		return "", errors.New("midjourney: message has no embed")
	}
	embed := msg.Embeds[0]
	if embed.Footer == nil {
		return "", errors.New("midjourney: embed has no footer")
	}
	footer := embed.Footer.Text
	if !strings.HasPrefix(footer, "/imagine ") {
		return "", fmt.Errorf("midjourney: footer doesn't start with /imagine: %s", footer)
	}
	footer = strings.TrimPrefix(footer, "/imagine ")
	if !strings.HasPrefix(footer, prompt) {
		return "", fmt.Errorf("midjourney: footer doesn't start with prompt: %s", footer)
	}
	suffixes := strings.TrimPrefix(footer, prompt)
	// Remove extra space that sometimes appears when suffixes are configured
	if strings.HasPrefix(suffixes, "  ") {
		suffixes = strings.TrimPrefix(suffixes, " ")
	}
	return fmt.Sprintf("%s%s", prompt, suffixes), nil
}

// Errors parsed from messages
var ErrInvalidParameter = errors.New("invalid parameter")
var ErrInvalidLink = errors.New("invalid link")
var ErrBannedPrompt = errors.New("banned prompt")
var ErrActionNeeded = errors.New("action needed to continue")
var ErrJobQueued = errors.New("job queued")
var ErrQueueFull = errors.New("queue full")
var ErrPendingMod = errors.New("pending mod message")
var ErrActionRequired = errors.New("action required to continue")
var ErrCompleteTask = errors.New("please complete the task")
var ErrInvalidRequest = errors.New("invalid request")
var ErrJobActionRestricted = errors.New("job action restricted")
var ErrEmptyPrompt = errors.New("empty prompt")

// Other errors
var ErrMessageNotFound = ai.NewError(errors.New("message not found"), false)

func parseError(msg *discord.Message) error {
	if len(msg.Embeds) == 0 {
		return nil
	}
	embed := msg.Embeds[0]
	title := strings.ToLower(embed.Title)
	desc := strings.ToLower(embed.Description)

	switch title {
	case "invalid parameter":
		err := fmt.Errorf("midjourney: %w: %s", ErrInvalidParameter, desc)
		return ai.NewError(err, false)
	case "invalid link":
		err := fmt.Errorf("midjourney: %w: %s", ErrInvalidLink, desc)
		return ai.NewError(err, false)
	case "banned prompt", "banned prompt detected", "banned image prompt":
		err := fmt.Errorf("midjourney: %w: %s", ErrBannedPrompt, desc)
		return ai.NewError(err, false)
	case "action needed to continue":
		err := fmt.Errorf("midjourney: %w: %s", ErrActionNeeded, desc)
		return ai.NewError(err, false)
	case "job queued":
		err := fmt.Errorf("midjourney: %w: %s", ErrJobQueued, desc)
		return ai.NewError(err, false)
	case "queue full":
		err := fmt.Errorf("midjourney: %w: %s", ErrQueueFull, desc)
		return ai.NewError(err, true)
	case "pending mod message":
		err := fmt.Errorf("midjourney: %w: %s", ErrPendingMod, desc)
		return ai.NewFatal(err)
	case "action required to continue":
		err := fmt.Errorf("midjourney: %w: %s", ErrActionRequired, desc)
		return ai.NewFatal(err)
	case "please complete the task":
		err := fmt.Errorf("midjourney: %w: %s", ErrCompleteTask, desc)
		return ai.NewFatal(err)
	case "invalid request":
		err := fmt.Errorf("midjourney: %w: %s", ErrInvalidRequest, desc)
		return ai.NewFatal(err)
	case "job action restricted":
		err := fmt.Errorf("midjourney: %w: %s", ErrJobActionRestricted, desc)
		return ai.NewFatal(err)
	case "empty prompt":
		err := fmt.Errorf("midjourney: %w: %s", ErrEmptyPrompt, desc)
		return ai.NewFatal(err)
	default:
		err := fmt.Errorf("midjourney: %s: %s", title, desc)
		return ai.NewError(err, true)
	}
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

type nonceSearch string

func (s nonceSearch) value() string {
	return string(s)
}

type interactionSearch string

func (s interactionSearch) value() string {
	return string(s)
}

func (c *Client) receiveMessage(parent context.Context, key search, timeout time.Duration, fn func() error) (*discord.Message, error) {
	msgChan := make(chan *discord.Message)
	defer close(msgChan)
	c.lck.Lock()
	c.callback[key] = append(c.callback[key], func(m *discord.Message) bool {
		// Check if channel is still open
		select {
		case <-msgChan:
			return false
		default:
		}
		// Send the message
		msgChan <- m
		return true
	})
	c.lck.Unlock()

	// Execute the function if any
	if fn != nil {
		if err := fn(); err != nil {
			return nil, err
		}
	}

	// Add a timeout to receive the message
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-msgChan:
		return msg, nil
	}
}

func (c *Client) Start(ctx context.Context) error {
	var appSearch discord.ApplicationCommandSearch

	switch c.guildID {
	case "":
		// Search for command in a DM channel
		u := fmt.Sprintf("users/%s/profile?with_mutual_guilds=false&with_mutual_friends_count=false", botID)
		var user discord.User
		resp, err := c.c.Do(ctx, "GET", u, nil)
		if err != nil {
			return fmt.Errorf("midjourney: couldn't get user %s: %w", botID, err)
		}
		if err := json.Unmarshal(resp, &user); err != nil {
			return fmt.Errorf("midjourney: couldn't unmarshal user %s: %w", string(resp), err)
		}
		applicationID := user.Application.ID
		if applicationID == "" {
			return fmt.Errorf("midjourney: couldn't find application id for user %s", botID)
		}

		u = fmt.Sprintf("channels/%s/application-command-index", c.channelID)
		resp, err = c.c.Do(ctx, "GET", u, nil)
		if err != nil {
			return fmt.Errorf("midjourney: couldn't get channel application commands: %w", err)
		}
		if err := json.Unmarshal(resp, &appSearch); err != nil {
			return fmt.Errorf("midjourney: couldn't unmarshal application command search %s: %w", string(resp), err)
		}
	default:
		// Search for command in a guild channel
		u := fmt.Sprintf("guilds/%s/application-command-index", c.guildID)
		resp, err := c.c.Do(ctx, "GET", u, nil)
		if err != nil {
			return fmt.Errorf("midjourney: couldn't get guild application commands: %w", err)
		}
		if err := json.Unmarshal(resp, &appSearch); err != nil {
			return fmt.Errorf("midjourney: couldn't unmarshal application command search %s: %w", string(resp), err)
		}
	}

	var cmd *discordgo.ApplicationCommand
	for _, c := range appSearch.Commands {
		if c.ApplicationID != botID {
			continue
		}
		if c.Name != "imagine" {
			continue
		}
		cmd = c
		break
	}
	if cmd == nil {
		return fmt.Errorf("midjourney: couldn't find imagine command")
	}
	c.cmd = cmd
	return nil
}

func (c *Client) Imagine(ctx context.Context, prompt string) (*ai.Preview, error) {
	// Validate prompt
	if err := c.validator.ValidatePrompt(prompt); err != nil {
		return nil, ai.NewError(err, false)
	}

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

	timeout := c.timeout

	response, err := c.receiveMessage(ctx, nonceSearch(nonce), timeout, func() error {
		// Launch interaction inside the receive message process because the
		// response may be received before it finishes, due to rate limit
		// locking.
		if _, err := c.c.Do(ctx, "POST", "interactions", imagine); err != nil {
			return fmt.Errorf("midjourney: couldn't send imagine interaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("midjourney: couldn't receive imagine response (%s): %w", nonce, err)
	}

	// Parse prompt
	responsePrompt, _, ok := parseContent(response.Content)
	if !ok {
		// Check if the response contains an error message
		err := parseError(response)
		switch {
		case errors.Is(err, ErrJobQueued):
			// The job is queued, so it will be processed.
			// We will take the response prompt from the message embed footer.
			responsePrompt, err = parseEmbedFooter(prompt, response)
			if err != nil {
				return nil, err
			}
			timeout = c.queuedTimeout
		case err != nil:
			return nil, err
		case response.Interaction != nil && response.Interaction.ID != "":
			// Search the response prompt by the interaction id
			response, err := c.receiveMessage(ctx, interactionSearch(response.Interaction.ID), timeout, nil)
			if err != nil {
				return nil, fmt.Errorf("midjourney: couldn't receive imagine response (%s): %w", nonce, err)
			}
			responsePrompt, _, ok = parseContent(response.Content)
			if !ok {
				return nil, fmt.Errorf("midjourney: couldn't parse prompt from update message: %s", response.Content)
			}
		default:
			return nil, fmt.Errorf("midjourney: couldn't parse prompt from imagine response: %s", response.Content)
		}
	}

	// The response prompt links may differ from the final links, so we need to
	// replace them with placeholders.
	responsePrompt = replaceLinks(responsePrompt)

	preview, err := c.receiveMessage(ctx, previewSearch(responsePrompt), timeout, nil)
	if err != nil {
		return nil, fmt.Errorf("midjourney: couldn't receive links message for (%s): %w", responsePrompt, err)
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
			if !strings.HasPrefix(comp.CustomID, upscaleID) {
				continue
			}
			imageIDs = append(imageIDs, strings.TrimPrefix(comp.CustomID, upscaleID))
		}
	}
	if len(imageIDs) == 0 {
		return nil, fmt.Errorf("midjourney: message has no image ids")
	}
	return &ai.Preview{
		URL:            preview.Attachments[0].URL,
		Prompt:         prompt,
		ResponsePrompt: responsePrompt,
		MessageID:      preview.ID,
		ImageIDs:       imageIDs,
	}, nil
}

func (c *Client) Upscale(ctx context.Context, preview *ai.Preview, index int) (string, error) {
	if index < 0 || index >= len(preview.ImageIDs) {
		return "", fmt.Errorf("midjourney: invalid index %d", index)
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

	msg, err := c.receiveMessage(ctx, upscaleSearch(preview.ResponsePrompt), c.timeout, func() error {
		// Launch interaction inside the receive message process because the
		// response may be received before it finishes, due to rate limit
		// locking.
		if _, err := c.c.Do(ctx, "POST", "interactions", upscale); err != nil {
			// Check if the message was deleted
			if errors.Is(err, discord.ErrMessageNotFound) {
				return ErrMessageNotFound
			}
			return fmt.Errorf("midjourney: couldn't send upscale interaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("midjourney: couldn't receive links message: %w", err)
	}
	return msg.Attachments[0].URL, nil
}

func (c *Client) Variation(ctx context.Context, preview *ai.Preview, index int) (*ai.Preview, error) {
	if index < 0 || index >= len(preview.ImageIDs) {
		return nil, fmt.Errorf("midjourney: invalid index %d", index)
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

	msg, err := c.receiveMessage(ctx, variationSearch(preview.ResponsePrompt), c.timeout, func() error {
		// Launch interaction inside the receive message process because the
		// response may be received before it finishes, due to rate limit
		// locking.
		if _, err := c.c.Do(ctx, "POST", "interactions", variation); err != nil {
			// Check if the message was deleted
			if errors.Is(err, discord.ErrMessageNotFound) {
				return ErrMessageNotFound
			}
			return fmt.Errorf("midjourney: couldn't send variation interaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("midjourney: couldn't receive links message: %w", err)
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
		return nil, fmt.Errorf("midjourney: message has no image ids")
	}
	return &ai.Preview{
		URL:            msg.Attachments[0].URL,
		Prompt:         preview.Prompt,
		ResponsePrompt: preview.ResponsePrompt,
		MessageID:      msg.ID,
		ImageIDs:       imageIDs,
	}, nil
}

func (c *Client) checkAction(msg *discord.Message) (bool, error) {
	if len(msg.Components) == 0 {
		return false, nil
	}
	components := msg.Components[0].Components
	if len(components) == 0 {
		return false, nil
	}
	if !strings.HasPrefix(components[0].CustomID, "MJ::Captcha") {
		return false, nil
	}
	if c.replicateToken == "" {
		return false, fmt.Errorf("midjourney: action required")
	}
	if len(msg.Embeds) == 0 {
		return false, fmt.Errorf("midjourney: missing embed in action")
	}
	if msg.Embeds[0].Image == nil {
		return false, fmt.Errorf("midjourney: missing image in embed")
	}
	image := msg.Embeds[0].Image.URL
	if image == "" {
		return false, fmt.Errorf("midjourney: missing image url in embed")
	}

	var options []string
	for _, comp := range components {
		options = append(options, strings.TrimSpace(strings.ToLower(comp.Label)))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	question := fmt.Sprintf("Choose one: %s", strings.Join(options, ", "))
	response, err := askimg.Ask(ctx, &askimg.Config{
		Token:    c.replicateToken,
		Image:    image,
		Question: question,
		Timeout:  1 * time.Minute,
	})
	if err != nil {
		return false, fmt.Errorf("midjourney: couldn't ask image: %w", err)
	}
	c.debugLog("ASK", struct {
		Question string `json:"question"`
		Response string `json:"response"`
	}{Question: question, Response: response})

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "" {
		return false, fmt.Errorf("midjourney: no response from image")
	}

	match := -1
	for i, opt := range options {
		if strings.HasPrefix(opt, response) || strings.HasPrefix(response, opt) {
			match = i
			break
		}
	}
	if match < 0 {
		return false, fmt.Errorf("midjourney: match not found (%s) in (%s)", response, strings.Join(options, ", "))
	}

	// Launch click button
	click := &discord.InteractionComponent{
		Type:          3,
		Nonce:         c.node.Generate().String(),
		GuildID:       c.guildID,
		ChannelID:     c.channelID,
		MessageID:     msg.ID,
		ApplicationID: c.cmd.ApplicationID,
		SessionID:     c.c.Session(),
		Data: discord.InteractionComponentData{
			ComponentType: 2,
			CustomID:      components[match].CustomID,
		},
	}
	c.debugLog("CLICK", click)
	if _, err := c.c.Do(ctx, "POST", "interactions", click); err != nil {
		return false, fmt.Errorf("midjourney: couldn't send click interaction: %w", err)
	}
	log.Printf("✅ midjourney: action completed (%s) %d %s %s\n", strings.Join(options, ","), match, response, image)
	return true, nil
}

var linkRegex = regexp.MustCompile(`https?://[^\s]+`)
var linkWrappedRegex = regexp.MustCompile(`<https?://[^\s]+>`)

func replaceLinks(s string) string {
	s = linkWrappedRegex.ReplaceAllString(s, "<LINK>")
	return linkRegex.ReplaceAllString(s, "<LINK>")
}

func cleanURL(u string) string {
	return strings.Split(u, "?")[0]
}
