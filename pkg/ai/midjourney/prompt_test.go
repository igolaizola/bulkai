package midjourney

import "testing"

func TestValidatePrompt(t *testing.T) {
	validator := NewValidator()
	tests := []struct {
		name    string
		prompt  string
		wantErr bool
	}{
		{
			name:    "empty prompt",
			prompt:  "",
			wantErr: true,
		},
		{
			name:    "valid prompt",
			prompt:  "this is a valid prompt",
			wantErr: false,
		},
		{
			name:    "banned word",
			prompt:  "the word sex is banned",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validator.ValidatePrompt(tt.prompt); (err != nil) != tt.wantErr {
				t.Errorf("ValidatePrompt() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
