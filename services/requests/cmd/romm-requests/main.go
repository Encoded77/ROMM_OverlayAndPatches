// romm-requests serves the game-request API behind RomM's origin and drives
// requests from search to the library.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/api"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/config"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/engine"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/events"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/igdb"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/importer"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/notify"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/provider"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/provider/ttb"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/romm"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/settings"
	"github.com/Encoded77/ROMM_OverlayAndPatches/services/requests/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	held, err := settings.Load(ctx, db, settings.Defaults(cfg.TTBURL, cfg.TTBStorage))
	if err != nil {
		return err
	}
	rommClient := romm.New(cfg.RommURL, cfg.RommToken, cfg.AuthCacheTTL)
	index := romm.NewIndex(rommClient, log)
	hub := events.NewHub()
	ntfy := notify.New(cfg.NtfyURL, cfg.NtfyToken, cfg.PublicURL, log)
	eng := &engine.Engine{
		Store: db, Hub: hub, Romm: rommClient, Settings: held, Notify: ntfy, Index: index, Log: log,
		Importer:    importer.New(cfg.StagingRoot, cfg.LibraryRoot, cfg.ScanHook),
		NewProvider: func(c ttb.Config) provider.Provider { return ttb.New(c) },
	}
	server := &api.Server{
		Romm: rommClient, IGDB: igdb.New(cfg.IGDBClientID, cfg.IGDBClientSecret), Store: db, Hub: hub,
		Log: log, Engine: eng, Index: index, Settings: held, Notify: ntfy,
	}

	go index.Run(ctx, time.Hour)
	go eng.Run(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.Handler(cfg.BasePath),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "base", cfg.BasePath)
		errc <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}
