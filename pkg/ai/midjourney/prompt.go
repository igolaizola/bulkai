package midjourney

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed banned.json
var bannedData []byte

type Validator interface {
	ValidatePrompt(prompt string) error
}

func NewValidator() Validator {
	// Parse bannedData into a slice of strings
	list := []string{}
	if err := json.Unmarshal(bannedData, &list); err != nil {
		// This should never happen
		panic(err)
	}
	lookup := make(map[string]struct{})
	for _, word := range list {
		lookup[word] = struct{}{}
	}
	return &validator{banned: lookup}
}

type validator struct {
	banned map[string]struct{}
}

func (v *validator) ValidatePrompt(prompt string) error {
	// Check if prompt is empty
	if prompt == "" {
		return fmt.Errorf("midjourney: prompt is empty")
	}

	// Convert prompt to lowercase
	prompt = strings.ToLower(prompt)

	// Split prompt into words using any whitespace or punctuation as a delimiter
	words := strings.FieldsFunc(prompt, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '!' || r == '?' ||
			r == ';' || r == ':' || r == '-' || r == '_' || r == '(' ||
			r == ')' || r == '[' || r == ']' || r == '{' || r == '}' ||
			r == '"' || r == '\'' || r == '/' || r == '\\' || r == '|' ||
			r == '@' || r == '#' || r == '$' || r == '%' || r == '^' ||
			r == '&' || r == '*' || r == '+' || r == '=' || r == '<' ||
			r == '>' || r == '~' || r == '`' || r == '\t' || r == '\n'
	})

	// Check if any words are banned
	for _, word := range words {
		if _, ok := v.banned[word]; ok {
			return fmt.Errorf("midjourney: word %q is banned", word)
		}
	}
	return nil
}
