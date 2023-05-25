package bulkai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/bulkai/pkg/ai"
	"github.com/igolaizola/bulkai/pkg/ai/bluewillow"
	"github.com/igolaizola/bulkai/pkg/ai/midjourney"
	"github.com/igolaizola/bulkai/pkg/discord"
	"github.com/igolaizola/bulkai/pkg/http"
	"github.com/igolaizola/bulkai/pkg/img"
	"gopkg.in/yaml.v2"
)

type Album struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Status     string    `json:"status"`
	Percentage float32   `json:"percentage"`
	Images     []*Image  `json:"images"`
	Prompts    []string  `json:"prompts"`
	Finished   []int     `json:"finished"`
}

type Image struct {
	URL    string `json:"url"`
	Prompt string `json:"prompt"`
	File   string `json:"file"`
}

type Config struct {
	Debug          bool          `yaml:"debug"`
	Bot            string        `yaml:"bot"`
	Proxy          string        `yaml:"proxy"`
	Output         string        `yaml:"output"`
	Album          string        `yaml:"album"`
	Prefix         string        `yaml:"prefix"`
	Suffix         string        `yaml:"suffix"`
	Prompts        []string      `yaml:"prompts"`
	Variation      bool          `yaml:"variation"`
	Upscale        bool          `yaml:"upscale"`
	Download       bool          `yaml:"download"`
	Thumbnail      bool          `yaml:"thumbnail"`
	Channel        string        `yaml:"channel"`
	Concurrency    int           `yaml:"concurrency"`
	Wait           time.Duration `yaml:"wait"`
	ReplicateToken string        `yaml:"replicate-token"`
	SessionFile    string        `yaml:"session"`
	Session        Session       `yaml:"-"`
}

type Session struct {
	JA3             string `yaml:"ja3"`
	UserAgent       string `yaml:"user-agent"`
	Language        string `yaml:"language"`
	Token           string `yaml:"token"`
	SuperProperties string `yaml:"super-properties"`
	Locale          string `yaml:"locale"`
	Cookie          string `yaml:"cookie"`
}

type Status struct {
	Percentage float32
	Estimated  time.Duration
}

type Option func(*option)

type option struct {
	onUpdate func(Status)
}

func WithOnUpdate(onUpdate func(Status)) Option {
	return func(o *option) {
		o.onUpdate = onUpdate
	}
}

// Generate launches multiple ai generations.
func Generate(ctx context.Context, cfg *Config, opts ...Option) error {
	if cfg.Session.Token == "" {
		return errors.New("missing token")
	}
	if cfg.Bot == "" {
		return errors.New("missing bot name")
	}
	if cfg.Output == "" {
		return errors.New("missing output directory")
	}
	if cfg.Session.JA3 == "" {
		return errors.New("missing ja3")
	}
	if cfg.Session.UserAgent == "" {
		return errors.New("missing user agent")
	}
	if cfg.Session.Cookie == "" {
		return errors.New("missing cookie")
	}
	if cfg.Session.Language == "" {
		return errors.New("missing language")
	}

	// Load options
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	// Check ai bot
	var newCli func(*discord.Client, string, bool) (ai.Client, error)
	var cli ai.Client
	switch strings.ToLower(cfg.Bot) {
	case "bluewillow":
		newCli = bluewillow.New
	case "midjourney":
		newCli = func(c *discord.Client, channelID string, debug bool) (ai.Client, error) {
			return midjourney.New(c, channelID, debug, cfg.ReplicateToken)
		}
	default:
		return fmt.Errorf("unsupported bot: %s", cfg.Bot)
	}

	// New album
	albumID := cfg.Album
	if albumID == "" {
		albumID = time.Now().UTC().Format("20060102_150405")
	}
	var album *Album
	albumDir := fmt.Sprintf("%s/%s", cfg.Output, albumID)
	imgDir := fmt.Sprintf("%s/images", albumDir)

	var prompts []string

	// Check if the album data file exists
	dataFile := fmt.Sprintf("%s/%s/data.json", cfg.Output, albumID)
	_, err := os.Stat(dataFile)
	switch {
	case os.IsNotExist(err):
		// Album doesn't exist, create it later
	case err != nil:
		return fmt.Errorf("couldn't stat album data file: %w", err)
	default:
		// Album already exists, resume it
		data, err := os.ReadFile(dataFile)
		if err != nil {
			return fmt.Errorf("couldn't read album data file: %w", err)
		}
		albumCandidate := &Album{}
		if err := json.Unmarshal(data, albumCandidate); err != nil {
			return fmt.Errorf("couldn't unmarshal album data file: %w", err)
		}
		if albumCandidate.Status == "finished" {
			log.Println("album already finished:", albumDir)
			return nil
		}
		album = albumCandidate
		prompts = album.Prompts
		log.Println("album resumed:", albumDir)
	}

	if len(prompts) == 0 {
		if len(cfg.Prompts) == 0 {
			return errors.New("missing prompt")
		}

		// Build prompts
		for _, prompt := range cfg.Prompts {
			// Check if prompt is a file
			if _, err := os.Stat(prompt); err != nil {
				prompts = append(prompts, prompt)
				continue
			}
			// Read lines from file
			file, err := os.Open(prompt)
			if err != nil {
				return fmt.Errorf("couldn't open prompt file: %w", err)
			}
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				prompt := strings.TrimSpace(scanner.Text())
				if prompt == "" {
					continue
				}
				prompts = append(prompts, prompt)
			}
			_ = file.Close()
			// Check for errors
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("couldn't read prompt file: %w", err)
			}
		}

		for i, prompt := range prompts {
			prompts[i] = fmt.Sprintf("%s%s%s", cfg.Prefix, prompt, cfg.Suffix)
		}
		sort.Strings(prompts)
	}

	// Check total images
	var lck sync.Mutex
	total := len(prompts) * 4
	if cfg.Variation {
		total = total + total*4
	}

	// Create http client
	httpClient, err := http.NewClient(cfg.Session.JA3, cfg.Session.UserAgent, cfg.Session.Language, cfg.Proxy)
	if err != nil {
		return fmt.Errorf("couldn't create http client: %w", err)
	}

	// Set proxy
	if cfg.Proxy != "" {
		p := strings.TrimPrefix(cfg.Proxy, "http://")
		p = strings.TrimPrefix(p, "https://")
		os.Setenv("HTTPS_PROXY", p)
		os.Setenv("HTTP_PROXY", p)
	}

	if err := http.SetCookies(httpClient, "https://discord.com", cfg.Session.Cookie); err != nil {
		return fmt.Errorf("couldn't set cookies: %w", err)
	}
	defer func() {
		cookie, err := http.GetCookies(httpClient, "https://discord.com")
		if err != nil {
			log.Printf("couldn't get cookies: %v\n", err)
		}
		cfg.Session.Cookie = strings.ReplaceAll(cookie, "\n", "")
		// TODO: save session to common method
		data, err := yaml.Marshal(cfg.Session)
		if err != nil {
			log.Println(fmt.Errorf("couldn't marshal session: %w", err))
		}
		if err := os.WriteFile(cfg.SessionFile, data, 0644); err != nil {
			log.Println(fmt.Errorf("couldn't write session: %w", err))
		}
	}()

	// Create discord client
	client, err := discord.New(ctx, &discord.Config{
		Token:           cfg.Session.Token,
		SuperProperties: cfg.Session.SuperProperties,
		Locale:          cfg.Session.Locale,
		UserAgent:       cfg.Session.UserAgent,
		HTTPClient:      httpClient,
		Debug:           cfg.Debug,
	})
	if err != nil {
		return fmt.Errorf("couldn't create discord client: %w", err)
	}

	// Start discord client
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("couldn't start discord client: %w", err)
	}

	// Start ai client
	cli, err = newCli(client, cfg.Channel, cfg.Debug)
	if err != nil {
		return fmt.Errorf("couldn't create %s client: %w", cfg.Bot, err)
	}
	if err := cli.Start(ctx); err != nil {
		return fmt.Errorf("couldn't start ai client: %w", err)
	}

	// Album doesn't exist, create it
	if album == nil {
		album = &Album{
			ID:        albumID,
			Status:    "created",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			Images:    []*Image{},
			Prompts:   prompts,
		}
		if err := os.MkdirAll(albumDir, 0755); err != nil {
			return fmt.Errorf("couldn't create album directory: %w", err)
		}
		if cfg.Download {
			if err := os.MkdirAll(imgDir, 0755); err != nil {
				return fmt.Errorf("couldn't create album images directory: %w", err)
			}
			if cfg.Thumbnail {
				if err := os.MkdirAll(fmt.Sprintf("%s/_thumbnails", imgDir), 0755); err != nil {
					return fmt.Errorf("couldn't create album images directory: %w", err)
				}
			}
		}
		if err := SaveAlbum(albumDir, album, cfg.Thumbnail); err != nil {
			return fmt.Errorf("couldn't save album: %w", err)
		}
		log.Println("album created:", albumDir)
	}

	// Launch ai bulk operation
	imageChan := ai.Bulk(ctx, cli, prompts, album.Finished, cfg.Variation, cfg.Upscale, cfg.Concurrency, cfg.Wait)
	var exit bool
	for !exit {
		var status string
		select {
		case <-ctx.Done():
			status = "cancelled"
			exit = true
		case image, ok := <-imageChan:
			if !ok {
				status = "finished"
				if album.Percentage < 100 {
					status = "partially finished"
				}
				exit = true
			} else {
				status = "running"
				lck.Lock()
				album.UpdatedAt = time.Now().UTC()
				images := toImages(ctx, client, image, imgDir, cfg.Download, cfg.Upscale, cfg.Thumbnail)
				if err != nil {
					log.Println(err)
				} else {
					album.Images = append(album.Images, images...)
					if image.IsLast {
						album.Finished = append(album.Finished, image.PromptIndex)
					}
				}
				lck.Unlock()
			}
		}
		lck.Lock()
		album.UpdatedAt = time.Now().UTC()
		percentage := float32(len(album.Images)) * 100.0 / float32(total)
		if percentage > album.Percentage {
			avg := album.UpdatedAt.Sub(album.CreatedAt) / time.Duration(len(album.Images))
			estimated := (time.Duration(total-len(album.Images)) * avg).Round(time.Minute)
			if o.onUpdate != nil {
				o.onUpdate(Status{
					Percentage: percentage,
					Estimated:  estimated,
				})
			}
		}
		album.Percentage = percentage
		album.Status = status

		err := SaveAlbum(albumDir, album, cfg.Thumbnail)
		lck.Unlock()
		if err != nil {
			return fmt.Errorf("couldn't generate html: %w", err)
		}
	}
	log.Printf("album %s %s\n", albumDir, album.Status)

	return nil
}

func toImages(ctx context.Context, client *discord.Client, image *ai.Image, imgDir string, download, upscale, preview bool) []*Image {
	if !download {
		return []*Image{{
			Prompt: image.Prompt,
			URL:    image.URL,
		}}
	}

	// Create image output name
	localFile := image.FileName()
	imgOutput := fmt.Sprintf("%s/%s", imgDir, localFile)
	if err := client.Download(ctx, image.URL, imgOutput); err != nil {
		log.Println(fmt.Errorf("❌ couldn't download `%s`: %w", image.URL, err))
	}

	// Generate preview image
	if upscale && preview {
		base := filepath.Base(imgOutput)
		base = base[:len(base)-len(filepath.Ext(base))]
		previewOutput := fmt.Sprintf("%s/_thumbnails/%s.jpg", imgDir, base)
		if err := img.Resize(8, imgOutput, previewOutput); err != nil {
			log.Println(fmt.Errorf("❌ couldn't preview `%s`: %w", imgOutput, err))
		}
	}

	// Current image is an upscale image, return it
	if upscale {
		return []*Image{{
			Prompt: image.Prompt,
			URL:    image.URL,
			File:   localFile,
		}}
	}

	var images []*Image

	// Split preview images when upscale is disabled
	localFiles := image.FileNames()
	var imgOutputs []string
	for _, localFile := range localFiles {
		imgOutputs = append(imgOutputs, fmt.Sprintf("%s/%s", imgDir, localFile))
		images = append(images, &Image{
			Prompt: image.Prompt,
			URL:    image.URL,
			File:   localFile,
		})
	}
	if err := img.Split4(imgOutput, imgOutputs); err != nil {
		log.Println(fmt.Errorf("❌ couldn't split `%s`: %w", imgOutput, err))
		return images
	}

	// Create preview images
	if preview {
		for _, imgOutput := range imgOutputs {
			base := filepath.Base(imgOutput)
			base = base[:len(base)-len(filepath.Ext(base))]
			previewOutput := fmt.Sprintf("%s/_thumbnails/%s.jpg", imgDir, base)
			if err := img.Resize(4, imgOutput, previewOutput); err != nil {
				log.Println(fmt.Errorf("❌ couldn't preview `%s`: %w", imgOutput, err))
			}
		}
	}
	return images
}

var albumHTML = `<html>
<head>
<style>
div.gallery {
	margin: 5px;
	border: 1px solid #ccc;
	float: left;
	width: 128px;
}

div.gallery:hover {
	border: 1px solid #777;
}

div.gallery img {
	width: 100%;
	height: auto;
}

div.gallery input {
	margin-top: 5px;
	width: 100%;
}
</style>
</head>
<body>

<h1>{{ .Title }}</h1>
<p>{{ .Status }}, elapsed: {{ .Elapsed }}</p>
{{range  .Images }}
<div class="gallery">
  <a target="_blank" href="{{ .URL }}">
    <img src="{{ .Source }}">
  </a>
  <input type="text" value="{{ .Prompt }}">
</div>
{{end}}

</body>
</html>
`

type htmlData struct {
	Title   string
	Status  string
	Images  []*htmlImage
	Elapsed string
}

type htmlImage struct {
	URL    string
	Source string
	Prompt string
}

func SaveAlbum(dir string, a *Album, thumbnail bool) error {
	// Sort images
	images := a.Images
	sort.Slice(images, func(i, j int) bool {
		return images[i].Prompt < images[j].Prompt || (images[i].Prompt == images[j].Prompt && images[i].URL < images[j].URL)
	})

	// Save json
	js, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("couldn't marshal album: %w", err)
	}
	if err := os.WriteFile(fmt.Sprintf("%s/data.json", dir), js, 0644); err != nil {
		return fmt.Errorf("couldn't write album: %w", err)
	}

	var local htmlData
	local.Title = fmt.Sprintf("Album %s", a.ID)
	local.Status = fmt.Sprintf("%s %d%% %s", a.Status, int(a.Percentage), a.UpdatedAt.Format("2006-01-02 15:04:05"))
	local.Elapsed = a.UpdatedAt.Sub(a.CreatedAt).String()
	external := local
	for _, img := range a.Images {
		prompt := strings.ReplaceAll(img.Prompt, "\"", "&quot;")
		external.Images = append(external.Images, &htmlImage{
			URL:    img.URL,
			Source: img.URL,
			Prompt: prompt,
		})
		url := fmt.Sprintf("images/%s", img.File)
		src := url
		if thumbnail {
			base := filepath.Base(img.File)
			base = base[:len(base)-len(filepath.Ext(base))]
			src = fmt.Sprintf("images/_thumbnails/%s.jpg", base)
		}
		local.Images = append(local.Images, &htmlImage{
			URL:    url,
			Source: src,
			Prompt: prompt,
		})
	}

	tmpl, err := template.New("album").Parse(albumHTML)
	if err != nil {
		return fmt.Errorf("couldn't parse template: %w", err)
	}

	// Write HTML with extarnal URLs
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, local); err != nil {
		return fmt.Errorf("couldn't execute template: %w", err)
	}
	if err := os.WriteFile(fmt.Sprintf("%s/index.html", dir), buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("couldn't write index.html: %w", err)
	}

	// Write HTML with local URLs
	buf = &bytes.Buffer{}
	if err := tmpl.Execute(buf, external); err != nil {
		return fmt.Errorf("couldn't execute template: %w", err)
	}
	if err := os.WriteFile(fmt.Sprintf("%s/remote.html", dir), buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("couldn't write remote.html: %w", err)
	}

	return nil
}
