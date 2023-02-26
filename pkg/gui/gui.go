package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/unit"
	"github.com/igolaizola/bulkai"
	"github.com/igolaizola/bulkai/pkg/session"
	"github.com/igolaizola/giov/wid"
	"golang.org/x/exp/shiny/materialdesign/icons"
	"gopkg.in/yaml.v2"
)

func Run(ctx context.Context, version string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	th := wid.NewTheme(gofont.Collection(), 14)
	s := newState(ctx, th)
	defer s.saveConfig()

	go func() {
		defer cancel()
		wid.RunWithContext(ctx,
			app.NewWindow(app.Title(fmt.Sprintf("BULKAI (%s)", version)), app.Size(unit.Dp(600), unit.Dp(600))),
			s.w,
			th,
		)
	}()

	go app.Main()
	go s.log()
	<-ctx.Done()
	return nil
}

func (s *state) log() {
	r, w := io.Pipe()
	defer r.Close()
	log.SetOutput(w)
	buf := make([]byte, 1024)
	t := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			n, err := r.Read(buf)
			if err != nil {
				s.error(err)
				return
			}
			s.logs = string(buf[:n]) + s.logs
		}
	}
}

var (
	configFile  = "bulkai.yaml"
	sessionFile = "session.yaml"

	bots = []string{"midjourney", "bluewillow"}

	homeIcon, _     = wid.NewIcon(icons.ActionHome)
	settingsIcon, _ = wid.NewIcon(icons.ActionSettings)
	infoIcon, _     = wid.NewIcon(icons.ActionInfo)
	alertIcon, _    = wid.NewIcon(icons.AlertErrorOutline)
	stopIcon, _     = wid.NewIcon(icons.NavigationCancel)
	newIcon, _      = wid.NewIcon(icons.ContentAdd)
	deleteIcon, _   = wid.NewIcon(icons.ContentClear)

	redFg   = wid.RGB(0xff0000)
	greenFg = wid.RGB(0x2c853b)
	grayFg  = wid.RGB(0x808080)
	blackFg = wid.RGB(0x000000)
)

type jobStatus int

var (
	notStarted jobStatus = 0
	running    jobStatus = 1
	finished   jobStatus = 2
)

type state struct {
	ctx context.Context
	th  *wid.Theme
	w   *layout.Widget
	cfg *bulkai.Config

	// UI fields
	page       string
	logs       string
	err        error
	job        jobStatus
	percentage float32
	estimated  time.Duration
	cancel     context.CancelFunc

	// Config fields that need to be parsed
	prompts     string
	bot         int
	wait        string
	concurrency string
}

func newState(ctx context.Context, th *wid.Theme) *state {
	var cfg *bulkai.Config
	var stateErr error

	// Load config from file
	f, err := os.Open(configFile)
	if err == nil {
		var candidateCfg bulkai.Config
		if err := yaml.NewDecoder(f).Decode(&candidateCfg); err != nil {
			stateErr = err
		} else {
			cfg = &candidateCfg
		}
		f.Close()
	}
	// Create new config if it doesn't exist
	if cfg == nil {
		cfg = &bulkai.Config{
			Bot:       bots[0],
			Output:    "output",
			Download:  true,
			Upscale:   true,
			Thumbnail: true,
			Variation: false,
		}
	}

	// Get bot index
	var bot int
	for i, b := range bots {
		if b == cfg.Bot {
			bot = i
			break
		}
	}

	s := &state{
		ctx:  ctx,
		th:   th,
		cfg:  cfg,
		bot:  bot,
		page: "main",
		w:    new(layout.Widget),
		job:  notStarted,
		err:  stateErr,
	}

	s.saveConfig()
	s.refresh()
	return s
}

func (s *state) error(err error) {
	log.Println(err)
	s.err = err
	s.refresh()
}

func (s *state) refresh() {
	var widgets []layout.Widget
	switch s.page {
	default:
		switch s.job {
		case running:
			widgets = []layout.Widget{
				wid.Label(s.th, "Running", wid.Middle(), wid.Heading()),
				wid.Label(s.th, fmt.Sprintf("Progress: %.2f%%", s.percentage), wid.Middle()),
				wid.Label(s.th, fmt.Sprintf("Estimated time to finish: %s", s.estimated), wid.Middle()),
				wid.ProgressBar(s.th, &s.percentage),
				wid.Label(s.th, "", wid.Heading()),
				wid.Label(s.th, "Press STOP if you want to cancel the current job"),
				wid.Space(unit.Dp(2)),
				wid.OutlineButton(s.th, "STOP", wid.BtnIcon(stopIcon), wid.Fg(&redFg), wid.Do(func() {
					if s.cancel != nil {
						s.cancel()
					}
				})),
			}
		case finished:
			widgets = []layout.Widget{
				wid.Label(s.th, "Finished", wid.Middle(), wid.Heading()),
				wid.Label(s.th, fmt.Sprintf("Progress: %.2f%%", s.percentage), wid.Middle()),
				wid.Label(s.th, fmt.Sprintf("Estimated time to finish: %s", time.Duration(0)), wid.Middle()),
				wid.ProgressBar(s.th, &s.percentage),
				wid.Label(s.th, "", wid.Heading()),
				wid.Label(s.th, "Press NEW if you want to configure a new process"),
				wid.OutlineButton(s.th, "NEW", wid.BtnIcon(newIcon), wid.Fg(&greenFg), wid.Do(func() {
					s.job = notStarted
					s.refresh()
				})),
			}
		default:
			widgets = []layout.Widget{
				wid.Edit(s.th, wid.Lbl("Album name"), wid.Var(&s.cfg.Album)),
				wid.Edit(s.th, wid.Lbl("Prefix"), wid.Var(&s.cfg.Prefix)),
				wid.Edit(s.th, wid.Lbl("Suffix"), wid.Var(&s.cfg.Suffix)),
				wid.Edit(s.th, wid.Lbl("Prompts"), wid.Var(&s.prompts), wid.Area(300, 300)),
				wid.Button(s.th, "START", wid.Do(func() { go s.start() })),
			}
		}
	case "settings":
		widgets = []layout.Widget{
			wid.DropDown(s.th, &s.bot, bots, wid.Lbl("AI Bot")),
			wid.Edit(s.th, wid.Lbl("Output folder"), wid.Var(&s.cfg.Output)),
			wid.Checkbox(s.th, "Download", wid.Bool(&s.cfg.Download)),
			wid.Checkbox(s.th, "Generate thumbnails", wid.Bool(&s.cfg.Thumbnail)),
			wid.Checkbox(s.th, "Upscale", wid.Bool(&s.cfg.Upscale)),
			wid.Checkbox(s.th, "Generate variations", wid.Bool(&s.cfg.Variation)),
			wid.Edit(s.th, wid.Lbl("Custom channel"), wid.Var(&s.cfg.Channel)),
			wid.Edit(s.th, wid.Lbl("Wait time"), wid.Var(&s.wait)),
			wid.Edit(s.th, wid.Lbl("Concurrency"), wid.Var(&s.concurrency)),
			wid.Edit(s.th, wid.Lbl("Proxy"), wid.Var(&s.cfg.Proxy)),
			wid.Row(s.th, nil, []float32{1, 1},
				wid.Label(s.th, "Launch chrome to import session", wid.Right()),
				wid.Button(s.th, "Create session", wid.Do(func() { go s.createSession() })),
			),
		}
	case "logs":
		widgets = []layout.Widget{
			wid.Row(s.th, nil, []float32{1, 1},
				wid.OutlineButton(s.th, "Press to clear", wid.BtnIcon(deleteIcon), wid.Do(func() {
					s.logs = ""
					s.refresh()
				})),
				wid.Label(s.th, "Log output, use mouse wheel to scroll"),
			),
			wid.Edit(s.th, wid.ReadOnly(), wid.Area(500, 500), wid.Var(&s.logs)),
		}
	}
	*s.w = s.grid(widgets...)
}

func (s *state) grid(body ...layout.Widget) layout.Widget {
	widgets := append(s.header(), body...)
	return wid.Row(s.th, nil, []float32{.05, .9, .05},
		// Left column
		wid.Col(nil, wid.Space(unit.Dp(1))),
		// Middle column
		wid.Col(nil, widgets...),
		// Right column
		wid.Col(nil, wid.Space(unit.Dp(1))),
	)
}

func (s *state) header() []layout.Widget {
	mainFg := grayFg
	settingsFg := grayFg
	aboutFg := grayFg
	switch s.page {
	case "main":
		mainFg = blackFg
	case "settings":
		settingsFg = blackFg
	case "logs":
		aboutFg = blackFg
	}

	var widgets []layout.Widget
	widgets = append(widgets,
		wid.Label(s.th, "AI image generator tool", wid.Middle(), wid.Bold(), wid.Role(wid.PrimaryContainer)),
		wid.Row(s.th, nil, []float32{1, 1, 1},
			wid.HeaderButton(s.th, "Main", wid.BtnIcon(homeIcon), wid.Fg(&mainFg), wid.Do(func() {
				s.page = "main"
				s.saveConfig()
				s.refresh()
			})),
			wid.HeaderButton(s.th, "Settings", wid.BtnIcon(settingsIcon), wid.Fg(&settingsFg), wid.Do(func() {
				s.page = "settings"
				s.saveConfig()
				s.refresh()
			})),
			wid.HeaderButton(s.th, "Logs", wid.BtnIcon(infoIcon), wid.Fg(&aboutFg), wid.Do(func() {
				s.page = "logs"
				s.saveConfig()
				s.refresh()
			})),
		),
	)
	if s.err != nil {
		widgets = append(widgets,
			wid.Separator(s.th, unit.Dp(1)),
			wid.HeaderButton(s.th, s.err.Error(), wid.BtnIcon(alertIcon), wid.Fg(&redFg), wid.Do(func() {
				s.err = nil
				s.refresh()
			})),
			wid.Separator(s.th, unit.Dp(1)),
		)
	}
	return widgets
}

func (s *state) start() {
	prompts := strings.TrimSpace(s.prompts)
	if prompts == "" {
		s.error(errors.New("prompts cannot be empty"))
		return
	}
	s.cfg.Prompts = strings.Split(prompts, "\n")
	s.cfg.Bot = bots[s.bot]

	// Load session from session.yaml
	f, err := os.Open(sessionFile)
	if err != nil {
		s.error(err)
		return
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	if err = decoder.Decode(&s.cfg.Session); err != nil {
		s.error(err)
		return
	}

	// Save the config
	s.saveConfig()

	s.err = nil
	s.job = running
	s.percentage = 0
	s.estimated = 0
	s.refresh()
	ctx, cancel := context.WithCancel(s.ctx)
	defer func() {
		cancel()
		s.job = finished
		s.refresh()
	}()
	s.cancel = cancel

	// Start the bulk generate command
	if err := bulkai.Generate(ctx, s.cfg, bulkai.WithOnUpdate(s.onUpdate)); err != nil {
		s.error(err)
		return
	}
}

func (s *state) createSession() {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		s.error(err)
		return
	}
	if err := session.Run(s.ctx, false, cwd, s.cfg.Proxy); err != nil {
		s.error(err)
	}
}

func (s *state) onUpdate(status bulkai.Status) {
	s.percentage = status.Percentage / 100.0
	s.estimated = status.Estimated
	s.refresh()
}

func (s *state) saveConfig() {
	// Convert UI types to config types
	bot := bots[s.bot]
	if s.concurrency != "" {
		concCandidate, err := strconv.Atoi(s.concurrency)
		if err != nil {
			s.error(err)
			return
		}
		s.cfg.Concurrency = concCandidate
	}
	if s.wait != "" {
		waitCandidate, err := strconv.Atoi(s.wait)
		if err != nil {
			s.error(err)
			return
		}
		s.cfg.Wait = time.Duration(waitCandidate) * time.Second
	}

	// Create or open the file
	f, err := os.Create(configFile)
	if err != nil {
		s.error(err)
		return
	}
	defer f.Close()

	encoder := yaml.NewEncoder(f)

	// Save all fields except album, prompts, prefix and suffix
	cfg := &bulkai.Config{
		Debug:       s.cfg.Debug,
		Bot:         bot,
		Proxy:       s.cfg.Proxy,
		Output:      s.cfg.Output,
		Variation:   s.cfg.Variation,
		Upscale:     s.cfg.Upscale,
		Download:    s.cfg.Download,
		Thumbnail:   s.cfg.Thumbnail,
		Channel:     s.cfg.Channel,
		Concurrency: s.cfg.Concurrency,
		Wait:        s.cfg.Wait,
	}
	if err = encoder.Encode(&cfg); err != nil {
		s.error(err)
		return
	}
}
