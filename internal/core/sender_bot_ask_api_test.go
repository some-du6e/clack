package core

// senderBotAsk is SenderBot with ask (sender_bot_ask_test.go).
func senderBotAsk(a *App, m *Message, ask bool) string { return a.SenderBot(m, ask) }
