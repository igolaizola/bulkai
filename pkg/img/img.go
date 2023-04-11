package img

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Split splits an image into 4 images
func Split4(input string, outputs []string) error {
	// Get encoder and decoder
	ext := filepath.Ext(input)
	var encode func(io.Writer, image.Image) error
	var decode func(io.Reader) (image.Image, error)
	switch ext {
	case ".png":
		encode = png.Encode
		decode = png.Decode
	case ".jpg", ".jpeg":
		encode = func(w io.Writer, m image.Image) error {
			return jpeg.Encode(w, m, nil)
		}
		decode = jpeg.Decode
	case ".webp":
		encode = png.Encode
		decode = webp.Decode
	default:
		return fmt.Errorf("img: unsupported extension: %s", ext)
	}

	// Obtain reader from file path
	reader, err := os.Open(input)
	if err != nil {
		return fmt.Errorf("img: couldn't open file %s: %w", input, err)
	}

	// Load image
	img, err := decode(reader)
	if err != nil {
		return fmt.Errorf("img: couldn't decode image: %w", err)
	}

	// Split image
	bounds := img.Bounds()
	width := bounds.Max.X / 2
	height := bounds.Max.Y / 2
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			incX := 0
			if x == 1 && bounds.Max.X%2 == 1 {
				incX = 1
			}
			incY := 0
			if y == 1 && bounds.Max.Y%2 == 1 {
				incY = 1
			}

			// Crop image
			cropped := image.NewRGBA(image.Rect(0, 0, width, height))
			draw.Draw(cropped, cropped.Bounds(), img, image.Point{x*width + incX, y*height + incY}, draw.Src)

			// Encode image
			var buf bytes.Buffer
			if err := encode(&buf, cropped); err != nil {
				return fmt.Errorf("img: couldn't encode image: %w", err)
			}

			// Save file
			output := outputs[y*2+x]
			if err := os.WriteFile(output, buf.Bytes(), 0644); err != nil {
				return fmt.Errorf("img: couldn't write file %s: %w", output, err)
			}
		}
	}
	return nil
}

func Resize(div int, path, output string) error {
	// Get encoder
	outExt := filepath.Ext(output)
	var encode func(io.Writer, image.Image) error
	switch outExt {
	case ".png":
		encode = png.Encode
	case ".jpg", ".jpeg":
		encode = func(w io.Writer, m image.Image) error {
			return jpeg.Encode(w, m, nil)
		}
	case ".webp":
		encode = png.Encode
	default:
		return fmt.Errorf("img: unsupported extension: %s", outExt)
	}

	// Get decoder
	inExt := filepath.Ext(path)
	var decode func(io.Reader) (image.Image, error)
	switch inExt {
	case ".png":
		decode = png.Decode
	case ".jpg", ".jpeg":
		decode = jpeg.Decode
	case ".webp":
		decode = webp.Decode
	default:
		return fmt.Errorf("img: unsupported extension: %s", inExt)
	}

	// Obtain reader from file path
	reader, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("img: couldn't open file %s: %w", path, err)
	}

	// Load image
	img, err := decode(reader)
	if err != nil {
		return fmt.Errorf("img: couldn't decode image: %w", err)
	}

	// Resize image to half size
	bounds := img.Bounds()
	width := bounds.Max.X / div
	height := bounds.Max.Y / div
	resized := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)

	// Encode image
	var buf bytes.Buffer
	if err := encode(&buf, resized); err != nil {
		return fmt.Errorf("img: couldn't encode image: %w", err)
	}

	// Save file
	if err := os.WriteFile(output, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("img: couldn't write file %s: %w", output, err)
	}
	return nil
}
