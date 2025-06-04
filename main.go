package main

import (
	"bytes"
	"image"
	"image/png"
	"log"
	"net/http"
	"os"

	"github.com/disintegration/imaging"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	bot, err := tgbotapi.NewBotAPI("7968847397:AAEp1CVGguISa6tXcU5k1Hnsth5Fhyvaxr4")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	userStates := make(map[int64]string)
	selectedFrames := make(map[int64]string)
	userPhotos := make(map[int64]string)

	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil {
			continue
		}

		if update.Message != nil {
			userID := update.Message.From.ID
			chatID := update.Message.Chat.ID

			if update.Message.IsCommand() {
				switch update.Message.Command() {
				case "start":
					userStates[userID] = ""
					selectedFrames[userID] = ""
					userPhotos[userID] = ""

					startMsg := tgbotapi.NewMessage(chatID, "👋 Welcome! Click below to start framing your photo:")
					startMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("🎬 Start Framing", "choose_frame"),
						),
					)
					bot.Send(startMsg)
				}
			}

			if len(update.Message.Photo) > 0 && userStates[userID] == "awaiting_photo" {
				photo := update.Message.Photo[len(update.Message.Photo)-1]
				userPhotos[userID] = photo.FileID
				userStates[userID] = "photo_uploaded"

				bot.Send(tgbotapi.NewMessage(chatID, "🖼 Processing your photo..."))
				processImage(bot, chatID, userID, selectedFrames[userID], photo.FileID)

				userStates[userID] = ""
				selectedFrames[userID] = ""
			} else if userStates[userID] == "awaiting_photo" {
				bot.Send(tgbotapi.NewMessage(chatID, "📸 Please upload a photo."))
			}
		}

		if update.CallbackQuery != nil {
			userID := update.CallbackQuery.From.ID
			chatID := update.CallbackQuery.Message.Chat.ID
			data := update.CallbackQuery.Data

			if data == "choose_frame" {
				userStates[userID] = "awaiting_frame"
				selectedFrames[userID] = ""
				userPhotos[userID] = ""

				bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "🔄 Choose a frame"))
				bot.Send(tgbotapi.NewMessage(chatID, "👋 Please choose a frame:"))

				frames := []string{"frame1", "frame2", "frame3"}
				for _, frame := range frames {
					photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(frame+".png"))
					photo.Caption = "Frame Preview: " + frame
					photo.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("Use this frame", frame),
						),
					)
					bot.Send(photo)
				}
				continue
			}

			// A frame was selected
			frame := data
			selectedFrames[userID] = frame
			userStates[userID] = "awaiting_photo"

			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ You selected "+frame))
			bot.Send(tgbotapi.NewMessage(chatID, "Now please upload your photo 📷"))
		}
	}
}

func processImage(bot *tgbotapi.BotAPI, chatID int64, userID int64, frameName, photoFileID string) {
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: photoFileID})
	if err != nil {
		log.Println("Error getting file:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to retrieve photo."))
		return
	}

	resp, err := http.Get(file.Link(bot.Token))
	if err != nil {
		log.Println("Error downloading photo:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to download photo."))
		return
	}
	defer resp.Body.Close()

	userImg, _, err := image.Decode(resp.Body)
	if err != nil {
		log.Println("Error decoding photo:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to decode photo."))
		return
	}

	frameFile, err := os.Open(frameName + ".png")
	if err != nil {
		log.Println("Error opening frame image:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to open frame image."))
		return
	}
	defer frameFile.Close()

	frameImg, _, err := image.Decode(frameFile)
	if err != nil {
		log.Println("Error decoding frame image:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to decode frame image."))
		return
	}

	userImg = imaging.Fill(userImg, frameImg.Bounds().Dx(), frameImg.Bounds().Dy(), imaging.Center, imaging.Lanczos)

	result := imaging.Overlay(userImg, frameImg, image.Point{0, 0}, 1.0)

	buf := new(bytes.Buffer)
	if err := png.Encode(buf, result); err != nil {
		log.Println("Error encoding result:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to encode final image."))
		return
	}

	msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
		Name:  "framed_photo.png",
		Bytes: buf.Bytes(),
	})
	msg.Caption = "🎉 Here's your framed photo!"
	bot.Send(msg)

	// Send "Choose a new frame" button
	button := tgbotapi.NewInlineKeyboardButtonData("🔄 Choose a new frame", "choose_frame")
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(button),
	)

	msgWithButton := tgbotapi.NewMessage(chatID, "Want to frame another photo?")
	msgWithButton.ReplyMarkup = markup
	bot.Send(msgWithButton)
}
