package ai

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Preview struct {
	URL       string
	Prompt    string
	MessageID string
	ImageIDs  []string
}

type Client interface {
	Start(ctx context.Context) error
	Imagine(ctx context.Context, prompt string) (*Preview, error)
	Upscale(ctx context.Context, preview *Preview, index int) (string, error)
	Variation(ctx context.Context, preview *Preview, index int) (*Preview, error)
	Concurrency() int
}

type Image struct {
	URL    string
	Prompt string

	Preview     bool
	PromptIndex int
	ImageIndex  int
	IsLast      bool
}

type entry struct {
	prompt string
	index  int
}

func Bulk(ctx context.Context, cli Client, prompts []string, skip []int, variationEnabled, upscaleEnabled bool, concurrency int, wait time.Duration) <-chan (*Image) {
	skipLookup := make(map[int]struct{})
	for _, s := range skip {
		skipLookup[s] = struct{}{}
	}
	if concurrency == 0 || concurrency > cli.Concurrency() {
		concurrency = cli.Concurrency()
	}
	chunks := make([][]entry, concurrency)
	for i, p := range prompts {
		if _, ok := skipLookup[i]; ok {
			continue
		}
		chunks[i%concurrency] = append(chunks[i%concurrency], entry{
			prompt: p,
			index:  i,
		})
	}

	out := make(chan (*Image))
	wg := sync.WaitGroup{}
	for _, entries := range chunks {
		entries := entries
		if len(prompts) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k, e := range entries {
				currWait := wait
				if k == 0 {
					currWait = 1 * time.Second
				}
				if currWait > 0 {
					// Wait before sending next request
					// use a random value between 85% and 115% of the wait time
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Duration(float64(currWait) * (0.85 + 0.3*rand.Float64()))):
					}
				}

				// Launch preview
				preview, err := imagine(cli, ctx, e.prompt)
				if err != nil {
					log.Println(fmt.Errorf("❌ couldn't imagine %s %w", e.prompt, err))
					continue
				}

				if !upscaleEnabled {
					out <- &Image{
						URL:         preview.URL,
						Prompt:      e.prompt,
						Preview:     true,
						PromptIndex: e.index,
						ImageIndex:  0,
						IsLast:      true,
					}
				}

				// Upscale or get variation for each image
				for i := range preview.ImageIDs {
					if upscaleEnabled {
						u, err := upscale(cli, ctx, preview, i)
						if err != nil {
							log.Println(fmt.Errorf("❌ couldn't upscale %s %d: %w", e.prompt, i, err))
							continue
						}
						out <- &Image{
							URL:         u,
							Prompt:      e.prompt,
							PromptIndex: e.index,
							ImageIndex:  i,
							IsLast:      i == len(preview.ImageIDs)-1 && !variationEnabled,
						}
					}

					if !variationEnabled {
						continue
					}

					// Get variation
					variationPreview, err := variation(cli, ctx, preview, i)
					if err != nil {
						log.Println(fmt.Errorf("❌ couldn't get variation: %w", err))
						continue
					}

					if !upscaleEnabled {
						out <- &Image{
							URL:         variationPreview.URL,
							Prompt:      e.prompt,
							Preview:     true,
							PromptIndex: e.index,
							ImageIndex:  4 + i*4,
							IsLast:      i == len(preview.ImageIDs)-1,
						}
						continue
					}

					// Upscale each variation image
					for j := range variationPreview.ImageIDs {
						var u string
						u, err := upscale(cli, ctx, variationPreview, j)
						if err != nil {
							log.Println(fmt.Errorf("❌ couldn't upscale %s %d: %w", e.prompt, j, err))
							continue
						}
						out <- &Image{
							URL:         u,
							Prompt:      e.prompt,
							PromptIndex: e.index,
							ImageIndex:  4 + i*4 + j,
							IsLast:      i == len(preview.ImageIDs)-1 && j == len(variationPreview.ImageIDs)-1,
						}
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func (i *Image) FileName() string {
	prompt := fixString(i.Prompt)
	ext := filepath.Ext(i.URL)
	return fmt.Sprintf("%s_%05d_%02d%s", prompt, i.PromptIndex, i.ImageIndex, ext)
}

func (i *Image) FileNames() []string {
	var names []string
	prompt := fixString(i.Prompt)
	ext := filepath.Ext(i.URL)
	for j := 0; j < 4; j++ {
		names = append(names, fmt.Sprintf("%s_%05d_%02d%s", prompt, i.PromptIndex, i.ImageIndex+j, ext))
	}
	return names
}

var nonAlphanumericRegex = regexp.MustCompile(`[^\p{L}\p{N} _]+`)

func fixString(str string) string {
	split := strings.Split(str, " ")
	var filtered []string
	for _, s := range split {
		if u, err := url.Parse(s); err == nil && u.Scheme != "" {
			continue
		}
		if len(s) == 0 {
			continue
		}
		filtered = append(filtered, s)
	}
	str = strings.Join(filtered, "_")

	str = nonAlphanumericRegex.ReplaceAllString(str, "")
	str = strings.ReplaceAll(str, " ", "_")

	// Limit to 50 characters to avoid issues with file names
	if len(str) > 50 {
		str = str[:50]
	}
	return str
}

func imagine(cli Client, ctx context.Context, prompt string) (*Preview, error) {
	var preview *Preview
	if err := retry(ctx, func(ctx context.Context) error {
		p, err := cli.Imagine(ctx, prompt)
		if err != nil {
			return err
		}
		preview = p
		return nil
	}); err != nil {
		return nil, err
	}
	return preview, nil
}

func upscale(cli Client, ctx context.Context, preview *Preview, index int) (string, error) {
	var upscaleURL string
	if err := retry(ctx, func(ctx context.Context) error {
		u, err := cli.Upscale(ctx, preview, index)
		if err != nil {
			return err
		}
		upscaleURL = u
		return nil
	}); err != nil {
		return "", err
	}
	return upscaleURL, nil
}

func variation(cli Client, ctx context.Context, preview *Preview, index int) (*Preview, error) {
	var variationPreview *Preview
	if err := retry(ctx, func(ctx context.Context) error {
		v, err := cli.Variation(ctx, preview, index)
		if err != nil {
			return err
		}
		variationPreview = v
		return nil
	}); err != nil {
		return nil, err
	}
	return variationPreview, nil
}

const maxAttempts = 5

func retry(ctx context.Context, fn func(context.Context) error) error {
	attempts := 0
	for {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		attempts++
		if attempts >= maxAttempts {
			return err
		}
		log.Println("retrying...", err)
	}
}
