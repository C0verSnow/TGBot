package handler

import (
	"github.com/C0verSnow/TGBot/internal/bot"
	"net/http"
)

func Probe(w http.ResponseWriter, r *http.Request) { bot.Production().Probe(w, r) }
