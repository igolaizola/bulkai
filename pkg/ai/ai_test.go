package ai

import "testing"

func TestFileName(t *testing.T) {
	tests := []struct {
		image *Image
		want  string
	}{
		{
			image: &Image{
				URL:         "https://barfoo.com/test.png",
				Prompt:      "test bar foo",
				PromptIndex: 123,
				ImageIndex:  3,
			},
			want: "test_bar_foo_00123_03.png",
		},
		{
			image: &Image{
				URL:         "https://barfoo.com/test.png",
				Prompt:      " https://barfoo.com/test.png test bar foo",
				PromptIndex: 7889,
				ImageIndex:  7,
			},
			want: "test_bar_foo_07889_07.png",
		},
	}

	for _, tt := range tests {
		got := tt.image.FileName()
		if got != tt.want {
			t.Errorf("FileName() = %v, want %v", got, tt.want)
		}
	}
}
