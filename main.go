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
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	// GIF_DISABLED: "bytes"
	// GIF_DISABLED: "io"
	// GIF_DISABLED: "math/rand"
	// GIF_DISABLED: "mime/multipart"
)

const (
	CheckInterval  = 1 * time.Minute
	TelegramAPIURL = "https://api.telegram.org/bot"
	AppVersion     = "schedule-watch-2026-10-26"

	// GIF_DISABLED: RegularGifDir = "assets/gif/regular"
	// GIF_DISABLED: MorningGifDir = "assets/gif/gm"
	// GIF_DISABLED: EveningGifDir = "assets/gif/evening"
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
	sentDailyGreetings        map[string]bool
	lastScheduleMessages      map[int64][]int
	lastUpdateID              int
	gifData                   GifData
	userStates                      map[int64]*UserState
	sendMessageFunc                 func(message string) error
	sendMessageToChatWithMarkupFunc func(chatID int64, message string, markup InlineKeyboardMarkup) (int, error)
	editMessageTextFunc             func(chatID int64, messageID int, text string, markup InlineKeyboardMarkup) error
}

type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID int        `json:"message_id"`
	Chat      Chat       `json:"chat"`
	Text      string     `json:"text"`
	Animation *MediaFile `json:"animation,omitempty"`
	Sticker   *MediaFile `json:"sticker,omitempty"`
	Video     *MediaFile `json:"video,omitempty"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data"`
}

type User struct {
	ID int64 `json:"id"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type UpdateResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

type TelegramMessageResponse struct {
	Ok     bool    `json:"ok"`
	Result Message `json:"result"`
}

func logDebug(format string, args ...interface{}) {
	fmt.Printf("🪲 "+format+"\n", args...)
}

func logInfo(format string, args ...interface{}) {
	fmt.Printf("ℹ️  "+format+"\n", args...)
}

func logError(format string, args ...interface{}) {
	fmt.Printf("❌ "+format+"\n", args...)
}

func LoadConfig() (Config, error) {
	logInfo("LoadConfig: пытаюсь прочитать config.json")
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		logDebug("LoadConfig: config.json не найден, пробую get_config.py")
		// Пытаемся загрузить из Python скрипта
		cmd := exec.Command("python3", "get_config.py")
		output, err := cmd.Output()
		if err != nil {
			logError("LoadConfig: get_config.py тоже не сработал: %v", err)
			return Config{}, fmt.Errorf("не могу загрузить конфиг: %v", err)
		}

		var config Config
		if err := json.Unmarshal(output, &config); err != nil {
			logError("LoadConfig: ошибка парсинга вывода get_config.py: %v", err)
			return Config{}, fmt.Errorf("ошибка парсинга конфига: %v", err)
		}

		logInfo("LoadConfig: конфиг загружен из get_config.py (BOT_TOKEN=%t, USER_ID=%s, NOTIFICATION_MINUTES=%d)", config.BotToken != "", config.UserID, config.NotificationMinutes)
		return config, nil
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		logError("LoadConfig: ошибка парсинга config.json: %v", err)
		return Config{}, fmt.Errorf("ошибка парсинга config.json: %v", err)
	}

	logInfo("LoadConfig: config.json загружен (BOT_TOKEN=%t, USER_ID=%s, NOTIFICATION_MINUTES=%d)", config.BotToken != "", config.UserID, config.NotificationMinutes)
	return config, nil
}

func NewTimetableBot(token, userID string) *TimetableBot {
	return &TimetableBot{
		botToken:                  token,
		userID:                    userID,
		schedule:                  []Lesson{},
		sentNotifications:         make(map[string]bool),
		sentDistanceNotifications: make(map[string]bool),
		sentDailyGreetings:        make(map[string]bool),
		lastScheduleMessages:      make(map[int64][]int),
		lastUpdateID:              0,
		gifData: GifData{
			Gifs: make(map[string]GifEntry),
			Settings: GifSettings{
				MorningTime: "09:00",
				EveningTime: "21:22",
			},
		},
		userStates: make(map[int64]*UserState),
	}
}

func (bot *TimetableBot) LoadSchedule(filename string) error {
	logInfo("LoadSchedule: загружаю %s", filename)

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		logError("LoadSchedule: файл %s не найден: %v", filename, err)
		fmt.Println("💡 Запусти сначала парсер: python parser_full.py")
		return err
	}
	logDebug("LoadSchedule: размер файла %s = %d байт", filename, len(data))

	err = json.Unmarshal(data, &bot.schedule)
	if err != nil {
		logError("LoadSchedule: ошибка парсинга JSON: %v", err)
		return err
	}

	logInfo("LoadSchedule: загружено %d пар", len(bot.schedule))
	if len(bot.schedule) > 0 {
		first := bot.schedule[0]
		last := bot.schedule[len(bot.schedule)-1]
		logDebug("LoadSchedule: первая пара=%s %s %s | последняя=%s %s %s", first.Date, first.TimeStart, first.Subject, last.Date, last.TimeStart, last.Subject)
	}
	return nil
}

func (bot *TimetableBot) SendMessage(message string) error {
	if bot.sendMessageFunc != nil {
		return bot.sendMessageFunc(message)
	}
	// GIF_DISABLED: was return bot.SendMessageWithAnimation(message, RegularGifDir)
	endpoint := fmt.Sprintf("%s%s/sendMessage", TelegramAPIURL, bot.botToken)
	logDebug("SendMessage: method=sendMessage chat_id=%s len(text)=%d", bot.userID, len(message))

	data := url.Values{}
	data.Set("chat_id", bot.userID)
	data.Set("text", message)
	data.Set("parse_mode", "HTML")

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		logError("SendMessage: ошибка отправки: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		logError("SendMessage: Telegram API вернул статус %d", resp.StatusCode)
		return fmt.Errorf("telegram error: %d: %s", resp.StatusCode, bodyString)
	}

	logDebug("SendMessage: текст успешно отправлен")
	return nil
}

/* GIF_DISABLED_START
   Чтобы вернуть гифки:
   1. Раскомментируй этот блок
   2. Верни imports: "bytes", "io", "math/rand", "mime/multipart"
   3. Верни константы: RegularGifDir, MorningGifDir, EveningGifDir
   4. Замени SendMessage обратно на: return bot.SendMessageWithAnimation(message, RegularGifDir)
   5. Верни вызовы SendMessageWithAnimation в CheckAndSendDailyGreetings
   6. Верни вызовы SendRandomAnimationToChat в SendScheduleToChat и SendMessageToChat

func (bot *TimetableBot) SendMessageWithAnimation(message, animationDir string) error {
	endpoint := fmt.Sprintf("%s%s/sendMessage", TelegramAPIURL, bot.botToken)
	logDebug("SendMessageWithAnimation: method=sendMessage chat_id=%s len(text)=%d animation_dir=%s", bot.userID, len(message), animationDir)

	if _, err := bot.SendRandomAnimationToChat(bot.userID, animationDir); err != nil {
		logError("SendMessageWithAnimation: GIF не отправился, продолжаю отправку текста: %v", err)
	}

	data := url.Values{}
	data.Set("chat_id", bot.userID)
	data.Set("text", message)
	data.Set("parse_mode", "HTML")

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		logError("SendMessageWithAnimation: ошибка отправки: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		logError("SendMessageWithAnimation: Telegram API вернул статус %d", resp.StatusCode)
		fmt.Printf("SendMessageWithAnimation response: %s\n", bodyString)
		fmt.Printf("SendMessageWithAnimation payload: %s\n", message)
		fmt.Printf("SendMessageWithAnimation chat_id: %s\n", bot.userID)
		return fmt.Errorf("telegram error: %d: %s", resp.StatusCode, bodyString)
	}

	logDebug("SendMessageWithAnimation: текст успешно отправлен")
	return nil
}

func (bot *TimetableBot) SendRandomAnimationToChat(chatID, dir string) (int, error) {
	animationPath, err := randomAnimationPath(dir)
	if err != nil {
		return 0, err
	}
	if animationPath == "" {
		logDebug("SendRandomAnimationToChat: нет GIF/MP4 файлов в %s, пропускаю", dir)
		return 0, nil
	}

	file, err := os.Open(animationPath)
	if err != nil {
		return 0, fmt.Errorf("не удалось открыть animation %s: %w", animationPath, err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("chat_id", chatID); err != nil {
		return 0, fmt.Errorf("не удалось записать chat_id в multipart: %w", err)
	}

	part, err := writer.CreateFormFile("animation", filepath.Base(animationPath))
	if err != nil {
		return 0, fmt.Errorf("не удалось создать multipart file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return 0, fmt.Errorf("не удалось записать animation в multipart: %w", err)
	}
	if err := writer.Close(); err != nil {
		return 0, fmt.Errorf("не удалось закрыть multipart writer: %w", err)
	}

	endpoint := fmt.Sprintf("%s%s/sendAnimation", TelegramAPIURL, bot.botToken)
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return 0, fmt.Errorf("не удалось создать sendAnimation request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ошибка sendAnimation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return 0, fmt.Errorf("sendAnimation telegram error: %s body=%s file=%s", resp.Status, string(bodyBytes), animationPath)
	}

	var response TelegramMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, fmt.Errorf("ошибка декодирования sendAnimation response: %w", err)
	}
	if !response.Ok {
		return 0, fmt.Errorf("sendAnimation response ok=false file=%s", animationPath)
	}

	logInfo("SendRandomAnimationToChat: отправлен GIF/MP4 %s chat_id=%s message_id=%d", animationPath, chatID, response.Result.MessageID)
	return response.Result.MessageID, nil
}

func randomAnimationPath(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("не удалось прочитать папку %s: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".gif" || ext == ".mp4" {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	if len(files) == 0 {
		return "", nil
	}

	return files[rand.Intn(len(files))], nil
}
GIF_DISABLED_END */

// escapeHTML экранирует специальные HTML символы для Telegram API
func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func (bot *TimetableBot) FormatNotification(lesson *Lesson) string {
	message := fmt.Sprintf(
		"🔔 <b>Скоро пара!</b>\n\n"+
			"📚 <b>Предмет:</b> %s\n"+
			"👨‍🏫 <b>Преподаватель:</b> %s\n"+
			"🚪 <b>Аудитория:</b> %s\n\n"+
			"🕐 <b>Время:</b> %s - %s\n"+
			"📅 <b>Дата:</b> %s (%s)",
		escapeHTML(lesson.Subject),
		escapeHTML(lesson.Teacher),
		escapeHTML(lesson.Room),
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

func moscowLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.Local
	}
	return loc
}

func (bot *TimetableBot) GetLessonsForDate(dateStr string) []Lesson {
	lessons := []Lesson{}
	for _, lesson := range bot.schedule {
		if lesson.Date == dateStr {
			lessons = append(lessons, lesson)
		}
	}

	sort.SliceStable(lessons, func(i, j int) bool {
		left, leftErr := ParseTime(lessons[i].Date, lessons[i].TimeStart)
		right, rightErr := ParseTime(lessons[j].Date, lessons[j].TimeStart)
		if leftErr != nil || rightErr != nil {
			return lessons[i].TimeStart < lessons[j].TimeStart
		}
		return left.Before(right)
	})

	return lessons
}

func (bot *TimetableBot) FindNextLesson(after time.Time) (Lesson, time.Time, bool) {
	loc := moscowLocation()
	after = after.In(loc)

	var next Lesson
	var nextStart time.Time
	found := false

	for _, lesson := range bot.schedule {
		start, err := ParseTime(lesson.Date, lesson.TimeStart)
		if err != nil || !start.After(after) {
			continue
		}

		if !found || start.Before(nextStart) {
			next = lesson
			nextStart = start
			found = true
		}
	}

	return next, nextStart, found
}

func lessonMode(room string) string {
	if isDistanceLearning(room) {
		return "дистант"
	}
	return "очная"
}

func lessonCountWord(count int) string {
	mod10 := count % 10
	mod100 := count % 100
	if mod10 == 1 && mod100 != 11 {
		return "пара"
	}
	if mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14) {
		return "пары"
	}
	return "пар"
}

func roomText(room string) string {
	room = strings.TrimSpace(room)
	if room == "" {
		return "не указана"
	}
	return room
}

func joinTimes(times []string) string {
	switch len(times) {
	case 0:
		return ""
	case 1:
		return times[0]
	case 2:
		return times[0] + " и " + times[1]
	default:
		return strings.Join(times[:len(times)-1], ", ") + " и " + times[len(times)-1]
	}
}

func formatDurationLeft(duration time.Duration) string {
	if duration < time.Minute {
		return "меньше минуты"
	}

	totalMinutes := int(duration.Round(time.Minute).Minutes())
	days := totalMinutes / (24 * 60)
	totalMinutes %= 24 * 60
	hours := totalMinutes / 60
	minutes := totalMinutes % 60

	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d д", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d ч", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d мин", minutes))
	}

	if len(parts) > 2 {
		parts = parts[:2]
	}

	return strings.Join(parts, " ")
}

func (bot *TimetableBot) BuildNextLessonMessage(now time.Time) string {
	lesson, start, found := bot.FindNextLesson(now)
	if !found {
		return "Ближайших пар не нашел."
	}

	left := formatDurationLeft(start.Sub(now.In(moscowLocation())))
	return fmt.Sprintf(
		"⏭ <b>Ближайшая пара</b>\n\n"+
			"📚 %s\n"+
			"🕐 %s-%s\n"+
			"🚪 %s\n"+
			"📍 %s\n"+
			"⏳ Осталось: %s\n"+
			"📅 %s (%s)",
		escapeHTML(lesson.Subject),
		lesson.TimeStart,
		lesson.TimeEnd,
		escapeHTML(roomText(lesson.Room)),
		lessonMode(lesson.Room),
		left,
		lesson.Date,
		lesson.Weekday,
	)
}

func (bot *TimetableBot) BuildMorningSummary(date time.Time) string {
	dateStr := date.In(moscowLocation()).Format("02.01.2006")
	lessons := bot.GetLessonsForDate(dateStr)
	if len(lessons) == 0 {
		return "☀️ <b>Доброе утро</b>\n\nСегодня пар нет, можно выдохнуть."
	}

	first := lessons[0]
	return fmt.Sprintf(
		"☀️ <b>Доброе утро</b>\n\nСегодня %d %s, первая в %s, %s.",
		len(lessons),
		lessonCountWord(len(lessons)),
		first.TimeStart,
		lessonMode(first.Room),
	)
}

func (bot *TimetableBot) BuildEveningSummary(date time.Time) string {
	loc := moscowLocation()
	tomorrow := date.In(loc).AddDate(0, 0, 1)
	lessons := bot.GetLessonsForDate(tomorrow.Format("02.01.2006"))
	if len(lessons) == 0 {
		return "🌙 <b>Завтра</b>\n\nЗавтра пар нет, можно выдохнуть."
	}

	times := make([]string, 0, len(lessons))
	for _, lesson := range lessons {
		times = append(times, lesson.TimeStart)
	}

	first := lessons[0]
	return fmt.Sprintf(
		"🌙 <b>Завтра</b>\n\nЗавтра %d %s: %s. Первая аудитория: %s.",
		len(lessons),
		lessonCountWord(len(lessons)),
		joinTimes(times),
		escapeHTML(roomText(first.Room)),
	)
}

func (bot *TimetableBot) GetUpcomingLessons() []Lesson {
	// Используем московское время (Europe/Moscow)
	loc := moscowLocation()
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

// SendMorningReminder отправляет утреннее напоминание со всеми парами на день
func (bot *TimetableBot) SendMorningReminder(date string, lessons []Lesson, distanceDay bool) {
	logInfo("SendMorningReminder: date=%s lessons=%d distance_day=%t already_sent=%t", date, len(lessons), distanceDay, bot.sentDistanceNotifications[date])
	if bot.sentDistanceNotifications[date] {
		logDebug("SendMorningReminder: пропуск, уже отправляли на %s", date)
		return
	}

	hasDistance, hasInPerson := false, false
	for _, l := range lessons {
		if isDistanceLearning(l.Room) {
			hasDistance = true
		} else {
			hasInPerson = true
		}
	}

	header := "У вас сегодня очные пары:"
	switch {
	case distanceDay:
		header = "У вас сегодня дистанционные пары:"
	case hasDistance && hasInPerson:
		header = "У вас сегодня пары (есть и дистанционные):"
	}

	message := "📱 <b>Утреннее напоминание</b>\n\n" + header + "\n\n"
	for _, lesson := range lessons {
		message += fmt.Sprintf(
			"• %s пара (%s-%s)\n  📚 %s\n",
			lesson.LessonNumber,
			lesson.TimeStart,
			lesson.TimeEnd,
			escapeHTML(lesson.Subject),
		)
		if lesson.Teacher != "" {
			message += fmt.Sprintf("  👨‍🏫 %s\n", escapeHTML(lesson.Teacher))
		}
		if !distanceDay && strings.TrimSpace(lesson.Room) != "" {
			message += fmt.Sprintf("  🚪 %s\n", escapeHTML(lesson.Room))
		}
	}

	if err := bot.SendMessage(message); err == nil {
		logInfo("SendMorningReminder: отправлено на %s", date)
		bot.sentDistanceNotifications[date] = true
	} else {
		logError("SendMorningReminder: ошибка отправки на %s: %v", date, err)
	}
}

// CheckAndSendMorningReminder решает, нужно ли утреннее напоминание (вызывается в 08:00 МСК)
func (bot *TimetableBot) CheckAndSendMorningReminder(now time.Time) {
	today := now.Format("02.01.2006")
	lessons := bot.GetLessonsForDate(today)
	if len(lessons) == 0 {
		logDebug("CheckAndSendMorningReminder: сегодня пар нет")
		return
	}

	distanceDay := !bot.HasInPersonLessonsForDate(today)
	settings := bot.gifData.Settings
	if distanceDay && !settings.RemindDistanceEnabled() {
		logDebug("CheckAndSendMorningReminder: дистант-день, напоминание выключено в настройках")
		return
	}
	if !distanceDay && !settings.RemindInPersonEnabled() {
		logDebug("CheckAndSendMorningReminder: очный день, напоминание выключено в настройках")
		return
	}
	bot.SendMorningReminder(today, lessons, distanceDay)
}

// HasInPersonLessonsForDate проверяет есть ли очные пары в указанную дату
func (bot *TimetableBot) HasInPersonLessonsForDate(date string) bool {
	for _, lesson := range bot.schedule {
		if lesson.Date == date && !isDistanceLearning(lesson.Room) {
			return true
		}
	}
	return false
}

// HasInPersonLessonsToday проверяет есть ли очные пары сегодня
func (bot *TimetableBot) HasInPersonLessonsToday() bool {
	loc, _ := time.LoadLocation("Europe/Moscow")
	today := time.Now().In(loc).Format("02.01.2006")
	return bot.HasInPersonLessonsForDate(today)
}

func (bot *TimetableBot) CheckAndSendNotifications() {
	now := time.Now()
	upcoming := bot.GetUpcomingLessons()
	logDebug("CheckAndSendNotifications: now=%s upcoming=%d sent=%d", now.Format(time.RFC3339), len(upcoming), len(bot.sentNotifications))

	// Очищаем просроченные состояния пользователей
	bot.CleanExpiredStates()

	loc, _ := time.LoadLocation("Europe/Moscow")
	nowInMoscow := now.In(loc)
	bot.CheckAndSendDailyGreetings(nowInMoscow)

	// Утреннее напоминание о парах (настраивается в /settings)
	if nowInMoscow.Hour() == MorningReminderHour && nowInMoscow.Minute() == MorningReminderMinute {
		logInfo("CheckAndSendNotifications: время утреннего напоминания")
		bot.CheckAndSendMorningReminder(nowInMoscow)
	}

	// Отправляем обычные уведомления за 30 минут
	for _, lesson := range upcoming {
		logDebug("CheckAndSendNotifications: candidate lesson=%s date=%s start=%s notify_at=%s", lesson.Subject, lesson.Date, lesson.TimeStart, lesson.Notification.Format(time.RFC3339))
		// Если есть очные пары сегодня - отправляем уведомления для ВСЕХ пар (включая дистанционные)
		// Если НЕТ очных пар - пропускаем дистанционные (они уже получили утреннее уведомление)
		if !bot.HasInPersonLessonsToday() && isDistanceLearning(lesson.Room) {
			logDebug("CheckAndSendNotifications: skip distance lesson=%s date=%s", lesson.Subject, lesson.Date)
			continue
		}

		lessonKey := fmt.Sprintf("%s_%s_%s", lesson.Date, lesson.LessonNumber, lesson.Subject)

		if bot.sentNotifications[lessonKey] {
			logDebug("CheckAndSendNotifications: already sent lessonKey=%s", lessonKey)
			continue
		}

		timeDiff := lesson.Notification.Sub(now).Seconds()

		// Отправляем если осталось меньше 60 секунд
		if timeDiff >= 0 && timeDiff <= 60 {
			logInfo("CheckAndSendNotifications: sending lessonKey=%s timeDiff=%.1fs", lessonKey, timeDiff)

			// Гифка перед уведомлением
			bot.SendGifForSlot(bot.userID, SlotNotification)

			message := bot.FormatNotification(&lesson)
			err := bot.SendMessage(message)
			if err == nil {
				logInfo("CheckAndSendNotifications: отправлено уведомление: %s (%s %s)",
					lesson.Subject, lesson.Date, lesson.TimeStart)
				bot.sentNotifications[lessonKey] = true
			} else {
				logError("CheckAndSendNotifications: ошибка отправки уведомления lessonKey=%s: %v", lessonKey, err)
			}
		}
	}
}

func (bot *TimetableBot) CheckAndSendDailyGreetings(now time.Time) {
	date := now.Format("02.01.2006")

	morningH, morningM := ParseSettingsTime(bot.gifData.Settings.MorningTime)
	eveningH, eveningM := ParseSettingsTime(bot.gifData.Settings.EveningTime)

	if now.Hour() == morningH && now.Minute() == morningM {
		key := "gm_" + date
		if bot.sentDailyGreetings[key] {
			logDebug("CheckAndSendDailyGreetings: morning already sent key=%s", key)
			return
		}

		logInfo("CheckAndSendDailyGreetings: отправляю доброе утро date=%s time=%s", date, bot.gifData.Settings.MorningTime)

		// Гифка перед утренним сообщением
		bot.SendGifForSlot(bot.userID, SlotMorning)

		message := bot.BuildMorningSummary(now)
		if err := bot.SendMessage(message); err != nil {
			logError("CheckAndSendDailyGreetings: ошибка утреннего сообщения date=%s: %v", date, err)
			return
		}
		bot.sentDailyGreetings[key] = true
		return
	}

	if now.Hour() == eveningH && now.Minute() == eveningM {
		key := "evening_" + date
		if bot.sentDailyGreetings[key] {
			logDebug("CheckAndSendDailyGreetings: evening already sent key=%s", key)
			return
		}

		logInfo("CheckAndSendDailyGreetings: отправляю вечерний summary date=%s time=%s", date, bot.gifData.Settings.EveningTime)

		// Гифка перед вечерним сообщением
		bot.SendGifForSlot(bot.userID, SlotEvening)

		message := bot.BuildEveningSummary(now)
		if err := bot.SendMessage(message); err != nil {
			logError("CheckAndSendDailyGreetings: ошибка вечернего сообщения date=%s: %v", date, err)
			return
		}
		bot.sentDailyGreetings[key] = true
	}
}

func (bot *TimetableBot) RunScheduler() {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("🤖 БОТ ЗАПУЩЕН")
	fmt.Println(strings.Repeat("=", 60))
	logInfo("RunScheduler: version=%s pid=%d cwd=%s exe=%s", AppVersion, os.Getpid(), mustGetwd(), mustExecutable())
	logInfo("RunScheduler: schedule_len=%d user_id=%s notification_minutes=%d", len(bot.schedule), bot.userID, NotificationMinutes)
	logInfo("RunScheduler: polling every %s", CheckInterval)
	logInfo("RunScheduler: notification window=%d minutes", NotificationMinutes)
	fmt.Println("💡 Для остановки нажми Ctrl+C")

	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	// Обработка сигналов прерывания
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем опрос обновлений в отдельной горутине
	go func() {
		for {
			logDebug("RunScheduler: PollUpdates tick")
			bot.PollUpdates()
			time.Sleep(1 * time.Second)
		}
	}()

	// Время последней проверки расписания (нулевое — значит проверить сразу после старта)
	var lastRefresh time.Time

	for {
		select {
		case <-ticker.C:
			logDebug("RunScheduler: minute tick")
			nowMoscow := time.Now().In(moscowLocation())
			logDebug("RunScheduler: Moscow time=%s", nowMoscow.Format(time.RFC3339))

			// Постоянная индексация: днём раз в 15 минут, ночью раз в час.
			// Выполняется в отдельной горутине, чтобы не задерживать уведомления.
			if shouldRefreshSchedule(nowMoscow, lastRefresh) {
				lastRefresh = time.Now()
				logInfo("RunScheduler: проверяю расписание на изменения")
				go bot.UpdateSchedule()
			}

			bot.CheckAndSendNotifications()
		case <-sigChan:
			fmt.Println("\n\n⏹️  Бот остановлен")
			logInfo("RunScheduler: получен сигнал остановки")
			return
		}
	}
}

func (bot *TimetableBot) Run() {
	logInfo("Run: стартую загрузку расписания")
	if err := bot.LoadSchedule("schedule.json"); err != nil {
		logError("Run: не удалось загрузить расписание: %v", err)
		return
	}

	// Загружаем данные гифок
	if err := bot.LoadGifData(); err != nil {
		logError("Run: не удалось загрузить gif data: %v", err)
		// Не фатально — продолжаем с пустыми гифками
	}

	// Уведомление о запуске
	startMsg := fmt.Sprintf(
		"🤖 <b>Бот запущен</b>\n\n"+
			"✅ Расписание: <b>%d пар</b>\n"+
			"🔔 Уведомления: за <b>%d мин</b> до пары\n"+
			"🖼 Гифки: <b>%d шт</b>\n"+
			"🌅 Утро: <b>%s</b> · 🌙 Вечер: <b>%s</b>\n"+
			"📦 Версия: <code>%s</code>\n\n"+
			"📋 <b>Команды:</b>\n"+
			"/schedule — расписание на день\n"+
			"/next — ближайшая пара\n"+
			"/gifs — управление гифками\n"+
			"/settings — настройки времени",
		len(bot.schedule),
		NotificationMinutes,
		len(bot.gifData.Gifs),
		bot.gifData.Settings.MorningTime,
		bot.gifData.Settings.EveningTime,
		AppVersion,
	)
	if err := bot.SendMessage(startMsg); err != nil {
		logError("Run: не удалось отправить стартовое сообщение: %v", err)
	} else {
		logInfo("Run: стартовое сообщение отправлено")
	}

	// Запускаем планировщик
	bot.RunScheduler()
}

func (bot *TimetableBot) PollUpdates() {
	endpoint := fmt.Sprintf("%s%s/getUpdates", TelegramAPIURL, bot.botToken)
	logDebug("PollUpdates: method=getUpdates offset=%d", bot.lastUpdateID+1)

	data := url.Values{}
	data.Set("offset", fmt.Sprintf("%d", bot.lastUpdateID+1))
	data.Set("timeout", "30")

	// Отправляем GET вместо POST, чтобы избежать проблем с заголовками,
	// или явно указываем Content-Type: application/x-www-form-urlencoded
	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		logError("PollUpdates: ошибка получения обновлений: %v", err)
		return
	}
	defer resp.Body.Close()
	logDebug("PollUpdates: HTTP status=%s", resp.Status)
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logError("PollUpdates: Telegram вернул %s body=%s", resp.Status, string(bodyBytes))
		return
	}

	var response UpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logError("PollUpdates: ошибка декодирования обновлений: %v", err)
		return
	}

	if !response.Ok {
		logError("PollUpdates: Telegram response ok=false")
		return
	}
	logDebug("PollUpdates: получено %d updates", len(response.Result))

	for _, update := range response.Result {
		logDebug("PollUpdates: update_id=%d has_message=%t has_callback=%t", update.UpdateID, update.Message != nil, update.CallbackQuery != nil)
		bot.lastUpdateID = update.UpdateID
		bot.HandleUpdate(update)
	}
}

func (bot *TimetableBot) HandleUpdate(update Update) {
	if update.CallbackQuery != nil {
		logInfo("HandleUpdate: callback_query id=%s from=%d data=%s", update.CallbackQuery.ID, update.CallbackQuery.From.ID, update.CallbackQuery.Data)
		bot.HandleCallbackQuery(update.CallbackQuery)
		return
	}

	if update.Message == nil {
		logDebug("HandleUpdate: пустое update без message и callback")
		return
	}

	msg := update.Message
	chatID := msg.Chat.ID

	// Логируем входящее сообщение
	logInfo("HandleUpdate: message_id=%d chat_id=%d text=%q has_animation=%t has_sticker=%t has_video=%t",
		msg.MessageID, chatID, msg.Text,
		msg.Animation != nil, msg.Sticker != nil, msg.Video != nil)

	// 1. Если пришло медиа и пользователь ждёт гифку — обрабатываем
	if fileID, fileType := ExtractMediaInfo(msg); fileID != "" {
		state := bot.GetUserState(chatID)
		if state != nil && state.State == "waiting_media" {
			bot.HandleMediaMessage(chatID, fileID, fileType)
			return
		}
		// Медиа вне контекста добавления — игнорируем
	}

	// 2. Команды (начинаются с /). Отменяют любое активное состояние.
	if strings.HasPrefix(msg.Text, "/") {
		if bot.GetUserState(chatID) != nil {
			bot.ClearUserState(chatID)
		}

		switch msg.Text {
		case "/start":
			welcomeMsg := "👋 <b>Привет!</b> Я бот расписания МГУ ВШГА.\n\n" +
				"📋 <b>Что я умею:</b>\n" +
				"🔔 Уведомления за " + fmt.Sprintf("%d", NotificationMinutes) + " мин до пары\n" +
				"📚 /schedule — расписание на день\n" +
				"⏭ /next — ближайшая пара\n" +
				"🖼 /gifs — персональные гифки\n" +
				"   <i>(добавляйте гифки, стикеры, видео)</i>\n" +
				"⚙️ /settings — настройки времени\n" +
				"   <i>(утренние и вечерние сообщения)</i>\n\n" +
				"Твой ID: <code>" + fmt.Sprintf("%d", chatID) + "</code>\n" +
				"<i>(Убедись, что этот ID прописан в config.json)</i>"

			logInfo("HandleUpdate: отправляю /start ответ chat_id=%d", chatID)
			bot.SendMessageToChat(chatID, welcomeMsg)

		case "/schedule":
			loc, _ := time.LoadLocation("Europe/Moscow")
			today := time.Now().In(loc)
			logInfo("HandleUpdate: /schedule requested chat_id=%d today=%s", chatID, today.Format("02.01.2006"))
			text, markup := bot.BuildScheduleMessageAndMarkup(today)

			logDebug("HandleUpdate: /schedule response text_len=%d", len(text))
			if err := bot.SendScheduleToChat(chatID, msg.MessageID, text, markup); err != nil {
				logError("HandleUpdate: /schedule send failed chat_id=%d: %v", chatID, err)
			}

		case "/next":
			loc := moscowLocation()
			now := time.Now().In(loc)
			logInfo("HandleUpdate: /next requested chat_id=%d now=%s", chatID, now.Format(time.RFC3339))

			// Гифка перед ответом
			bot.SendGifForSlot(fmt.Sprintf("%d", chatID), SlotCommands)

			text := bot.BuildNextLessonMessage(now)
			if err := bot.SendMessageToChat(chatID, text); err != nil {
				logError("HandleUpdate: /next send failed chat_id=%d: %v", chatID, err)
			}

		case "/gifs":
			bot.HandleGifsCommand(chatID)

		case "/settings":
			bot.HandleSettingsCommand(chatID)

		default:
			logDebug("HandleUpdate: unknown command=%q", msg.Text)
		}
		return
	}

	// 3. Текстовое сообщение — проверяем state machine (ожидание имени гифки)
	if msg.Text != "" {
		if bot.HandleGifNameMessage(chatID, msg.Text) {
			return
		}
	}

	// 4. Если пользователь в состоянии waiting_media но прислал текст — подсказываем
	state := bot.GetUserState(chatID)
	if state != nil && state.State == "waiting_media" {
		bot.SendMessageToChat(chatID, "📸 Отправьте гифку, стикер или короткое видео.\nИли нажмите ❌ Отмена.")
		return
	}

	// 5. Остальные сообщения игнорируем
	logDebug("HandleUpdate: message ignored text=%q", msg.Text)
}

func (bot *TimetableBot) HandleCallbackQuery(query *CallbackQuery) {
	logInfo("HandleCallbackQuery: callback_id=%s from=%d data=%s message_id=%d", query.ID, query.From.ID, query.Data, func() int {
		if query.Message != nil {
			return query.Message.MessageID
		}
		return 0
	}())
	// Сразу отвечаем, чтобы убрать иконку загрузки
	bot.AnswerCallbackQuery(query.ID)

	if strings.HasPrefix(query.Data, "g:") {
		bot.HandleGifCallback(query)
	} else if strings.HasPrefix(query.Data, "s:") {
		bot.HandleSettingsCallback(query)
	} else if strings.HasPrefix(query.Data, "date:") {
		dateStr := strings.TrimPrefix(query.Data, "date:")
		loc, _ := time.LoadLocation("Europe/Moscow")
		logDebug("HandleCallbackQuery: parsed date string=%s", dateStr)

		// Парсим дату из callback
		parsedDate, err := time.ParseInLocation("02.01.2006", dateStr, loc)
		if err != nil {
			logError("HandleCallbackQuery: ошибка парсинга даты из callback: %v", err)
			return
		}

		text, markup := bot.BuildScheduleMessageAndMarkup(parsedDate)
		logInfo("HandleCallbackQuery: rebuilding schedule for %s text_len=%d", parsedDate.Format("02.01.2006"), len(text))

		if query.Message != nil {
			if err := bot.EditMessageText(query.Message.Chat.ID, query.Message.MessageID, text, markup); err != nil {
				logError("HandleCallbackQuery: editMessageText failed: %v", err)
			}
		} else {
			logError("HandleCallbackQuery: query.Message=nil, нечего редактировать")
		}
	} else {
		logDebug("HandleCallbackQuery: unsupported callback data=%s", query.Data)
	}
}

func (bot *TimetableBot) AnswerCallbackQuery(callbackQueryID string) {
	endpoint := fmt.Sprintf("%s%s/answerCallbackQuery", TelegramAPIURL, bot.botToken)
	logDebug("AnswerCallbackQuery: callback_query_id=%s", callbackQueryID)
	data := url.Values{}
	data.Set("callback_query_id", callbackQueryID)
	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		logError("AnswerCallbackQuery: error=%v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logError("AnswerCallbackQuery: status=%s body=%s", resp.Status, string(bodyBytes))
		return
	}
	logDebug("AnswerCallbackQuery: ok")
}

func (bot *TimetableBot) BuildScheduleMessageAndMarkup(date time.Time) (string, InlineKeyboardMarkup) {
	dateStr := date.Format("02.01.2006")
	logDebug("BuildScheduleMessageAndMarkup: date=%s schedule_len=%d", dateStr, len(bot.schedule))

	// Ищем пары на эту дату
	var lessonsForDay []Lesson
	for _, l := range bot.schedule {
		if l.Date == dateStr {
			lessonsForDay = append(lessonsForDay, l)
		}
	}

	logInfo("BuildScheduleMessageAndMarkup: date=%s lessons=%d", dateStr, len(lessonsForDay))
	if len(lessonsForDay) > 0 {
		for i, l := range lessonsForDay {
			logDebug("BuildScheduleMessageAndMarkup: lesson[%d]=%s %s %s room=%s teacher=%s", i, l.LessonNumber, l.TimeStart, l.Subject, l.Room, l.Teacher)
		}
	}

	// Формируем текст
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📅 <b>Расписание на %s</b>\n\n", dateStr))

	if len(lessonsForDay) == 0 {
		b.WriteString("<i>Пар нет! Можно отдыхать 🛌</i>")
	} else {
		for _, l := range lessonsForDay {
			b.WriteString(fmt.Sprintf("<b>%s пара</b> (%s-%s)\n", l.LessonNumber, l.TimeStart, l.TimeEnd))
			b.WriteString(fmt.Sprintf("📚 %s\n", escapeHTML(l.Subject)))
			if l.Teacher != "" {
				b.WriteString(fmt.Sprintf("👨‍🏫 %s\n", escapeHTML(l.Teacher)))
			}
			b.WriteString(fmt.Sprintf("🚪 %s\n\n", escapeHTML(l.Room)))
		}
	}

	// Формируем клавиатуру
	yesterday := date.AddDate(0, 0, -1).Format("02.01.2006")
	tomorrow := date.AddDate(0, 0, 1).Format("02.01.2006")

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "⬅️ Назад", CallbackData: "date:" + yesterday},
				{Text: "Вперед ➡️", CallbackData: "date:" + tomorrow},
			},
		},
	}

	return b.String(), markup
}

func (bot *TimetableBot) EditMessageText(chatID int64, messageID int, text string, markup InlineKeyboardMarkup) error {
	if bot.editMessageTextFunc != nil {
		return bot.editMessageTextFunc(chatID, messageID, text, markup)
	}
	endpoint := fmt.Sprintf("%s%s/editMessageText", TelegramAPIURL, bot.botToken)
	logDebug("EditMessageText: chat_id=%d message_id=%d text_len=%d", chatID, messageID, len(text))

	data := url.Values{}
	data.Set("chat_id", fmt.Sprintf("%d", chatID))
	data.Set("message_id", fmt.Sprintf("%d", messageID))
	data.Set("text", text)
	data.Set("parse_mode", "HTML")

	markupBytes, err := json.Marshal(markup)
	if err == nil {
		data.Set("reply_markup", string(markupBytes))
		logDebug("EditMessageText: reply_markup=%s", string(markupBytes))
	} else {
		logError("EditMessageText: marshal reply_markup failed: %v", err)
	}

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		logError("EditMessageText: ошибка редактирования сообщения: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logError("EditMessageText: Telegram status=%s body=%s", resp.Status, string(bodyBytes))
		return fmt.Errorf("editMessageText telegram error: %s", resp.Status)
	}
	logInfo("EditMessageText: ok chat_id=%d message_id=%d", chatID, messageID)
	return nil
}

func (bot *TimetableBot) SendScheduleToChat(chatID int64, commandMessageID int, message string, markup InlineKeyboardMarkup) error {
	bot.DeletePreviousScheduleMessages(chatID)
	if commandMessageID != 0 {
		if err := bot.DeleteMessage(chatID, commandMessageID); err != nil {
			logError("SendScheduleToChat: не удалось удалить команду /schedule chat_id=%d message_id=%d: %v", chatID, commandMessageID, err)
		}
	}

	messageIDs := []int{}

	// Отправляем гифку перед расписанием (слот "commands")
	gifMsgID := bot.SendGifForSlot(fmt.Sprintf("%d", chatID), SlotCommands)
	if gifMsgID != 0 {
		messageIDs = append(messageIDs, gifMsgID)
	}

	messageID, err := bot.SendMessageToChatWithMarkup(chatID, message, markup)
	if err != nil {
		if len(messageIDs) > 0 {
			bot.lastScheduleMessages[chatID] = messageIDs
		}
		return err
	}
	if messageID != 0 {
		messageIDs = append(messageIDs, messageID)
	}

	if len(messageIDs) > 0 {
		bot.lastScheduleMessages[chatID] = messageIDs
	}
	return nil
}

func (bot *TimetableBot) DeletePreviousScheduleMessages(chatID int64) {
	messageIDs := bot.lastScheduleMessages[chatID]
	if len(messageIDs) == 0 {
		return
	}

	logInfo("DeletePreviousScheduleMessages: удаляю %d старых schedule сообщений chat_id=%d", len(messageIDs), chatID)
	for _, messageID := range messageIDs {
		if messageID == 0 {
			continue
		}
		if err := bot.DeleteMessage(chatID, messageID); err != nil {
			logError("DeletePreviousScheduleMessages: не удалось удалить chat_id=%d message_id=%d: %v", chatID, messageID, err)
		}
	}
	delete(bot.lastScheduleMessages, chatID)
}

func (bot *TimetableBot) DeleteMessage(chatID int64, messageID int) error {
	endpoint := fmt.Sprintf("%s%s/deleteMessage", TelegramAPIURL, bot.botToken)
	logDebug("DeleteMessage: chat_id=%d message_id=%d", chatID, messageID)

	data := url.Values{}
	data.Set("chat_id", fmt.Sprintf("%d", chatID))
	data.Set("message_id", fmt.Sprintf("%d", messageID))

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		return fmt.Errorf("ошибка deleteMessage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("deleteMessage telegram error: %s body=%s", resp.Status, string(bodyBytes))
	}

	logDebug("DeleteMessage: ok chat_id=%d message_id=%d", chatID, messageID)
	return nil
}

func (bot *TimetableBot) SendMessageToChatWithMarkup(chatID int64, message string, markup InlineKeyboardMarkup) (int, error) {
	if bot.sendMessageToChatWithMarkupFunc != nil {
		return bot.sendMessageToChatWithMarkupFunc(chatID, message, markup)
	}
	endpoint := fmt.Sprintf("%s%s/sendMessage", TelegramAPIURL, bot.botToken)
	logDebug("SendMessageToChatWithMarkup: chat_id=%d text_len=%d", chatID, len(message))

	data := url.Values{}
	data.Set("chat_id", fmt.Sprintf("%d", chatID))
	data.Set("text", message)
	data.Set("parse_mode", "HTML")

	markupBytes, err := json.Marshal(markup)
	if err == nil {
		data.Set("reply_markup", string(markupBytes))
		logDebug("SendMessageToChatWithMarkup: reply_markup=%s", string(markupBytes))
	} else {
		logError("SendMessageToChatWithMarkup: marshal reply_markup failed: %v", err)
	}

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		logError("SendMessageToChatWithMarkup: ошибка отправки с кнопками: %v", err)
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logError("SendMessageToChatWithMarkup: Telegram status=%s body=%s", resp.Status, string(bodyBytes))
		return 0, fmt.Errorf("sendMessage telegram error: %s", resp.Status)
	}

	var response TelegramMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, fmt.Errorf("ошибка декодирования sendMessage response: %w", err)
	}
	if !response.Ok {
		return 0, fmt.Errorf("sendMessage response ok=false")
	}
	logInfo("SendMessageToChatWithMarkup: ok chat_id=%d message_id=%d", chatID, response.Result.MessageID)

	return response.Result.MessageID, nil
}

func (bot *TimetableBot) SendMessageToChat(chatID int64, message string) error {
	endpoint := fmt.Sprintf("%s%s/sendMessage", TelegramAPIURL, bot.botToken)
	logDebug("SendMessageToChat: chat_id=%d text_len=%d", chatID, len(message))

	data := url.Values{}
	data.Set("chat_id", fmt.Sprintf("%d", chatID))
	data.Set("text", message)
	data.Set("parse_mode", "HTML")

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		logError("SendMessageToChat: ошибка отправки: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logError("SendMessageToChat: Telegram status=%s body=%s", resp.Status, string(bodyBytes))
		return fmt.Errorf("sendMessage telegram error: %s", resp.Status)
	}
	logInfo("SendMessageToChat: ok chat_id=%d", chatID)

	return nil
}

func main() {
	fmt.Println("⚙️  Загружаю конфигурацию...")
	logInfo("main: version=%s pid=%d cwd=%s exe=%s", AppVersion, os.Getpid(), mustGetwd(), mustExecutable())

	config, err := LoadConfig()
	if err != nil {
		logError("main: ошибка загрузки конфига: %v", err)
		os.Exit(1)
	}

	BotToken = config.BotToken
	UserID = config.UserID
	NotificationMinutes = config.NotificationMinutes

	if BotToken == "" || UserID == "" {
		logError("main: config.py/config.json не заполнен")
		fmt.Println("💡 Скопируй config.example.py -> config.py и заполни токен и ID")
		os.Exit(1)
	}

	bot := NewTimetableBot(BotToken, UserID)
	logInfo("main: bot created user_id=%s schedule_len=%d", UserID, len(bot.schedule))
	bot.Run()
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("<cwd error: %v>", err)
	}
	return wd
}

func mustExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Sprintf("<exe error: %v>", err)
	}
	if rel, err := filepath.EvalSymlinks(exe); err == nil {
		return rel
	}
	return exe
}
