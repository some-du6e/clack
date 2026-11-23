package core

// senderBotFor is the bot a sender stands for (sender_bot_test.go).
func senderBotFor(a *App, user, bot string, bp *BotProfile) string {
	return a.SenderBot(&Message{User: user, BotID: bot, BotProfile: bp}, false)
}
