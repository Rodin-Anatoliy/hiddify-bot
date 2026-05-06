package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/config"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/hiddify"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/repository"
	tgbot "github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/telegram"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

// commit is injected at build time: -ldflags="-X main.commit=<hash>"
var commit = "unknown"

func main() {
	cfg := config.MustLoad()

	log := logger.New(cfg.Log.Level)
	slog.SetDefault(log)
	log.Info("starting hiddify-bot", "commit", commit)

	// ── Persistence ───────────────────────────────────────────────────────────
	db, err := repository.Open(cfg.DB.Path)
	if err != nil {
		log.Error("database init failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	userRepo    := repository.NewUserRepository(db)
	ticketRepo  := repository.NewTicketRepository(db)
	sessionRepo := repository.NewAdminSessionRepository(db)

	// Clean up expired admin reply sessions from previous runs.
	if n, err := sessionRepo.DeleteExpired(context.Background()); err != nil {
		log.Warn("session cleanup failed", "err", err)
	} else if n > 0 {
		log.Info("cleaned up expired sessions", "count", n)
	}

	// ── Hiddify API ───────────────────────────────────────────────────────────
	hiddifyClient := hiddify.NewClient(
		cfg.Hiddify.BaseURL,
		cfg.Hiddify.AdminProxy,
		cfg.Hiddify.UserProxy,
		cfg.Hiddify.APIKey,
		log,
	)

	// ── Use cases ─────────────────────────────────────────────────────────────
	userUC := usecase.NewUserUseCase(userRepo, hiddifyClient, log)

	// ── Telegram bot ──────────────────────────────────────────────────────────
	// Bot is built first because it implements port.Sender, which support and
	// broadcast use cases depend on. Use cases are injected after construction.
	bot, err := tgbot.New(cfg.Telegram.Token, cfg.Telegram.AdminID, userUC, sessionRepo, log)
	if err != nil {
		log.Error("telegram bot init failed", "err", err)
		os.Exit(1)
	}

	supportUC   := usecase.NewSupportUseCase(ticketRepo, bot, log)
	broadcastUC := usecase.NewBroadcastUseCase(userRepo, bot, log)
	bot.InjectUseCases(supportUC, broadcastUC)

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("bot started")
		bot.Start()
	}()

	<-quit
	log.Info("shutting down...")
	bot.Stop()
	log.Info("stopped")
}
