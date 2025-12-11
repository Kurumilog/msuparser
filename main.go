package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	CheckInterval  = 1 * time.Minute
	TelegramAPIURL = "https://api.telegram.org/bot"
)

var (
	BotToken            string
	UserID              string
	NotificationMinutes int
)

type Config struct {
	BotToken            string `json:"BOT_TOKEN"`
	UserID              string `json:"USER_ID"`
	NotificationMinutes int    `json:"NOTIFICATION_MINUTES"`
}

type TimetableBot struct {
	botToken                  string
	userID                    string
	schedule                  []Lesson
	sentNotifications         map[string]bool
	sentDistanceNotifications map[string]bool // Трекинг дистанционных уведомлений по дате
	lastUpdateID              int
}

type Update struct {
	UpdateID int     `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type UpdateResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

func LoadConfig() (Config, error) {
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		// Пытаемся загрузить из Python скрипта
		cmd := exec.Command("python3", "get_config.py")
		output, err := cmd.Output()
		if err != nil {
			return Config{}, fmt.Errorf("не могу загрузить конфиг: %v", err)
		}

		var config Config
		if err := json.Unmarshal(output, &config); err != nil {
			return Config{}, fmt.Errorf("ошибка парсинга конфига: %v", err)
		}

		return config, nil
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("ошибка парсинга config.json: %v", err)
	}

	return config, nil
}

func NewTimetableBot(token, userID string) *TimetableBot {
	return &TimetableBot{
		botToken:                  token,
		userID:                    userID,
		schedule:                  []Lesson{},
		sentNotifications:         make(map[string]bool),
		sentDistanceNotifications: make(map[string]bool),
		lastUpdateID:              0,
	}
}

func (bot *TimetableBot) LoadSchedule(filename string) error {
	fmt.Println("📂 Загружаю расписание...")

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("❌ Файл %s не найден!\n", filename)
		fmt.Println("💡 Запусти сначала парсер: python parser_full.py")
		return err
	}

	err = json.Unmarshal(data, &bot.schedule)
	if err != nil {
		fmt.Printf("❌ Ошибка парсинга JSON: %v\n", err)
		return err
	}

	fmt.Printf("✅ Загружено %d пар\n", len(bot.schedule))
	return nil
}

func (bot *TimetableBot) UpdateSchedule() {
	// Запускаем парсер для обновления расписания
	cmd := exec.Command("./test_parser")
	cmd.Dir = "."

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ Ошибка запуска парсера: %v\n", err)
		fmt.Printf("Вывод: %s\n", string(output))
		return
	}

	// Перезагружаем расписание из обновленного файла
	err = bot.LoadSchedule("schedule.json")
	if err != nil {
		fmt.Printf("❌ Ошибка перезагрузки расписания: %v\n", err)
		return
	}
}

func (bot *TimetableBot) SendMessage(message string) error {
	endpoint := fmt.Sprintf("%s%s/sendMessage", TelegramAPIURL, BotToken)

	data := url.Values{}
	data.Set("chat_id", UserID)
	data.Set("text", message)
	data.Set("parse_mode", "HTML")

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		fmt.Printf("❌ Ошибка отправки: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("❌ Ошибка Telegram API: статус %d\n", resp.StatusCode)
		return fmt.Errorf("telegram error: %d", resp.StatusCode)
	}

	return nil
}

func (bot *TimetableBot) FormatNotification(lesson *Lesson) string {
	message := fmt.Sprintf(
		"🔔 <b>Скоро пара!</b>\n\n"+
			"📚 <b>Предмет:</b> %s\n"+
			"👨‍🏫 <b>Преподаватель:</b> %s\n"+
			"🚪 <b>Аудитория:</b> %s\n\n"+
			"🕐 <b>Время:</b> %s - %s\n"+
			"📅 <b>Дата:</b> %s (%s)",
		lesson.Subject,
		lesson.Teacher,
		lesson.Room,
		lesson.TimeStart,
		lesson.TimeEnd,
		lesson.Date,
		lesson.Weekday,
	)
	return message
}

func ParseTime(dateStr, timeStr string) (time.Time, error) {
	// Парсим дату/время в московском часовом поясе (Europe/Moscow)
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		// fallback на локальную зону
		loc = time.Local
	}

	dateTimeStr := fmt.Sprintf("%s %s", dateStr, timeStr)
	return time.ParseInLocation("02.01.2006 15:04", dateTimeStr, loc)
}

func (bot *TimetableBot) GetUpcomingLessons() []Lesson {
	// Используем московское время (Europe/Moscow)
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	upcoming := []Lesson{}

	for _, lesson := range bot.schedule {
		notificationTime, err := ParseTime(lesson.Date, lesson.TimeStart)
		if err != nil {
			continue
		}

		// Устанавливаем время уведомления
		// Для 3 пары - за 45 минут (в 12:15 если пара в 13:00)
		// Для остальных - за 15 минут
		minutesToSubtract := NotificationMinutes
		if lesson.LessonNumber == "3" {
			minutesToSubtract = 45
		}
		notificationTime = notificationTime.Add(-time.Duration(minutesToSubtract) * time.Minute)

		// Проверяем что пара в будущем
		// Сравниваем во временной зоне Минска
		if now.Before(notificationTime.In(loc)) {
			lesson.Notification = notificationTime
			upcoming = append(upcoming, lesson)
		}
	}

	return upcoming
}

// isDistanceLearning проверяет является ли пара дистанционной
func isDistanceLearning(room string) bool {
	room = strings.ToLower(strings.TrimSpace(room))
	return strings.Contains(room, "дистанц") || strings.Contains(room, "виртуал")
}

// SendDistanceLearningNotification отправляет уведомление о дистанционных парах за день
func (bot *TimetableBot) SendDistanceLearningNotification(date string, lessons []Lesson) {
	if bot.sentDistanceNotifications[date] {
		return
	}

	message := fmt.Sprintf(
		"📱 <b>Утреннее напоминание</b>\n\n" +
			"У вас сегодня дистанционные пары:\n\n",
	)

	for _, lesson := range lessons {
		message += fmt.Sprintf(
			"• %s пара (%s-%s)\n  📚 %s\n",
			lesson.LessonNumber,
			lesson.TimeStart,
			lesson.TimeEnd,
			lesson.Subject,
		)
		if lesson.Teacher != "" {
			message += fmt.Sprintf("  👨‍🏫 %s\n", lesson.Teacher)
		}
	}

	err := bot.SendMessage(message)
	if err == nil {
		fmt.Printf("✅ Отправлено уведомление о дистанционных парах на %s\n", date)
		bot.sentDistanceNotifications[date] = true
	}
}

// GetTodayDistanceLessons возвращает все дистанционные пары на сегодня
func (bot *TimetableBot) GetTodayDistanceLessons() map[string][]Lesson {
	loc, _ := time.LoadLocation("Europe/Moscow")
	now := time.Now().In(loc)
	today := now.Format("02.01.2006")

	distanceLessons := make(map[string][]Lesson)

	for _, lesson := range bot.schedule {
		if lesson.Date == today && isDistanceLearning(lesson.Room) {
			distanceLessons[lesson.Date] = append(distanceLessons[lesson.Date], lesson)
		}
	}

	return distanceLessons
}

// HasInPersonLessonsToday проверяет есть ли очные пары сегодня
func (bot *TimetableBot) HasInPersonLessonsToday() bool {
	loc, _ := time.LoadLocation("Europe/Moscow")
	now := time.Now().In(loc)
	today := now.Format("02.01.2006")

	for _, lesson := range bot.schedule {
		if lesson.Date == today && !isDistanceLearning(lesson.Room) {
			return true
		}
	}
	return false
}

func (bot *TimetableBot) CheckAndSendNotifications() {
	now := time.Now()
	upcoming := bot.GetUpcomingLessons()

	// Проверяем: если ВСЕ пары дистанционные (нет очных) - отправляем утреннее уведомление в 8:00
	loc, _ := time.LoadLocation("Europe/Moscow")
	nowInMoscow := now.In(loc)
	if nowInMoscow.Hour() == 8 && nowInMoscow.Minute() == 0 {
		// Отправляем утреннее уведомление только если нет очных пар
		if !bot.HasInPersonLessonsToday() {
			distanceLessons := bot.GetTodayDistanceLessons()
			for date, lessons := range distanceLessons {
				bot.SendDistanceLearningNotification(date, lessons)
			}
		}
	}

	// Отправляем обычные уведомления за 30 минут
	for _, lesson := range upcoming {
		// Если есть очные пары сегодня - отправляем уведомления для ВСЕХ пар (включая дистанционные)
		// Если НЕТ очных пар - пропускаем дистанционные (они уже получили утреннее уведомление)
		if !bot.HasInPersonLessonsToday() && isDistanceLearning(lesson.Room) {
			continue
		}

		lessonKey := fmt.Sprintf("%s_%s_%s", lesson.Date, lesson.LessonNumber, lesson.Subject)

		if bot.sentNotifications[lessonKey] {
			continue
		}

		timeDiff := lesson.Notification.Sub(now).Seconds()

		// Отправляем если осталось меньше 60 секунд
		if timeDiff >= 0 && timeDiff <= 60 {
			message := bot.FormatNotification(&lesson)
			err := bot.SendMessage(message)
			if err == nil {
				fmt.Printf("✅ Отправлено уведомление: %s (%s %s)\n",
					lesson.Subject, lesson.Date, lesson.TimeStart)
				bot.sentNotifications[lessonKey] = true
			}
		}
	}
}

func (bot *TimetableBot) RunScheduler() {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("🤖 БОТ ЗАПУЩЕН")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("⏰ Проверяю расписание каждую минуту...")
	fmt.Printf("🔔 Буду отправлять уведомления за %d минут до пары\n", NotificationMinutes)
	fmt.Println("💡 Для остановки нажми Ctrl+C")

	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	// Обработка сигналов прерывания
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем опрос обновлений в отдельной горутине
	go func() {
		for {
			bot.PollUpdates()
			time.Sleep(1 * time.Second)
		}
	}()

	// Переменная для отслеживания последнего запуска парсера
	lastParserRun := time.Now().Add(-25 * time.Hour) // Инициализируем в прошлом

	for {
		select {
		case <-ticker.C:
			// Проверяем нужно ли запустить парсер (каждый день в 2:00)
			loc, _ := time.LoadLocation("Europe/Moscow")
			nowMoscow := time.Now().In(loc)

			// Если сейчас 2:00 и парсер не запускался сегодня
			if nowMoscow.Hour() == 2 && nowMoscow.Minute() == 0 {
				if time.Since(lastParserRun) > 23*time.Hour {
					fmt.Println("\n🔄 Запуск парсера для обновления расписания...")
					bot.UpdateSchedule()
					lastParserRun = time.Now()
					fmt.Println("✅ Расписание обновлено\n")
				}
			}

			bot.CheckAndSendNotifications()
		case <-sigChan:
			fmt.Println("\n\n⏹️  Бот остановлен")
			return
		}
	}
}

func (bot *TimetableBot) Run() {
	if err := bot.LoadSchedule("schedule.json"); err != nil {
		return
	}

	// Запускаем планировщик
	bot.RunScheduler()
}

func (bot *TimetableBot) PollUpdates() {
	endpoint := fmt.Sprintf("%s%s/getUpdates", TelegramAPIURL, bot.botToken)

	data := url.Values{}
	data.Set("offset", fmt.Sprintf("%d", bot.lastUpdateID+1))
	data.Set("timeout", "30")

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		fmt.Printf("⚠️ Ошибка получения обновлений: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var response UpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		fmt.Printf("⚠️ Ошибка декодирования обновлений: %v\n", err)
		return
	}

	if !response.Ok {
		return
	}

	for _, update := range response.Result {
		bot.lastUpdateID = update.UpdateID
		bot.HandleUpdate(update)
	}
}

func (bot *TimetableBot) HandleUpdate(update Update) {
	if update.Message.Text == "/start" {
		welcomeMsg := "👋 Привет! Я бот расписания МГУ ВШГА.\n\n" +
			"Я буду присылать уведомления за 15 минут до начала пар.\n" +
			"Расписание обновляется автоматически раз в 3 дня.\n\n" +
			"Твой ID: " + fmt.Sprintf("%d", update.Message.Chat.ID) + "\n" +
			"(Убедись, что этот ID прописан в config.py)"

		bot.SendMessageToChat(update.Message.Chat.ID, welcomeMsg)
	}
}

func (bot *TimetableBot) SendMessageToChat(chatID int64, message string) error {
	endpoint := fmt.Sprintf("%s%s/sendMessage", TelegramAPIURL, bot.botToken)

	data := url.Values{}
	data.Set("chat_id", fmt.Sprintf("%d", chatID))
	data.Set("text", message)
	data.Set("parse_mode", "HTML")

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		fmt.Printf("❌ Ошибка отправки: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	return nil
}

func main() {
	fmt.Println("⚙️  Загружаю конфигурацию...")

	config, err := LoadConfig()
	if err != nil {
		fmt.Printf("❌ Ошибка загрузки конфига: %v\n", err)
		os.Exit(1)
	}

	BotToken = config.BotToken
	UserID = config.UserID
	NotificationMinutes = config.NotificationMinutes

	if BotToken == "" || UserID == "" {
		fmt.Println("❌ config.py не заполнен!")
		fmt.Println("💡 Скопируй config.example.py -> config.py и заполни токен и ID")
		os.Exit(1)
	}

	bot := NewTimetableBot(BotToken, UserID)
	bot.Run()
}
