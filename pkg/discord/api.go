package discord

import "github.com/bwmarrin/discordgo"

type InteractionCommand struct {
	Type          int                    `json:"type"`
	ApplicationID string                 `json:"application_id"`
	GuildID       string                 `json:"guild_id,omitempty"`
	ChannelID     string                 `json:"channel_id"`
	SessionID     string                 `json:"session_id"`
	Data          InteractionCommandData `json:"data"`
	Nonce         string                 `json:"nonce,omitempty"`
}

type InteractionCommandData struct {
	Version            string                                               `json:"version"`
	ID                 string                                               `json:"id"`
	Name               string                                               `json:"name"`
	Type               int                                                  `json:"type"`
	Options            []*discordgo.ApplicationCommandInteractionDataOption `json:"options"`
	ApplicationCommand *discordgo.ApplicationCommand                        `json:"application_command"`
	Attachments        []*discordgo.MessageAttachment                       `json:"attachments"`
}

type InteractionComponent struct {
	Type          int                      `json:"type"`
	ApplicationID string                   `json:"application_id"`
	ChannelID     string                   `json:"channel_id"`
	GuildID       string                   `json:"guild_id,omitempty"`
	SessionID     string                   `json:"session_id"`
	Data          InteractionComponentData `json:"data"`
	Nonce         string                   `json:"nonce,omitempty"`
	MessageID     string                   `json:"message_id"`
}
type InteractionComponentData struct {
	ComponentType int    `json:"component_type"`
	CustomID      string `json:"custom_id"`
}

const (
	InteractionCreateEvent  = "INTERACTION_CREATE"
	InteractionSuccessEvent = "INTERACTION_SUCCESS"
	MessageCreateEvent      = "MESSAGE_CREATE"
	MessageUpdateEvent      = "MESSAGE_UPDATE"
)

type Message struct {
	//discordgo.Message
	// The ID of the message.
	ID string `json:"id"`

	// The ID of the channel in which the message was sent.
	ChannelID string `json:"channel_id"`

	// The ID of the guild in which the message was sent.
	GuildID string `json:"guild_id,omitempty"`

	// The content of the message.
	Content string `json:"content"`

	// Nonce used for validating a message was sent.
	Nonce string `json:"nonce"`

	// A list of attachments present in the message.
	Attachments []*discordgo.MessageAttachment `json:"attachments"`

	// A list of components attached to the message.
	Components []*Component `json:"components"`

	// A list of embeds present in the message.
	Embeds []*discordgo.MessageEmbed `json:"embeds"`

	// Interaction data
	Interaction *Interaction `json:"interaction"`
}

type Interaction struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"`
}

type Component struct {
	Type       int          `json:"type"`
	Style      int          `json:"style,omitempty"`
	Label      string       `json:"label,omitempty"`
	CustomID   string       `json:"custom_id,omitempty"`
	Components []*Component `json:"components,omitempty"`
}

type User struct {
	discordgo.User
	Profile     UserProfile     `json:"user_profile"`
	Application UserApplication `json:"application"`
}

type UserProfile struct {
	Bio string `json:"bio"`
}

type UserApplication struct {
	ID string `json:"id"`
	// Flags    any    `json:"flags"`
	Verified bool `json:"verified"`
}

type ApplicationCommandSearch struct {
	Applications []*Application                  `json:"applications"`
	Commands     []*discordgo.ApplicationCommand `json:"application_commands"`
}

// Application stores values for a Discord Application
//
// This has been modified from the original discordgo.Application in order to
// fix the `Flags` field which used to be an int and is now a string. We removed
// it from the struct to avoid breaking changes.
type Application struct {
	ID                  string          `json:"id,omitempty"`
	Name                string          `json:"name"`
	Icon                string          `json:"icon,omitempty"`
	Description         string          `json:"description,omitempty"`
	RPCOrigins          []string        `json:"rpc_origins,omitempty"`
	BotPublic           bool            `json:"bot_public,omitempty"`
	BotRequireCodeGrant bool            `json:"bot_require_code_grant,omitempty"`
	TermsOfServiceURL   string          `json:"terms_of_service_url"`
	PrivacyProxyURL     string          `json:"privacy_policy_url"`
	Owner               *User           `json:"owner"`
	Summary             string          `json:"summary"`
	VerifyKey           string          `json:"verify_key"`
	Team                *discordgo.Team `json:"team"`
	GuildID             string          `json:"guild_id"`
	PrimarySKUID        string          `json:"primary_sku_id"`
	Slug                string          `json:"slug"`
	CoverImage          string          `json:"cover_image"`
	// Flags               any             `json:"flags,omitempty"`
}
