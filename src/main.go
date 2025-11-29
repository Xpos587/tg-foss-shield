package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"text/template"

	"github.com/altcha-org/altcha-lib-go"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

//go:embed index.html
var fs embed.FS

// Конфиг из ENV
var (
	BotToken = os.Getenv("BOT_TOKEN")
	BaseURL  = os.Getenv("BASE_URL") // http://your-ip:8080
	HMACKey  = os.Getenv("HMAC_KEY") // Любая рандомная строка
	Port     = "8080"
)

func main() {
	// Проверяем переменные окружения
	if BotToken == "" || BaseURL == "" || HMACKey == "" {
		log.Fatal("Required env vars: BOT_TOKEN, BASE_URL, HMAC_KEY")
	}

	// Стартуем бота
	b, err := bot.New(BotToken, bot.WithDefaultHandler(handler))
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Start(ctx)

	// HTTP сервер
	http.HandleFunc("/", serveCaptcha)
	http.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		verifyCaptcha(w, r, b)
	})

	log.Printf("Bot started on :%s", Port)
	log.Fatal(http.ListenAndServe(":"+Port, nil))
}

func handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	for _, user := range update.Message.NewChatMembers {
		if user.IsBot {
			continue
		}

		chatID := update.Message.Chat.ID
		userID := user.ID
		url := fmt.Sprintf("%s/?user_id=%d&chat_id=%d", BaseURL, userID, chatID)

		// Мьютим
		if err := restrictUser(b, ctx, chatID, userID, false); err != nil {
			log.Printf("Error muting user %d: %v", userID, err)
			continue
		}

		// Отправляем кнопку
		kb := &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "🤖 Я не робот", URL: url}},
			},
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("[%s](tg://user?id=%d), нажми чтобы писать в чат", user.FirstName, userID),
			ParseMode:   models.ParseModeMarkdown,
			ReplyMarkup: kb,
		})
	}
}

func serveCaptcha(w http.ResponseWriter, r *http.Request) {
	challenge, err := altcha.CreateChallenge(altcha.ChallengeOptions{
		HMACKey:   HMACKey,
		MaxNumber: 50000,
	})
	if err != nil {
		http.Error(w, "Failed", http.StatusInternalServerError)
		return
	}

	t, _ := template.ParseFS(fs, "index.html")
	w.Header().Set("Content-Type", "text/html")
	t.Execute(w, map[string]string{
		"Challenge": string(must(json.Marshal(challenge))),
		"UserID":    r.URL.Query().Get("user_id"),
		"ChatID":    r.URL.Query().Get("chat_id"),
	})
}

func verifyCaptcha(w http.ResponseWriter, r *http.Request, b *bot.Bot) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	payload := r.FormValue("altcha")
	userID := must(strconv.ParseInt(r.FormValue("user_id"), 10, 64))
	chatID := must(strconv.ParseInt(r.FormValue("chat_id"), 10, 64))

	if !must(altcha.VerifySolution(payload, HMACKey, true)) {
		http.Error(w, "Invalid", http.StatusForbidden)
		return
	}

	if err := restrictUser(b, context.Background(), chatID, userID, true); err != nil {
		log.Printf("Error unmuting user %d: %v", userID, err)
		http.Error(w, "Failed", http.StatusInternalServerError)
		return
	}

	log.Printf("Verified user %d in chat %d", userID, chatID)
	w.Write([]byte(`<!DOCTYPE html><meta charset="utf-8">
<title>✅ Verified</title><body style="font-family:sans-serif;text-align:center;padding:50px">
<h1>✅ Success!</h1><p>You can return to Telegram</p><script>setTimeout(()=>window.close(),3000)</script>`))
}

// restrictUser мьютит или размьютит юзера
func restrictUser(b *bot.Bot, ctx context.Context, chatID, userID int64, allow bool) error {
	perms := &models.ChatPermissions{
		CanSendMessages: allow,
	}

	if allow {
		perms.CanSendAudios = true
		perms.CanSendDocuments = true
		perms.CanSendPhotos = true
		perms.CanSendVideos = true
		perms.CanSendVideoNotes = true
		perms.CanSendVoiceNotes = true
		perms.CanSendPolls = true
		perms.CanSendOtherMessages = true
		perms.CanAddWebPagePreviews = true
	}

	_, err := b.RestrictChatMember(ctx, &bot.RestrictChatMemberParams{
		ChatID:                        chatID,
		UserID:                        userID,
		Permissions:                   perms,
		UseIndependentChatPermissions: true,
	})
	return err
}

// must - helper для паники при ошибках
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
