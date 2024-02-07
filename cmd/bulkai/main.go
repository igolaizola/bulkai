package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/igolaizola/bulkai"
	"github.com/igolaizola/bulkai/pkg/session"
	"github.com/peterbourgon/ff/v3"
	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/peterbourgon/ff/v3/ffyaml"
)

// Build flags
var Version = ""
var Commit = ""
var Date = ""

func main() {
	// Create signal based context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Launch command
	cmd := newCommand()
	if err := cmd.ParseAndRun(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func newCommand() *ffcli.Command {
	fs := flag.NewFlagSet("bulkai", flag.ExitOnError)

	return &ffcli.Command{
		ShortUsage: "bulkai [flags] <subcommand>",
		FlagSet:    fs,
		Exec: func(ctx context.Context, args []string) error {
			return flag.ErrHelp
		},
		Subcommands: []*ffcli.Command{
			newGenerateCommand(),
			newCreateSessionCommand(),
			newVersionCommand(),
		},
	}
}

func newGenerateCommand() *ffcli.Command {
	fs := flag.NewFlagSet("bulk", flag.ExitOnError)
	_ = fs.String("config", "bulkai.yaml", "config file (optional)")

	cfg := &bulkai.Config{}
	fs.StringVar(&cfg.Bot, "bot", "", "bot name")
	var prompts fsStrings
	fs.Var(&prompts, "prompt", "prompt list")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy address (optional)")
	fs.StringVar(&cfg.Output, "output", "output", "output directory")
	fs.StringVar(&cfg.Album, "album", "", "album id (optional)")
	fs.StringVar(&cfg.Prefix, "prefix", "", "prefix to be added")
	fs.StringVar(&cfg.Suffix, "suffix", "", "suffix to be added")
	fs.BoolVar(&cfg.Variation, "variation", false, "generate variations")
	fs.BoolVar(&cfg.Download, "download", true, "download images")
	fs.BoolVar(&cfg.Upscale, "upscale", true, "upscale images")
	fs.BoolVar(&cfg.Thumbnail, "thumbnail", true, "generate thumbnails")
	fs.BoolVar(&cfg.Html, "html", true, "generate html files")
	fs.StringVar(&cfg.Channel, "channel", "", "channel in format guid/channel (optional, if not provided DMs will be used)")
	fs.IntVar(&cfg.Concurrency, "concurrency", 3, "concurrency (optional, if 0 the maximum for the bot will be used)")
	fs.DurationVar(&cfg.Wait, "wait", 0, "wait time between prompts (optional)")
	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.ReplicateToken, "replicate-token", "", "replicate token (optional)")
	fs.BoolVar(&cfg.DiscordCDN, "discord-cdn", false, "use discord cdn instead of midjourney cdn")

	// Session
	fs.StringVar(&cfg.SessionFile, "session", "session.yaml", "session config file (optional)")

	fsSession := flag.NewFlagSet("", flag.ExitOnError)
	for _, fs := range []*flag.FlagSet{fs, fsSession} {
		fs.StringVar(&cfg.Session.UserAgent, "user-agent", "", "user agent")
		fs.StringVar(&cfg.Session.JA3, "ja3", "", "ja3 fingerprint")
		fs.StringVar(&cfg.Session.Language, "language", "", "language")
		fs.StringVar(&cfg.Session.Token, "token", "", "authentication token")
		fs.StringVar(&cfg.Session.SuperProperties, "super-properties", "", "super properties")
		fs.StringVar(&cfg.Session.Locale, "locale", "", "locale")
		fs.StringVar(&cfg.Session.Cookie, "cookie", "", "cookie")
	}

	return &ffcli.Command{
		Name:       "generate",
		ShortUsage: "bulkai generate [flags] <key> <value data...>",
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
			ff.WithEnvVarPrefix("BULKAI"),
		},
		ShortHelp: "generate images in bulk",
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			loadSession(fsSession, cfg.SessionFile)
			cfg.Prompts = prompts
			last := 0
			return bulkai.Generate(ctx, cfg, bulkai.WithOnUpdate(func(s bulkai.Status) {
				curr := int(s.Percentage)
				if curr == last {
					return
				}
				last = curr
				fmt.Printf("{\"progress\": \"%d\", \"estimated\": \"%s\"}\n", curr, s.Estimated)
			}))
		},
	}
}

func newCreateSessionCommand() *ffcli.Command {
	fs := flag.NewFlagSet("create-session", flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	output := fs.String("output", "session.yaml", "output file (optional)")
	proxy := fs.String("proxy", "", "proxy server (optional)")
	profile := fs.Bool("profile", false, "use profile (optional)")
	return &ffcli.Command{
		Name:       "create-session",
		ShortUsage: "bulkai create-session [flags] <key> <value data...>",
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ff.PlainParser),
			ff.WithEnvVarPrefix("BULKAI"),
		},
		ShortHelp: "create session using chrome",
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return session.Run(ctx, *profile, *output, *proxy)
		},
	}
}

func newVersionCommand() *ffcli.Command {
	return &ffcli.Command{
		Name:       "version",
		ShortUsage: "bulkai version",
		ShortHelp:  "print version",
		Exec: func(ctx context.Context, args []string) error {
			v := Version
			if v == "" {
				if buildInfo, ok := debug.ReadBuildInfo(); ok {
					v = buildInfo.Main.Version
				}
			}
			if v == "" {
				v = "dev"
			}
			versionFields := []string{v}
			if Commit != "" {
				versionFields = append(versionFields, Commit)
			}
			if Date != "" {
				versionFields = append(versionFields, Date)
			}
			fmt.Println(strings.Join(versionFields, " "))
			return nil
		},
	}
}

func loadSession(fs *flag.FlagSet, file string) error {
	if file == "" {
		return fmt.Errorf("session file not specified")
	}
	if _, err := os.Stat(file); err != nil {
		return nil
	}
	log.Printf("loading session from %s", file)
	return ff.Parse(fs, []string{}, []ff.Option{
		ff.WithConfigFile(file),
		ff.WithConfigFileParser(ffyaml.Parser),
	}...)
}

type fsStrings []string

func (f *fsStrings) String() string {
	return strings.Join(*f, ",")
}

func (f *fsStrings) Set(value string) error {
	*f = append(*f, value)
	return nil
}
