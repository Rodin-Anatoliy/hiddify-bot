package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/config"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/hiddify"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/repository"
	tgbot "github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/telegram"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

func main() {
	cfg := config.MustLoad("config.yml")

	log := logger.New(cfg.Log.Level)
	slog.SetDefault(log)
	log.Info("starting hiddify-bot", "version", "0.1.0")

	db, err := repository.Open(cfg.DB.Path)
	if err != nil {
		log.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	userRepo := repository.NewUserRepository(db)
	ticketRepo := repository.NewTicketRepository(db)

	hiddifyClient := hiddify.NewClient(
		cfg.Hiddify.BaseURL,
		cfg.Hiddify.AdminProxy,
		cfg.Hiddify.APIKey,
		log,
	)

	userUC := usecase.NewUserUseCase(userRepo, hiddifyClient, log)

	bot, err := tgbot.New(
		cfg.Telegram.Token,
		cfg.Telegram.AdminID,
		userUC,
		nil,
		nil,
		log,
	)
	if err != nil {
		log.Error("failed to create telegram bot", "err", err)
		os.Exit(1)
	}

	supportUC := usecase.NewSupportUseCase(ticketRepo, bot, cfg.Telegram.AdminID, log)
	broadcastUC := usecase.NewBroadcastUseCase(userRepo, bot, log)

	bot.InjectUseCases(supportUC, broadcastUC)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("bot is running, press Ctrl+C to stop")
		bot.Start()
	}()

	<-quit
	log.Info("shutting down gracefully...")
	bot.Stop()
	log.Info("bot stopped")
}
