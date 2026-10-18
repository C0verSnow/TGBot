package handler

import (
	"github.com/C0verSnow/TGBot/internal/bot"
	"net/http"
)

func Handler(w http.ResponseWriter, r *http.Request) { bot.Production().Webhook(w, r) }
