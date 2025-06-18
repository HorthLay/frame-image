package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/disintegration/imaging"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

var (
	userStates     = make(map[int64]string)
	selectedFrames = make(map[int64]string)
	userPhotos     = make(map[int64]string)
	users          = make(map[string]User)
	usersMutex     = sync.Mutex{}
)

func main() {
	bot, err := tgbotapi.NewBotAPI("7575675023:AAH0KrU7KMrOXFVrS-ucN5Ofj9XK9_g-Sl8")
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	go startWebAPI()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil {
			continue
		}

		if update.Message != nil {
			userID := update.Message.From.ID
			chatID := update.Message.Chat.ID
			username := update.Message.From.UserName

			if update.Message.IsCommand() {
				switch update.Message.Command() {
				case "start":
					userStates[userID] = ""
					selectedFrames[userID] = ""
					userPhotos[userID] = ""

					if username != "" {
						usersMutex.Lock()
						if _, exists := users[username]; !exists {
							users[username] = User{ID: userID, Username: username}
							log.Printf("New user added: %s", username)
						}
						usersMutex.Unlock()
					}

					startMsg := tgbotapi.NewMessage(chatID, "👋 សូមស្វាគមន៍!\n\nមកកាន់កម្មវិធីបង្កើតរូបភាពស៊ុមរបស់យើង។\n\nសូមជ្រើសរើសស៊ុមដើម្បីចាប់ផ្តើម។")
					startMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("🎬 Start Framing", "choose_frame"),
						),
					)
					bot.Send(startMsg)

				case "upload_image":
					userStates[userID] = "uploading_frame"
					bot.Send(tgbotapi.NewMessage(chatID, "📤 សូមផ្ញើរូបភាព PNG/JPG ជាមួយចំណងជើង (ឈ្មោះស៊ុម)។"))
				}
			}

			// Handle uploaded photo or image document
			if len(update.Message.Photo) > 0 || update.Message.Document != nil {
				state := userStates[userID]

				var fileID, fileName string
				var isImage bool

				if update.Message.Document != nil {
					doc := update.Message.Document
					if strings.HasPrefix(doc.MimeType, "image/") {
						fileID = doc.FileID
						fileName = doc.FileName
						isImage = true
					}
				} else {
					photo := update.Message.Photo[len(update.Message.Photo)-1]
					fileID = photo.FileID
					fileName = update.Message.Caption + ".png"
					isImage = true
				}

				if !isImage {
					bot.Send(tgbotapi.NewMessage(chatID, "❌ Only image files are allowed."))
					continue
				}

				switch state {
				case "uploading_frame":
					file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to get file."))
						break
					}

					resp, err := http.Get(file.Link(bot.Token))
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to download frame image."))
						break
					}
					defer resp.Body.Close()

					os.MkdirAll("frame", os.ModePerm)

					frameName := strings.TrimSpace(update.Message.Caption)
					if frameName == "" {
						frameName = strings.TrimSuffix(fileName, ".png")
					}
					savePath := fmt.Sprintf("frame/%s.png", frameName)

					outFile, err := os.Create(savePath)
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to save frame image."))
						break
					}
					defer outFile.Close()

					_, err = io.Copy(outFile, resp.Body)
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to write image to file."))
						break
					}

					bot.Send(tgbotapi.NewMessage(chatID, "✅ ស៊ុមបានបង្ហោះដោយជោគជ័យ `"+savePath+"`"))
					userStates[userID] = ""

				case "awaiting_photo":
					userPhotos[userID] = fileID
					userStates[userID] = "photo_uploaded"
					bot.Send(tgbotapi.NewMessage(chatID, "🖼 កំពុងដំណើរការរូបភាពរបស់អ្នក..."))
					processImage(bot, chatID, userID, selectedFrames[userID], fileID)
					userStates[userID] = ""
					selectedFrames[userID] = ""

				default:
					bot.Send(tgbotapi.NewMessage(chatID, "🤖 Not expecting a file right now."))
				}
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
				bot.Send(tgbotapi.NewMessage(chatID, "👋 សូម​ជ្រើសរើស​ស៊ុម:"))

				files, _ := os.ReadDir("frame")
				for _, f := range files {
					frameName := strings.TrimSuffix(f.Name(), ".png")
					photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath("frame/"+f.Name()))
					photo.Caption = "Frame Preview: " + frameName
					photo.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("Use this frame", frameName),
						),
					)
					bot.Send(photo)
				}
				continue
			}

			// frame selected
			selectedFrames[userID] = data
			userStates[userID] = "awaiting_photo"
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ You selected "+data))
			bot.Send(tgbotapi.NewMessage(chatID, "ឥឡូវ​នេះ សូម​បង្ហោះ​រូបថត​របស់​អ្នក 📷"))
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

	frameFile, err := os.Open("frame/" + frameName + ".png")
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
	msg.Caption = "🎉 នេះជារូបថតស៊ុមរបស់អ្នក!"
	bot.Send(msg)

	button := tgbotapi.NewInlineKeyboardButtonData("🔄 ជ្រើសរើសស៊ុមថ្មី", "choose_frame")
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(button),
	)

	msgWithButton := tgbotapi.NewMessage(chatID, "តើអ្នកចង់ជ្រើសរើសស៊ុមថ្មីមែនទេ?")
	msgWithButton.ReplyMarkup = markup
	bot.Send(msgWithButton)
}

func startWebAPI() {
	http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		usersMutex.Lock()
		defer usersMutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	})

	log.Println("🌐 Web API running at http://localhost:8080/users")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
