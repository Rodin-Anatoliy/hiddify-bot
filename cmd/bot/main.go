package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/config"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/repository/hiddify"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/repository/sqlite"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/service"
	tgbot "github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

// commit is injected at build time: -ldflags="-X main.commit=<hash>"
var commit = "unknown"

func main() {
	cfg := config.MustLoad()

	log := logger.New(cfg.Log.Level)
	slog.SetDefault(log)
	log.Info("starting hiddify-bot", "commit", commit)

	db, err := sqlite.Open(cfg.DB.Path)
	if err != nil {
		log.Error("database init failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	userRepo := sqlite.NewUserRepository(db)
	ticketRepo := sqlite.NewTicketRepository(db)
	sessionRepo := sqlite.NewAdminSessionRepository(db)

	if n, err := sessionRepo.DeleteExpired(context.Background()); err != nil {
		log.Warn("session cleanup failed", "err", err)
	} else if n > 0 {
		log.Info("cleaned up expired sessions", "count", n)
	}

	hiddifyClient := hiddify.NewClient(
		cfg.Hiddify.BaseURL,
		cfg.Hiddify.AdminProxy,
		cfg.Hiddify.UserProxy,
		cfg.Hiddify.APIKey,
		log,
	)

	userUC := service.NewUserUseCase(userRepo, hiddifyClient, log)

	bot, err := tgbot.New(cfg.Telegram.Token, cfg.Telegram.AdminID, userUC, sessionRepo, log)
	if err != nil {
		log.Error("telegram bot init failed", "err", err)
		os.Exit(1)
	}

	supportUC := service.NewSupportUseCase(ticketRepo, log)
	broadcastUC := service.NewBroadcastUseCase(userRepo, log)
	bot.InjectUseCases(supportUC, broadcastUC)

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
