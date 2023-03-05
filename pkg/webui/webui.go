package webui

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/pkg/browser"
)

// Embed the build directory from the frontend.
//
//go:embed static
var static embed.FS

func Serve(ctx context.Context, port int) error {
	// Listen on a random port
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}
	port = listener.Addr().(*net.TCPAddr).Port

	// Serve static files
	staticFS, err := fs.Sub(static, "static")
	if err != nil {
		log.Fatal(err)
	}
	staticHandler := http.FileServer(http.FS(staticFS))

	// Add handlers
	mux := http.NewServeMux()
	mux.Handle("/", staticHandler)
	srv := &http.Server{
		Handler: mux,
	}

	// Start web server
	errC := make(chan error)
	defer close(errC)
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errC <- err
		}
	}()

	// Open browser when the server is ready
	go func() {
		// Open browser when the server is ready
		go func() {
			u := fmt.Sprintf("http://localhost:%d", port)
			_ = browser.OpenURL(u)
			fmt.Println("Open in your browser:", u)
		}()
	}()

	// Wait server error or context cancelation
	select {
	case err := <-errC:
		return err
	case <-ctx.Done():
	}

	// Try to gracefully shutdown the server
	shutdownCTX, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCTX); err != nil {
		return err
	}
	return err
}
