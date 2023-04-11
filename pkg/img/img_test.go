package img

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestSplit4(t *testing.T) {
	tests := []string{
		"testdata/test.webp",
		"testdata/test.png",
		"testdata/test.jpg",
	}
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			ext := filepath.Ext(tt)
			outputs := []string{
				fmt.Sprintf("testdata/output/test1%s", ext),
				fmt.Sprintf("testdata/output/test2%s", ext),
				fmt.Sprintf("testdata/output/test3%s", ext),
				fmt.Sprintf("testdata/output/test4%s", ext),
			}
			if err := Split4(tt, outputs); err != nil {
				t.Errorf("Split4() error = %v", err)
			}
		})
	}
}

func TestResize(t *testing.T) {
	tests := []string{
		"testdata/test.webp",
		"testdata/test.png",
		"testdata/test.jpg",
	}
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			ext := filepath.Ext(tt)
			output := fmt.Sprintf("testdata/output/test_thumbnail%s.jpg", ext)
			if err := Resize(4, tt, output); err != nil {
				t.Errorf("Resize() error = %v", err)
			}
		})
	}

}
