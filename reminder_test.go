package main

import (
	"strings"
	"testing"
	"time"
)

func TestGifSettingsRemindFlags(t *testing.T) {
	// Case 1: Defaults (nil fields)
	var s GifSettings
	if !s.RemindDistanceEnabled() {
		t.Errorf("expected RemindDistanceEnabled to default to true")
	}
	if s.RemindInPersonEnabled() {
		t.Errorf("expected RemindInPersonEnabled to default to false")
	}

	// Case 2: Explicit values
	tTrue := true
	tFalse := false

	s.RemindDistance = &tFalse
	s.RemindInPerson = &tTrue

	if s.RemindDistanceEnabled() {
		t.Errorf("expected RemindDistanceEnabled to be false")
	}
	if !s.RemindInPersonEnabled() {
		t.Errorf("expected RemindInPersonEnabled to be true")
	}

	s.RemindDistance = &tTrue
	s.RemindInPerson = &tFalse

	if !s.RemindDistanceEnabled() {
		t.Errorf("expected RemindDistanceEnabled to be true")
	}
	if s.RemindInPersonEnabled() {
		t.Errorf("expected RemindInPersonEnabled to be false")
	}
}

func TestCheckbox(t *testing.T) {
	if got := checkbox(true); got != "✅" {
		t.Errorf("checkbox(true) = %q, want ✅", got)
	}
	if got := checkbox(false); got != "⬜" {
		t.Errorf("checkbox(false) = %q, want ⬜", got)
	}
}

func TestReminderStatusText(t *testing.T) {
	tTrue := true
	tFalse := false

	tests := []struct {
		distance *bool
		inPerson *bool
		wantSub  string
	}{
		{distance: &tTrue, inPerson: &tTrue, wantSub: "дистант и очно"},
		{distance: &tTrue, inPerson: &tFalse, wantSub: "только дистант"},
		{distance: nil, inPerson: nil, wantSub: "только дистант"},
		{distance: &tFalse, inPerson: &tTrue, wantSub: "только очно"},
		{distance: &tFalse, inPerson: &tFalse, wantSub: "выкл"},
	}

	for _, tt := range tests {
		s := GifSettings{RemindDistance: tt.distance, RemindInPerson: tt.inPerson}
		got := reminderStatusText(s)
		if !strings.Contains(got, tt.wantSub) {
			t.Errorf("reminderStatusText(%+v) = %q, want substring %q", s, got, tt.wantSub)
		}
	}
}

func TestIsDistanceLearning(t *testing.T) {
	tests := []struct {
		room string
		want bool
	}{
		{"дистанционно", true},
		{"Дистанционный курс", true},
		{"виртуальная аудитория", true},
		{"ВИРТУАЛ", true},
		{"101", false},
		{"ауд. 205", false},
		{"спортзал", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := isDistanceLearning(tt.room); got != tt.want {
			t.Errorf("isDistanceLearning(%q) = %v, want %v", tt.room, got, tt.want)
		}
	}
}

func TestSendMorningReminder(t *testing.T) {
	var sentMessages []string
	bot := NewTimetableBot("token", "12345")
	bot.sendMessageFunc = func(msg string) error {
		sentMessages = append(sentMessages, msg)
		return nil
	}

	lessonsDistance := []Lesson{
		{LessonNumber: "1", TimeStart: "09:00", TimeEnd: "10:30", Subject: "Бюджетное право [Лк]", Room: "дистанционно", Teacher: "Рябова Е.В."},
		{LessonNumber: "2", TimeStart: "10:45", TimeEnd: "12:15", Subject: "Криминалистика [Лк]", Room: "дистанционно", Teacher: "Джуманбетова А.А."},
	}

	// 1. Send distance day reminder
	bot.SendMorningReminder("28.10.2026", lessonsDistance, true)
	if len(sentMessages) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(sentMessages))
	}
	msg1 := sentMessages[0]
	if !strings.Contains(msg1, "дистанционные пары") {
		t.Errorf("expected distance header, got %s", msg1)
	}
	if !strings.Contains(msg1, "Рябова Е.В.") {
		t.Errorf("expected teacher in message, got %s", msg1)
	}

	// 2. Duplicate prevention for same date
	bot.SendMorningReminder("28.10.2026", lessonsDistance, true)
	if len(sentMessages) != 1 {
		t.Errorf("expected duplicate reminder to be suppressed, got %d messages", len(sentMessages))
	}

	// 3. Send in-person day reminder
	lessonsInPerson := []Lesson{
		{LessonNumber: "1", TimeStart: "09:00", TimeEnd: "10:30", Subject: "Физика [Лк]", Room: "205", Teacher: "Сидоров С.С."},
	}
	bot.SendMorningReminder("29.10.2026", lessonsInPerson, false)
	if len(sentMessages) != 2 {
		t.Fatalf("expected 2 messages sent, got %d", len(sentMessages))
	}
	msg2 := sentMessages[1]
	if !strings.Contains(msg2, "очные пары") {
		t.Errorf("expected in-person header, got %s", msg2)
	}
	if !strings.Contains(msg2, "🚪 205") {
		t.Errorf("expected room emoji and number, got %s", msg2)
	}

	// 4. Mixed day reminder
	lessonsMixed := []Lesson{
		{LessonNumber: "1", TimeStart: "09:00", TimeEnd: "10:30", Subject: "Очная пара", Room: "301"},
		{LessonNumber: "2", TimeStart: "10:45", TimeEnd: "12:15", Subject: "Дистант пара", Room: "дистанционно"},
	}
	bot.SendMorningReminder("30.10.2026", lessonsMixed, false)
	if len(sentMessages) != 3 {
		t.Fatalf("expected 3 messages sent, got %d", len(sentMessages))
	}
	msg3 := sentMessages[2]
	if !strings.Contains(msg3, "есть и дистанционные") {
		t.Errorf("expected mixed header, got %s", msg3)
	}
}

func TestCheckAndSendMorningReminder(t *testing.T) {
	loc := moscowLocation()
	testDate := time.Date(2026, 10, 28, 8, 0, 0, 0, loc)
	dateStr := testDate.Format("02.01.2006")

	tTrue := true
	tFalse := false

	// Case 1: No lessons today -> no message
	bot := NewTimetableBot("token", "12345")
	var sentMessages []string
	bot.sendMessageFunc = func(msg string) error {
		sentMessages = append(sentMessages, msg)
		return nil
	}
	bot.CheckAndSendMorningReminder(testDate)
	if len(sentMessages) != 0 {
		t.Errorf("expected 0 messages when no lessons, got %d", len(sentMessages))
	}

	// Case 2: Distance day + Distance disabled -> no message
	bot = NewTimetableBot("token", "12345")
	bot.sendMessageFunc = func(msg string) error {
		sentMessages = append(sentMessages, msg)
		return nil
	}
	bot.schedule = []Lesson{
		{Date: dateStr, LessonNumber: "1", TimeStart: "09:00", Room: "дистанционно", Subject: "Химия"},
	}
	bot.gifData.Settings.RemindDistance = &tFalse
	bot.CheckAndSendMorningReminder(testDate)
	if len(sentMessages) != 0 {
		t.Errorf("expected 0 messages when distance disabled, got %d", len(sentMessages))
	}

	// Case 3: Distance day + Distance enabled -> 1 message
	bot.gifData.Settings.RemindDistance = &tTrue
	bot.CheckAndSendMorningReminder(testDate)
	if len(sentMessages) != 1 {
		t.Errorf("expected 1 message when distance enabled, got %d", len(sentMessages))
	}

	// Case 4: In-person day + In-person disabled (default) -> no message
	sentMessages = nil
	bot = NewTimetableBot("token", "12345")
	bot.sendMessageFunc = func(msg string) error {
		sentMessages = append(sentMessages, msg)
		return nil
	}
	bot.schedule = []Lesson{
		{Date: dateStr, LessonNumber: "1", TimeStart: "09:00", Room: "101", Subject: "Математика"},
	}
	bot.CheckAndSendMorningReminder(testDate)
	if len(sentMessages) != 0 {
		t.Errorf("expected 0 messages when in-person disabled, got %d", len(sentMessages))
	}

	// Case 5: In-person day + In-person enabled -> 1 message
	bot.gifData.Settings.RemindInPerson = &tTrue
	bot.CheckAndSendMorningReminder(testDate)
	if len(sentMessages) != 1 {
		t.Errorf("expected 1 message when in-person enabled, got %d", len(sentMessages))
	}
}

func TestSendReminderMenuAndCallbacks(t *testing.T) {
	tempDir := t.TempDir()
	origGifFile := GifDataFile
	GifDataFile = tempDir + "/test_gifs.json"
	defer func() { GifDataFile = origGifFile }()

	bot := NewTimetableBot("token", "12345")

	var lastEditedText string
	var lastMarkup InlineKeyboardMarkup

	bot.editMessageTextFunc = func(chatID int64, messageID int, text string, markup InlineKeyboardMarkup) error {
		lastEditedText = text
		lastMarkup = markup
		return nil
	}

	// Test sendReminderMenu
	bot.sendReminderMenu(12345, 100)
	if !strings.Contains(lastEditedText, "Утреннее напоминание о парах") {
		t.Errorf("expected menu text, got %s", lastEditedText)
	}
	if len(lastMarkup.InlineKeyboard) < 3 {
		t.Fatalf("expected at least 3 button rows, got %d", len(lastMarkup.InlineKeyboard))
	}

	// Test callback s:rm
	query := &CallbackQuery{
		ID:      "q1",
		Data:    "s:rm",
		Message: &Message{MessageID: 100, Chat: Chat{ID: 12345}},
	}
	bot.HandleSettingsCallback(query)
	if !strings.Contains(lastEditedText, "Утреннее напоминание о парах") {
		t.Errorf("expected reminder menu on s:rm")
	}

	// Test callback s:rd (toggle distance from true to false)
	query.Data = "s:rd"
	bot.HandleSettingsCallback(query)
	if bot.gifData.Settings.RemindDistanceEnabled() {
		t.Errorf("expected RemindDistance to be toggled to false")
	}

	// Test callback s:ri (toggle in-person from false to true)
	query.Data = "s:ri"
	bot.HandleSettingsCallback(query)
	if !bot.gifData.Settings.RemindInPersonEnabled() {
		t.Errorf("expected RemindInPerson to be toggled to true")
	}
}

func TestHasInPersonLessonsToday(t *testing.T) {
	loc := moscowLocation()
	today := time.Now().In(loc).Format("02.01.2006")

	bot := NewTimetableBot("token", "12345")
	if bot.HasInPersonLessonsToday() {
		t.Errorf("expected false when schedule is empty")
	}

	bot.schedule = []Lesson{
		{Date: today, Room: "дистанционно"},
	}
	if bot.HasInPersonLessonsToday() {
		t.Errorf("expected false when only distance lesson today")
	}

	bot.schedule = []Lesson{
		{Date: today, Room: "101"},
	}
	if !bot.HasInPersonLessonsToday() {
		t.Errorf("expected true when in-person lesson today")
	}
}
