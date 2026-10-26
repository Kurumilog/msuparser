package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================
// Типы данных
// ============================================================

// GifEntry — одна сохранённая гифка/стикер/видео
type GifEntry struct {
	FileID   string   `json:"file_id"`
	FileType string   `json:"file_type"` // "animation", "sticker", "video"
	Slots    []string `json:"slots"`     // "morning", "evening", "notification", "commands"
}

// GifSettings — пользовательские настройки времени
type GifSettings struct {
	MorningTime string `json:"morning_time"` // формат "HH:MM", default "09:00"
	EveningTime string `json:"evening_time"` // формат "HH:MM", default "21:22"

	// Утреннее напоминание о парах в 08:00. nil = значение по умолчанию
	// (дистант — включено, как было раньше; очно — выключено).
	RemindDistance *bool `json:"remind_distance,omitempty"`
	RemindInPerson *bool `json:"remind_in_person,omitempty"`
}

// MorningReminderHour/Minute — время утреннего напоминания о парах
const (
	MorningReminderHour   = 8
	MorningReminderMinute = 0
)

// RemindDistanceEnabled — напоминать в дни, когда все пары дистанционные
func (s GifSettings) RemindDistanceEnabled() bool {
	return s.RemindDistance == nil || *s.RemindDistance
}

// RemindInPersonEnabled — напоминать в обычные (очные) дни
func (s GifSettings) RemindInPersonEnabled() bool {
	return s.RemindInPerson != nil && *s.RemindInPerson
}

// GifData — всё хранилище гифок + настройки
type GifData struct {
	Gifs     map[string]GifEntry `json:"gifs"`
	Settings GifSettings         `json:"settings"`
}

// UserState — состояние пользователя в процессе добавления гифки
type UserState struct {
	State     string    // "waiting_media", "waiting_name"
	FileID    string    // временный file_id (после получения медиа)
	FileType  string    // временный file_type
	ExpiresAt time.Time // таймаут 5 минут
	MenuMsgID int       // ID сообщения-подсказки для редактирования
}

// MediaFile — минимальный тип для приёма медиа из Telegram
type MediaFile struct {
	FileID string `json:"file_id"`
}

// ============================================================
// Константы
// ============================================================

var GifDataFile = "gifs.json"

const (
	StateTimeout  = 5 * time.Minute
	MaxGifNameLen = 15 // макс длина имени гифки в символах

	SlotMorning      = "morning"
	SlotEvening      = "evening"
	SlotNotification = "notification"
	SlotCommands     = "commands"
)

// slotDisplayNames — отображаемые названия слотов
var slotDisplayNames = map[string]string{
	SlotMorning:      "🌅 Утро",
	SlotEvening:      "🌙 Вечер",
	SlotNotification: "🔔 Уведомления",
	SlotCommands:     "📱 Команды",
}

// allSlots — все доступные слоты в порядке отображения
var allSlots = []string{SlotMorning, SlotEvening, SlotNotification, SlotCommands}

// userStatesGlobal — мьютекс для защиты userStates (используется из разных горутин)
var userStatesMu sync.Mutex

// ============================================================
// Загрузка / Сохранение
// ============================================================

func (bot *TimetableBot) LoadGifData() error {
	logInfo("LoadGifData: загружаю %s", GifDataFile)
	data, err := os.ReadFile(GifDataFile)
	if err != nil {
		logInfo("LoadGifData: %s не найден, создаю пустой", GifDataFile)
		bot.gifData = GifData{
			Gifs: make(map[string]GifEntry),
			Settings: GifSettings{
				MorningTime: "09:00",
				EveningTime: "21:22",
			},
		}
		return bot.SaveGifData()
	}

	if err := json.Unmarshal(data, &bot.gifData); err != nil {
		logError("LoadGifData: ошибка парсинга: %v", err)
		return fmt.Errorf("ошибка парсинга %s: %w", GifDataFile, err)
	}

	if bot.gifData.Gifs == nil {
		bot.gifData.Gifs = make(map[string]GifEntry)
	}
	if bot.gifData.Settings.MorningTime == "" {
		bot.gifData.Settings.MorningTime = "09:00"
	}
	if bot.gifData.Settings.EveningTime == "" {
		bot.gifData.Settings.EveningTime = "21:22"
	}

	logInfo("LoadGifData: загружено %d гифок, утро=%s, вечер=%s",
		len(bot.gifData.Gifs), bot.gifData.Settings.MorningTime, bot.gifData.Settings.EveningTime)
	return nil
}

func (bot *TimetableBot) SaveGifData() error {
	data, err := json.MarshalIndent(bot.gifData, "", "  ")
	if err != nil {
		logError("SaveGifData: ошибка маршалинга: %v", err)
		return fmt.Errorf("ошибка маршалинга gif data: %w", err)
	}
	if err := os.WriteFile(GifDataFile, data, 0644); err != nil {
		logError("SaveGifData: ошибка записи: %v", err)
		return err
	}
	logDebug("SaveGifData: сохранено %d гифок", len(bot.gifData.Gifs))
	return nil
}

// ============================================================
// Выбор и отправка гифок по слоту
// ============================================================

// GetGifsForSlot возвращает все гифки, назначенные на данный слот
func (bot *TimetableBot) GetGifsForSlot(slot string) []GifEntry {
	var result []GifEntry
	for _, gif := range bot.gifData.Gifs {
		for _, s := range gif.Slots {
			if s == slot {
				result = append(result, gif)
				break
			}
		}
	}
	return result
}

// SendGifForSlot отправляет рандомную гифку из слота в чат.
// Возвращает message_id отправленного сообщения (0 если ничего не отправлено).
func (bot *TimetableBot) SendGifForSlot(chatID string, slot string) int {
	gifs := bot.GetGifsForSlot(slot)
	if len(gifs) == 0 {
		logDebug("SendGifForSlot: нет гифок для слота %s", slot)
		return 0
	}

	gif := gifs[rand.Intn(len(gifs))]
	fileIDPreview := gif.FileID
	if len(fileIDPreview) > 10 {
		fileIDPreview = fileIDPreview[:10] + "..."
	}
	logInfo("SendGifForSlot: отправляю %s (type=%s) для слота %s chat_id=%s",
		fileIDPreview, gif.FileType, slot, chatID)

	msgID, err := bot.SendMediaByFileID(chatID, gif.FileID, gif.FileType)
	if err != nil {
		logError("SendGifForSlot: ошибка отправки: %v", err)
		return 0
	}
	return msgID
}

// ============================================================
// Отправка медиа через Telegram API
// ============================================================

// SendMediaByFileID отправляет медиа по file_id, возвращает message_id
func (bot *TimetableBot) SendMediaByFileID(chatID, fileID, fileType string) (int, error) {
	var method, paramName string
	switch fileType {
	case "sticker":
		method = "sendSticker"
		paramName = "sticker"
	case "video":
		method = "sendVideo"
		paramName = "video"
	default: // "animation" и всё остальное
		method = "sendAnimation"
		paramName = "animation"
	}

	endpoint := fmt.Sprintf("%s%s/%s", TelegramAPIURL, bot.botToken, method)
	logDebug("SendMediaByFileID: method=%s chat_id=%s file_type=%s", method, chatID, fileType)

	data := url.Values{}
	data.Set("chat_id", chatID)
	data.Set(paramName, fileID)

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		return 0, fmt.Errorf("ошибка %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := readAllBody(resp)
		return 0, fmt.Errorf("%s telegram error: %s body=%s", method, resp.Status, string(bodyBytes))
	}

	var response TelegramMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, fmt.Errorf("ошибка декодирования %s response: %w", method, err)
	}
	if !response.Ok {
		return 0, fmt.Errorf("%s response ok=false", method)
	}

	logDebug("SendMediaByFileID: ok message_id=%d", response.Result.MessageID)
	return response.Result.MessageID, nil
}

// readAllBody читает тело HTTP-ответа (хелпер)
func readAllBody(resp *http.Response) ([]byte, error) {
	body := make([]byte, 0, 1024)
	buf := make([]byte, 256)
	for {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	return body, nil
}

// ============================================================
// Управление состоянием пользователя (state machine)
// ============================================================

func (bot *TimetableBot) SetUserState(chatID int64, state *UserState) {
	userStatesMu.Lock()
	defer userStatesMu.Unlock()
	bot.userStates[chatID] = state
	logDebug("SetUserState: chat_id=%d state=%s expires=%s", chatID, state.State, state.ExpiresAt.Format(time.RFC3339))
}

func (bot *TimetableBot) GetUserState(chatID int64) *UserState {
	userStatesMu.Lock()
	defer userStatesMu.Unlock()

	state, ok := bot.userStates[chatID]
	if !ok {
		return nil
	}

	// Проверяем таймаут
	if time.Now().After(state.ExpiresAt) {
		logInfo("GetUserState: таймаут для chat_id=%d state=%s", chatID, state.State)
		delete(bot.userStates, chatID)
		return nil
	}

	return state
}

func (bot *TimetableBot) ClearUserState(chatID int64) {
	userStatesMu.Lock()
	defer userStatesMu.Unlock()
	delete(bot.userStates, chatID)
	logDebug("ClearUserState: chat_id=%d", chatID)
}

// CleanExpiredStates удаляет просроченные состояния (вызывается раз в минуту)
func (bot *TimetableBot) CleanExpiredStates() {
	userStatesMu.Lock()
	defer userStatesMu.Unlock()

	now := time.Now()
	for chatID, state := range bot.userStates {
		if now.After(state.ExpiresAt) {
			logInfo("CleanExpiredStates: удалён expired state chat_id=%d state=%s", chatID, state.State)
			delete(bot.userStates, chatID)
		}
	}
}

// ============================================================
// Команда /gifs — главное меню
// ============================================================

func (bot *TimetableBot) HandleGifsCommand(chatID int64) {
	logInfo("HandleGifsCommand: chat_id=%d", chatID)

	text := "🖼 <b>Управление гифками</b>\n\n" +
		"Добавляйте свои гифки, стикеры и короткие видео.\n" +
		"Назначайте их на утро, вечер, уведомления или команды."

	gifCount := len(bot.gifData.Gifs)
	if gifCount > 0 {
		text += fmt.Sprintf("\n\n📊 Сохранено: <b>%d</b> шт", gifCount)
	}

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "➕ Добавить", CallbackData: "g:a"},
				{Text: "📋 Мои гифки", CallbackData: "g:l"},
			},
		},
	}

	if _, err := bot.SendMessageToChatWithMarkup(chatID, text, markup); err != nil {
		logError("HandleGifsCommand: ошибка: %v", err)
	}
}

// ============================================================
// Команда /settings — настройки времени
// ============================================================

func (bot *TimetableBot) HandleSettingsCommand(chatID int64) {
	logInfo("HandleSettingsCommand: chat_id=%d", chatID)
	bot.sendSettingsMenu(chatID, 0) // 0 = отправить новое сообщение
}

func (bot *TimetableBot) sendSettingsMenu(chatID int64, editMessageID int) {
	text := fmt.Sprintf(
		"⚙️ <b>Настройки</b>\n\n"+
			"🌅 Утреннее сообщение: <b>%s</b>\n"+
			"🌙 Вечернее сообщение: <b>%s</b>\n\n"+
			"В указанное время придёт гифка и сводка расписания.\n\n"+
			"⏰ Напоминание о парах в %02d:%02d: %s",
		bot.gifData.Settings.MorningTime,
		bot.gifData.Settings.EveningTime,
		MorningReminderHour, MorningReminderMinute,
		reminderStatusText(bot.gifData.Settings),
	)

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🌅 Настроить утро", CallbackData: "s:mt"},
				{Text: "🌙 Настроить вечер", CallbackData: "s:et"},
			},
			{
				{Text: "⏰ Утреннее напоминание о парах", CallbackData: "s:rm"},
			},
			{
				{Text: "🔄 Спарсить расписание", CallbackData: "s:parse"},
			},
		},
	}

	if editMessageID != 0 {
		bot.EditMessageText(chatID, editMessageID, text, markup)
	} else {
		bot.SendMessageToChatWithMarkup(chatID, text, markup)
	}
}

// ============================================================
// Callback routing
// ============================================================

// HandleGifCallback обрабатывает все callback_data с префиксом "g:"
func (bot *TimetableBot) HandleGifCallback(query *CallbackQuery) {
	data := query.Data
	chatID := query.From.ID
	var messageID int
	if query.Message != nil {
		messageID = query.Message.MessageID
		chatID = query.Message.Chat.ID
	}

	logInfo("HandleGifCallback: data=%s chat_id=%d message_id=%d", data, chatID, messageID)

	switch {
	case data == "g:m":
		bot.editGifsMenu(chatID, messageID)

	case data == "g:a":
		bot.handleGifAddStart(chatID, messageID)

	case data == "g:c":
		bot.handleGifCancel(chatID, messageID)

	case data == "g:l":
		bot.handleGifList(chatID, messageID)

	case strings.HasPrefix(data, "g:e:"):
		name := strings.TrimPrefix(data, "g:e:")
		bot.handleGifEdit(chatID, messageID, name)

	case strings.HasPrefix(data, "g:v:"):
		name := strings.TrimPrefix(data, "g:v:")
		bot.handleGifView(chatID, messageID, name)

	case strings.HasPrefix(data, "g:s:"):
		rest := strings.TrimPrefix(data, "g:s:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			bot.handleGifToggleSlot(chatID, messageID, parts[0], parts[1])
		}

	case strings.HasPrefix(data, "g:d:"):
		name := strings.TrimPrefix(data, "g:d:")
		bot.handleGifDeleteAsk(chatID, messageID, name)

	case strings.HasPrefix(data, "g:cd:"):
		name := strings.TrimPrefix(data, "g:cd:")
		bot.handleGifDeleteConfirm(chatID, messageID, name)
	}
}

// HandleSettingsCallback обрабатывает все callback_data с префиксом "s:"
func (bot *TimetableBot) HandleSettingsCallback(query *CallbackQuery) {
	data := query.Data
	chatID := query.From.ID
	var messageID int
	if query.Message != nil {
		messageID = query.Message.MessageID
		chatID = query.Message.Chat.ID
	}

	logInfo("HandleSettingsCallback: data=%s chat_id=%d message_id=%d", data, chatID, messageID)

	switch {
	case data == "s:m":
		bot.sendSettingsMenu(chatID, messageID)

	case data == "s:mt":
		bot.handleSettingsTimeSelect(chatID, messageID, "morning")

	case data == "s:et":
		bot.handleSettingsTimeSelect(chatID, messageID, "evening")

	case data == "s:parse":
		bot.handleParseSchedule(chatID, messageID)

	case data == "s:rm":
		bot.sendReminderMenu(chatID, messageID)

	case data == "s:rd":
		on := !bot.gifData.Settings.RemindDistanceEnabled()
		bot.gifData.Settings.RemindDistance = &on
		bot.saveReminderSettings()
		bot.sendReminderMenu(chatID, messageID)

	case data == "s:ri":
		on := !bot.gifData.Settings.RemindInPersonEnabled()
		bot.gifData.Settings.RemindInPerson = &on
		bot.saveReminderSettings()
		bot.sendReminderMenu(chatID, messageID)

	case strings.HasPrefix(data, "s:t:"):
		rest := strings.TrimPrefix(data, "s:t:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			bot.handleSettingsSetTime(chatID, messageID, parts[0], parts[1])
		}
	}
}

// ============================================================
// /settings — утреннее напоминание о парах
// ============================================================

func (bot *TimetableBot) saveReminderSettings() {
	if err := bot.SaveGifData(); err != nil {
		logError("saveReminderSettings: ошибка сохранения: %v", err)
	}
}

func checkbox(on bool) string {
	if on {
		return "✅"
	}
	return "⬜"
}

func reminderStatusText(s GifSettings) string {
	d, p := s.RemindDistanceEnabled(), s.RemindInPersonEnabled()
	switch {
	case d && p:
		return "<b>вкл</b> (дистант и очно)"
	case d:
		return "<b>вкл</b> (только дистант)"
	case p:
		return "<b>вкл</b> (только очно)"
	}
	return "<b>выкл</b>"
}

func (bot *TimetableBot) sendReminderMenu(chatID int64, messageID int) {
	s := bot.gifData.Settings
	text := fmt.Sprintf(
		"⏰ <b>Утреннее напоминание о парах</b>\n\n"+
			"Каждое утро в <b>%02d:%02d</b> бот присылает список всех пар на сегодня "+
			"(номер, время, предмет, преподаватель).\n\n"+
			"%s <b>Дистант</b> — в дни, когда все пары дистанционные\n"+
			"%s <b>Очно</b> — в обычные дни, когда есть очные пары\n\n"+
			"<i>Если не отмечено ничего, напоминание отключено. Если пар нет — сообщение не приходит.</i>",
		MorningReminderHour, MorningReminderMinute,
		checkbox(s.RemindDistanceEnabled()),
		checkbox(s.RemindInPersonEnabled()),
	)
	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: checkbox(s.RemindDistanceEnabled()) + " Дистант", CallbackData: "s:rd"}},
			{{Text: checkbox(s.RemindInPersonEnabled()) + " Очно", CallbackData: "s:ri"}},
			{{Text: "◀️ Назад к настройкам", CallbackData: "s:m"}},
		},
	}
	if messageID != 0 {
		bot.EditMessageText(chatID, messageID, text, markup)
	} else {
		bot.SendMessageToChatWithMarkup(chatID, text, markup)
	}
}

// ============================================================
// /gifs — добавление гифки (flow)
// ============================================================

func (bot *TimetableBot) editGifsMenu(chatID int64, messageID int) {
	text := "🖼 <b>Управление гифками</b>\n\n" +
		"Добавляйте свои гифки, стикеры и короткие видео.\n" +
		"Назначайте их на утро, вечер, уведомления или команды."

	gifCount := len(bot.gifData.Gifs)
	if gifCount > 0 {
		text += fmt.Sprintf("\n\n📊 Сохранено: <b>%d</b> шт", gifCount)
	}

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "➕ Добавить", CallbackData: "g:a"},
				{Text: "📋 Мои гифки", CallbackData: "g:l"},
			},
		},
	}

	bot.EditMessageText(chatID, messageID, text, markup)
}

func (bot *TimetableBot) handleGifAddStart(chatID int64, messageID int) {
	logInfo("handleGifAddStart: chat_id=%d", chatID)

	// Устанавливаем состояние ожидания медиа
	bot.SetUserState(chatID, &UserState{
		State:     "waiting_media",
		ExpiresAt: time.Now().Add(StateTimeout),
		MenuMsgID: messageID,
	})

	text := "📸 <b>Отправьте гифку, стикер или короткое видео</b>\n\n" +
		"⏱ У вас есть 5 минут"

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "❌ Отмена", CallbackData: "g:c"}},
		},
	}

	bot.EditMessageText(chatID, messageID, text, markup)
}

func (bot *TimetableBot) handleGifCancel(chatID int64, messageID int) {
	logInfo("handleGifCancel: chat_id=%d", chatID)
	bot.ClearUserState(chatID)
	bot.editGifsMenu(chatID, messageID)
}

// HandleMediaMessage обрабатывает входящее медиа-сообщение при ожидании гифки
func (bot *TimetableBot) HandleMediaMessage(chatID int64, fileID, fileType string) {
	state := bot.GetUserState(chatID)
	if state == nil || state.State != "waiting_media" {
		return
	}

	fileIDPreview := fileID
	if len(fileIDPreview) > 10 {
		fileIDPreview = fileIDPreview[:10] + "..."
	}
	logInfo("HandleMediaMessage: chat_id=%d file_type=%s file_id=%s", chatID, fileType, fileIDPreview)

	// Сохраняем file_id во временное состояние
	state.FileID = fileID
	state.FileType = fileType
	state.State = "waiting_name"
	state.ExpiresAt = time.Now().Add(StateTimeout) // сбрасываем таймер
	bot.SetUserState(chatID, state)

	// Обновляем подсказку
	text := "✏️ <b>Как назвать эту гифку?</b>\n\n" +
		fmt.Sprintf("Введите имя (до %d символов).\n", MaxGifNameLen) +
		"⏱ У вас есть 5 минут"

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "❌ Отмена", CallbackData: "g:c"}},
		},
	}

	if state.MenuMsgID != 0 {
		bot.EditMessageText(chatID, state.MenuMsgID, text, markup)
	}
}

// HandleGifNameMessage обрабатывает текстовое сообщение с именем гифки.
// Возвращает true если сообщение было обработано (пользователь в состоянии waiting_name).
func (bot *TimetableBot) HandleGifNameMessage(chatID int64, name string) bool {
	state := bot.GetUserState(chatID)
	if state == nil || state.State != "waiting_name" {
		return false
	}

	logInfo("HandleGifNameMessage: chat_id=%d name=%q", chatID, name)

	// Валидация имени
	name = strings.TrimSpace(name)
	if name == "" {
		bot.SendMessageToChat(chatID, "❌ Имя не может быть пустым. Попробуйте ещё раз.")
		return true
	}

	// Проверяем длину
	nameRunes := []rune(name)
	if len(nameRunes) > MaxGifNameLen {
		bot.SendMessageToChat(chatID, fmt.Sprintf("❌ Слишком длинное имя. Максимум %d символов.", MaxGifNameLen))
		return true
	}

	// Проверяем уникальность
	if _, exists := bot.gifData.Gifs[name]; exists {
		bot.SendMessageToChat(chatID, fmt.Sprintf("❌ Гифка «%s» уже существует. Выберите другое имя.", escapeHTML(name)))
		return true
	}

	// Проверяем что callback data не будет слишком длинным
	testCallback := "g:s:" + name + ":notification"
	if len(testCallback) > 64 {
		bot.SendMessageToChat(chatID, "❌ Имя слишком длинное для кнопок. Попробуйте короче.")
		return true
	}

	// Сохраняем гифку
	bot.gifData.Gifs[name] = GifEntry{
		FileID:   state.FileID,
		FileType: state.FileType,
		Slots:    []string{},
	}

	if err := bot.SaveGifData(); err != nil {
		logError("HandleGifNameMessage: ошибка сохранения: %v", err)
		bot.SendMessageToChat(chatID, "❌ Ошибка сохранения. Попробуйте позже.")
		bot.ClearUserState(chatID)
		return true
	}

	savedMenuMsgID := state.MenuMsgID
	bot.ClearUserState(chatID)

	// Обновляем подсказку
	text := fmt.Sprintf("✅ Гифка «<b>%s</b>» сохранена!\n\n"+
		"Теперь назначьте её на слоты через 📋 Мои гифки.", escapeHTML(name))

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📋 Мои гифки", CallbackData: "g:l"},
				{Text: "➕ Ещё", CallbackData: "g:a"},
			},
		},
	}

	if savedMenuMsgID != 0 {
		bot.EditMessageText(chatID, savedMenuMsgID, text, markup)
	} else {
		bot.SendMessageToChatWithMarkup(chatID, text, markup)
	}

	logInfo("HandleGifNameMessage: гифка «%s» сохранена (type=%s)", name, state.FileType)
	return true
}

// ============================================================
// /gifs — список и настройка гифок
// ============================================================

func (bot *TimetableBot) handleGifList(chatID int64, messageID int) {
	logInfo("handleGifList: chat_id=%d gifs=%d", chatID, len(bot.gifData.Gifs))

	if len(bot.gifData.Gifs) == 0 {
		text := "📋 <b>Мои гифки</b>\n\n" +
			"<i>Пока пусто. Добавьте первую гифку!</i>"
		markup := InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "➕ Добавить", CallbackData: "g:a"}},
				{{Text: "◀️ Назад", CallbackData: "g:m"}},
			},
		}
		bot.EditMessageText(chatID, messageID, text, markup)
		return
	}

	text := "📋 <b>Мои гифки</b>\n\nВыберите гифку для настройки:"

	// Собираем имена и сортируем
	names := make([]string, 0, len(bot.gifData.Gifs))
	for name := range bot.gifData.Gifs {
		names = append(names, name)
	}
	sort.Strings(names)

	// Формируем кнопки с именами гифок (по 2 в ряд)
	var rows [][]InlineKeyboardButton
	var currentRow []InlineKeyboardButton

	for _, name := range names {
		gif := bot.gifData.Gifs[name]
		label := name
		if len(gif.Slots) > 0 {
			label += fmt.Sprintf(" (%d)", len(gif.Slots))
		}
		currentRow = append(currentRow, InlineKeyboardButton{
			Text:         label,
			CallbackData: "g:e:" + name,
		})
		if len(currentRow) == 2 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	// Кнопка "Назад"
	rows = append(rows, []InlineKeyboardButton{
		{Text: "◀️ Назад", CallbackData: "g:m"},
	})

	markup := InlineKeyboardMarkup{InlineKeyboard: rows}
	bot.EditMessageText(chatID, messageID, text, markup)
}

func (bot *TimetableBot) handleGifEdit(chatID int64, messageID int, name string) {
	gif, exists := bot.gifData.Gifs[name]
	if !exists {
		bot.handleGifList(chatID, messageID)
		return
	}

	logInfo("handleGifEdit: chat_id=%d name=%s slots=%v", chatID, name, gif.Slots)

	text := fmt.Sprintf("🖼 <b>%s</b>\n\n"+
		"Тип: %s\n"+
		"Назначена: %s",
		escapeHTML(name),
		gifTypeDisplay(gif.FileType),
		slotsDisplay(gif.Slots),
	)

	// Кнопки настроек
	var rows [][]InlineKeyboardButton

	// Кнопка просмотра
	rows = append(rows, []InlineKeyboardButton{
		{Text: "👁 Посмотреть", CallbackData: "g:v:" + name},
	})

	// Кнопки слотов
	for _, slot := range allSlots {
		enabled := hasSlot(gif.Slots, slot)
		icon := "❌"
		if enabled {
			icon = "✅"
		}
		rows = append(rows, []InlineKeyboardButton{
			{Text: fmt.Sprintf("%s %s", slotDisplayNames[slot], icon), CallbackData: "g:s:" + name + ":" + slot},
		})
	}

	// Удалить и Назад
	rows = append(rows, []InlineKeyboardButton{
		{Text: "🗑 Удалить", CallbackData: "g:d:" + name},
		{Text: "◀️ Назад", CallbackData: "g:l"},
	})

	markup := InlineKeyboardMarkup{InlineKeyboard: rows}
	bot.EditMessageText(chatID, messageID, text, markup)
}

func (bot *TimetableBot) handleGifView(chatID int64, messageID int, name string) {
	gif, exists := bot.gifData.Gifs[name]
	if !exists {
		return
	}

	logInfo("handleGifView: chat_id=%d name=%s", chatID, name)

	// Отправляем гифку (не меняем меню — пользователь увидит и может продолжить настройку)
	if _, err := bot.SendMediaByFileID(fmt.Sprintf("%d", chatID), gif.FileID, gif.FileType); err != nil {
		logError("handleGifView: ошибка отправки: %v", err)
	}
}

func (bot *TimetableBot) handleGifToggleSlot(chatID int64, messageID int, name, slot string) {
	gif, exists := bot.gifData.Gifs[name]
	if !exists {
		return
	}

	logInfo("handleGifToggleSlot: chat_id=%d name=%s slot=%s", chatID, name, slot)

	if hasSlot(gif.Slots, slot) {
		gif.Slots = removeSlot(gif.Slots, slot)
	} else {
		gif.Slots = append(gif.Slots, slot)
	}

	bot.gifData.Gifs[name] = gif
	if err := bot.SaveGifData(); err != nil {
		logError("handleGifToggleSlot: ошибка сохранения: %v", err)
	}

	// Обновляем меню настроек гифки
	bot.handleGifEdit(chatID, messageID, name)
}

func (bot *TimetableBot) handleGifDeleteAsk(chatID int64, messageID int, name string) {
	if _, exists := bot.gifData.Gifs[name]; !exists {
		return
	}

	text := fmt.Sprintf("🗑 Удалить гифку «<b>%s</b>»?\n\nЭто действие нельзя отменить.", escapeHTML(name))

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✅ Да, удалить", CallbackData: "g:cd:" + name},
				{Text: "◀️ Нет, назад", CallbackData: "g:e:" + name},
			},
		},
	}

	bot.EditMessageText(chatID, messageID, text, markup)
}

func (bot *TimetableBot) handleGifDeleteConfirm(chatID int64, messageID int, name string) {
	logInfo("handleGifDeleteConfirm: chat_id=%d name=%s", chatID, name)

	delete(bot.gifData.Gifs, name)
	if err := bot.SaveGifData(); err != nil {
		logError("handleGifDeleteConfirm: ошибка сохранения: %v", err)
	}

	text := fmt.Sprintf("✅ Гифка «<b>%s</b>» удалена.", escapeHTML(name))

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "📋 Мои гифки", CallbackData: "g:l"}},
		},
	}

	bot.EditMessageText(chatID, messageID, text, markup)
}

// ============================================================
// /settings — парсинг расписания
// ============================================================

func (bot *TimetableBot) handleParseSchedule(chatID int64, messageID int) {
	logInfo("handleParseSchedule: chat_id=%d", chatID)

	// Сразу показываем "грузится"
	loadingMarkup := InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	bot.EditMessageText(chatID, messageID,
		"⏳ <b>Парсю расписание...</b>\n\n<i>Обычно занимает 5–15 секунд</i>",
		loadingMarkup)

	// Запускаем парсер (синхронно — блокируем, но ответ уже отправлен)
	bot.UpdateSchedule()

	// Показываем результат
	count := len(bot.schedule)
	var text string
	if count > 0 {
		text = fmt.Sprintf("✅ <b>Расписание обновлено!</b>\n\nЗагружено: <b>%d пар</b>", count)
	} else {
		text = "❌ <b>Не удалось спарсить расписание</b>\n\n<i>Проверь логи на сервере</i>"
	}

	markup := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "◀️ Назад к настройкам", CallbackData: "s:m"}},
		},
	}
	bot.EditMessageText(chatID, messageID, text, markup)
}

// ============================================================
// /settings — выбор времени
// ============================================================

func (bot *TimetableBot) handleSettingsTimeSelect(chatID int64, messageID int, timeType string) {
	var label string
	var times []string
	var callbackPrefix string

	if timeType == "morning" {
		label = "🌅 Выберите время утреннего сообщения"
		times = []string{
			"06:00", "06:30", "07:00", "07:30",
			"08:00", "08:30", "09:00", "09:30",
			"10:00", "10:30", "11:00",
		}
		callbackPrefix = "s:t:m:"
	} else {
		label = "🌙 Выберите время вечернего сообщения"
		times = []string{
			"19:00", "19:30", "20:00", "20:30",
			"21:00", "21:30", "22:00", "22:30",
			"23:00", "23:30",
		}
		callbackPrefix = "s:t:e:"
	}

	text := fmt.Sprintf("<b>%s</b>\n\nВ это время придёт гифка и сводка расписания.", label)

	// Определяем текущее выбранное время
	currentTime := bot.gifData.Settings.MorningTime
	if timeType != "morning" {
		currentTime = bot.gifData.Settings.EveningTime
	}

	// Формируем кнопки по 3 в ряд
	var rows [][]InlineKeyboardButton
	var currentRow []InlineKeyboardButton

	for _, t := range times {
		display := t
		if t == currentTime {
			display = "✅ " + t
		}
		currentRow = append(currentRow, InlineKeyboardButton{
			Text:         display,
			CallbackData: callbackPrefix + t,
		})
		if len(currentRow) == 3 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	rows = append(rows, []InlineKeyboardButton{
		{Text: "◀️ Назад", CallbackData: "s:m"},
	})

	markup := InlineKeyboardMarkup{InlineKeyboard: rows}
	bot.EditMessageText(chatID, messageID, text, markup)
}

func (bot *TimetableBot) handleSettingsSetTime(chatID int64, messageID int, timeType, timeValue string) {
	logInfo("handleSettingsSetTime: chat_id=%d type=%s value=%s", chatID, timeType, timeValue)

	if !isValidTimeFormat(timeValue) {
		logError("handleSettingsSetTime: невалидный формат времени: %s", timeValue)
		return
	}

	if timeType == "m" {
		bot.gifData.Settings.MorningTime = timeValue
	} else if timeType == "e" {
		bot.gifData.Settings.EveningTime = timeValue
	}

	if err := bot.SaveGifData(); err != nil {
		logError("handleSettingsSetTime: ошибка сохранения: %v", err)
	}

	// Возвращаемся в меню настроек
	bot.sendSettingsMenu(chatID, messageID)
}

// ============================================================
// Хелперы для извлечения медиа из сообщения
// ============================================================

// ExtractMediaInfo извлекает file_id и тип медиа из сообщения.
// Возвращает пустые строки если в сообщении нет медиа.
func ExtractMediaInfo(msg *Message) (fileID, fileType string) {
	if msg.Animation != nil {
		return msg.Animation.FileID, "animation"
	}
	if msg.Video != nil {
		return msg.Video.FileID, "video"
	}
	if msg.Sticker != nil {
		return msg.Sticker.FileID, "sticker"
	}
	return "", ""
}

// ============================================================
// Вспомогательные функции
// ============================================================

func hasSlot(slots []string, slot string) bool {
	for _, s := range slots {
		if s == slot {
			return true
		}
	}
	return false
}

func removeSlot(slots []string, slot string) []string {
	result := make([]string, 0, len(slots))
	for _, s := range slots {
		if s != slot {
			result = append(result, s)
		}
	}
	return result
}

func gifTypeDisplay(fileType string) string {
	switch fileType {
	case "animation":
		return "🎞 GIF"
	case "sticker":
		return "😀 Стикер"
	case "video":
		return "🎬 Видео"
	default:
		return fileType
	}
}

func slotsDisplay(slots []string) string {
	if len(slots) == 0 {
		return "<i>не назначена</i>"
	}
	names := make([]string, 0, len(slots))
	for _, s := range slots {
		if name, ok := slotDisplayNames[s]; ok {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func isValidTimeFormat(t string) bool {
	if len(t) != 5 || t[2] != ':' {
		return false
	}
	h := (int(t[0]-'0'))*10 + int(t[1]-'0')
	m := (int(t[3]-'0'))*10 + int(t[4]-'0')
	return h >= 0 && h <= 23 && m >= 0 && m <= 59
}

// ParseSettingsTime парсит время из строки "HH:MM" → (hour, minute)
func ParseSettingsTime(timeStr string) (int, int) {
	if len(timeStr) != 5 || timeStr[2] != ':' {
		return 9, 0 // fallback
	}
	h := (int(timeStr[0]-'0'))*10 + int(timeStr[1]-'0')
	m := (int(timeStr[3]-'0'))*10 + int(timeStr[4]-'0')
	return h, m
}
