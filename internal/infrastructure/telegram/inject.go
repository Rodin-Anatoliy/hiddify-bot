package telegram

import "github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"

func (bot *Bot) InjectUseCases(supportUC *usecase.SupportUseCase, broadcastUC *usecase.BroadcastUseCase) {
	bot.supportUC = supportUC
	bot.broadcastUC = broadcastUC
}
