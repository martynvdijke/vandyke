package webhook

import (
	"encoding/json"
	"fmt"
	"strings"

	"vandyke.thor.edu/vandyke/internal/store"
)

// discordBody is the JSON body Discord webhooks accept.
type discordBody struct {
	Content         string         `json:"content"`
	Embeds          []discordEmbed `json:"embeds"`
	AllowedMentions discordMention `json:"allowed_mentions"`
}

type discordEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Timestamp   string `json:"timestamp"`
}

// discordMention carries an empty parse list so a word or meaning containing
// @everyone or a role mention cannot notify a channel.
type discordMention struct {
	Parse []string `json:"parse"`
}

type slackBody struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type string    `json:"type"`
	Text slackText `json:"text"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Render renders a generic payload using an endpoint's preset. The result is
// the exact bytes that are signed and sent.
func Render(preset string, payload Payload) ([]byte, error) {
	switch preset {
	case "", store.WebhookPresetGeneric:
		return json.Marshal(payload)
	case store.WebhookPresetDiscord:
		return json.Marshal(discordPayload(payload))
	case store.WebhookPresetSlack:
		return json.Marshal(slackPayload(payload))
	default:
		return nil, fmt.Errorf("unknown webhook preset %q", preset)
	}
}

// displayWord and displayMeaning supply the non-empty fallbacks Discord and
// Slack require for a photo-only entry.
func displayWord(payload Payload) string {
	if strings.TrimSpace(payload.Word) != "" {
		return payload.Word
	}
	return fmt.Sprintf("Entry #%d", payload.ID)
}

func displayMeaning(payload Payload) string {
	if strings.TrimSpace(payload.Meaning) != "" {
		return payload.Meaning
	}
	return payload.URL
}

func discordPayload(payload Payload) discordBody {
	return discordBody{
		Content: "vanDyke " + payload.Event,
		Embeds: []discordEmbed{{
			Title:       displayWord(payload),
			Description: displayMeaning(payload),
			URL:         payload.URL,
			Timestamp:   payload.CreatedAt,
		}},
		AllowedMentions: discordMention{Parse: []string{}},
	}
}

func slackPayload(payload Payload) slackBody {
	word := displayWord(payload)
	meaning := displayMeaning(payload)
	text := fmt.Sprintf("vanDyke %s: %s — %s", payload.Event, word, meaning)
	block := fmt.Sprintf("*%s*\n%s\n<%s|View entry>",
		escapeSlack(word), escapeSlack(meaning), escapeSlack(payload.URL))
	return slackBody{
		Text: text,
		Blocks: []slackBlock{{
			Type: "section",
			Text: slackText{Type: "mrkdwn", Text: block},
		}},
	}
}

// escapeSlack neutralises the three characters Slack treats as markup so an
// entry cannot inject <!channel> or another special token.
func escapeSlack(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(value)
}
