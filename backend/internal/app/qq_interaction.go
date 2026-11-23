package app

import (
	"ai-bot/backend/internal/qq"
	"context"
)

func (a *App) ackInteraction(ctx context.Context, bot, id string) error {
	var app, enc string
	if err := a.db.QueryRow(ctx, "SELECT app_id,app_secret_enc FROM bots WHERE id=$1 AND enabled", bot).Scan(&app, &enc); err != nil {
		return err
	}
	secret, err := a.cipher.Decrypt(enc)
	if err != nil {
		return err
	}
	return qq.NewClient(app, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL).Acknowledge(ctx, id)
}
