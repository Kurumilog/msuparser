package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func L(date, num, start, end, subj, room string) Lesson {
	return Lesson{Date: date, LessonNumber: num, TimeStart: start, TimeEnd: end, Subject: subj, Room: room, Teacher: "Иванов И.И."}
}

func TestDayChangeCount(t *testing.T) {
	ch := DayChange{
		Added:     []Lesson{{Subject: "A"}},
		Cancelled: []Lesson{{Subject: "B"}},
		Moved:     []MovedLesson{{Old: Lesson{Subject: "C"}, New: Lesson{Subject: "C"}}},
		Replaced:  []ReplacedLesson{{Old: Lesson{Subject: "D"}, New: Lesson{Subject: "E"}}},
		Modified:  []ModifiedLesson{{Old: Lesson{Subject: "F"}, New: Lesson{Subject: "F"}}},
		MovedAway: []CrossMove{{From: Lesson{Subject: "G"}, To: Lesson{Subject: "G"}}},
		MovedHere: []CrossMove{{From: Lesson{Subject: "H"}, To: Lesson{Subject: "H"}}},
	}
	if ch.Count() != 7 {
		t.Errorf("expected count 7, got %d", ch.Count())
	}
}

func TestLessonKey(t *testing.T) {
	l := Lesson{TimeStart: "09:00", Subject: "Математика"}
	if got := lessonKey(l); got != "09:00|Математика" {
		t.Errorf("lessonKey() = %q, want %q", got, "09:00|Математика")
	}
}

func TestSameSubject(t *testing.T) {
	if !sameSubject("  Математика  ", "математика") {
		t.Errorf("expected sameSubject to be true for case/space difference")
	}
	if sameSubject("Математика", "Физика") {
		t.Errorf("expected sameSubject to be false for different subjects")
	}
}

func TestLessonsByDate(t *testing.T) {
	lessons := []Lesson{
		L("28.10.2026", "2", "10:45", "12:15", "Б", "101"),
		L("27.10.2026", "1", "09:00", "10:30", "А", "102"),
		L("28.10.2026", "1", "09:00", "10:30", "В", "103"),
	}
	m := lessonsByDate(lessons)
	if len(m) != 2 {
		t.Fatalf("expected 2 dates, got %d", len(m))
	}
	if len(m["28.10.2026"]) != 2 {
		t.Fatalf("expected 2 lessons on 28.10.2026, got %d", len(m["28.10.2026"]))
	}
	// Verify sorting by TimeStart
	if m["28.10.2026"][0].TimeStart != "09:00" || m["28.10.2026"][1].TimeStart != "10:45" {
		t.Errorf("lessons not sorted by time")
	}
}

func TestDiffDayUnit(t *testing.T) {
	d := "28.10.2026"

	// 1. Unchanged
	old1 := []Lesson{L(d, "1", "09:00", "10:30", "Математика", "101")}
	ch1 := diffDay(d, old1, old1)
	if ch1.Count() != 0 {
		t.Errorf("expected 0 changes for identical days, got %d", ch1.Count())
	}

	// 2. Added & Cancelled
	old2 := []Lesson{L(d, "1", "09:00", "10:30", "Старый предмет", "101")}
	new2 := []Lesson{L(d, "2", "10:45", "12:15", "Новый предмет", "102")}
	ch2 := diffDay(d, old2, new2)
	if len(ch2.Cancelled) != 1 || len(ch2.Added) != 1 {
		t.Errorf("expected 1 cancelled and 1 added, got %+v", ch2)
	}

	// 3. Moved in-day (same subject, different time)
	old3 := []Lesson{L(d, "1", "09:00", "10:30", "Физика", "101")}
	new3 := []Lesson{L(d, "3", "13:00", "14:30", "Физика", "101")}
	ch3 := diffDay(d, old3, new3)
	if len(ch3.Moved) != 1 || ch3.Marks[lessonKey(new3[0])] != "🔄" {
		t.Errorf("expected 1 moved with 🔄 mark, got %+v", ch3)
	}

	// 4. Replaced (same time, different subject)
	old4 := []Lesson{L(d, "1", "09:00", "10:30", "Химия", "101")}
	new4 := []Lesson{L(d, "1", "09:00", "10:30", "Биология", "101")}
	ch4 := diffDay(d, old4, new4)
	if len(ch4.Replaced) != 1 || ch4.Marks[lessonKey(new4[0])] != "🟥" {
		t.Errorf("expected 1 replaced with 🟥 mark, got %+v", ch4)
	}

	// 5. Modified (room & teacher)
	old5 := []Lesson{{Date: d, LessonNumber: "1", TimeStart: "09:00", TimeEnd: "10:30", Subject: "История", Room: "101", Teacher: "Петров"}}
	new5 := []Lesson{{Date: d, LessonNumber: "1", TimeStart: "09:00", TimeEnd: "10:30", Subject: "История", Room: "202", Teacher: "Сидоров"}}
	ch5 := diffDay(d, old5, new5)
	if len(ch5.Modified) != 1 || ch5.Marks[lessonKey(new5[0])] != "🔄" {
		t.Errorf("expected 1 modified with 🔄 mark, got %+v", ch5)
	}
	if !ch5.TeacherShown[lessonKey(new5[0])] {
		t.Errorf("expected TeacherShown to be true when teacher changed")
	}
}

func TestDiff(t *testing.T) {
	loc := moscowLocation()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, loc)
	meta := scheduleMeta{RangeStart: "26.10.2026", RangeEnd: "09.11.2026"}

	d := "28.10.2026"
	old := []Lesson{
		L(d, "1", "09:00", "10:30", "Русский язык [лек]", "101"),
		L(d, "2", "10:45", "12:15", "Английский [сем]", "412"),
		L(d, "3", "13:00", "14:30", "Физкультура [сем]", "спз"),
		L(d, "4", "14:45", "16:15", "Математика [лек]", "205"),
		L("29.10.2026", "1", "09:00", "10:30", "История [лек]", "1"),
		L("03.11.2026", "1", "09:00", "10:30", "Далёкая [лек]", "1"), // вне окна
	}
	nw := []Lesson{
		L(d, "1", "09:00", "10:30", "Русский язык [лек]", "101"),
		L(d, "3", "13:00", "14:30", "Биология [сем]", "204"),
		L(d, "4", "14:45", "16:15", "Математика [лек]", "301"),
		L(d, "5", "16:30", "18:00", "Английский [сем]", "412"),
		L("30.10.2026", "2", "10:45", "12:15", "История [лек]", "1"),
		L("03.11.2026", "2", "10:45", "12:15", "Далёкая [лек]", "1"),
	}
	ch := DiffSchedules(old, nw, meta, now, 7)
	if len(ch) != 3 {
		t.Fatalf("ожидал 3 дня, получил %d", len(ch))
	}
	c := ch[0]
	if len(c.Moved) != 1 || len(c.Replaced) != 1 || len(c.Modified) != 1 {
		t.Fatalf("неверный разбор: %+v", c)
	}
	if len(ch[1].MovedAway) != 1 || len(ch[2].MovedHere) != 1 {
		t.Fatalf("межденный перенос не найден")
	}
	if len(ch[0].Added) != 0 || len(ch[0].Cancelled) != 0 {
		t.Fatalf("лишние add/cancel")
	}
}

func TestNoChangeAndStaleMeta(t *testing.T) {
	loc := moscowLocation()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, loc)
	meta := scheduleMeta{RangeStart: "26.10.2026", RangeEnd: "09.11.2026"}
	a := []Lesson{L("27.10.2026", "1", "09:00", "10:30", "X [лек]", "1")}
	if ch := DiffSchedules(a, a, meta, now, 7); len(ch) != 0 {
		t.Fatal("ложные изменения")
	}
	// старый снимок не покрывал эту дату — не считаем добавлением
	old := scheduleMeta{RangeStart: "01.10.2026", RangeEnd: "15.10.2026"}
	if ch := DiffSchedules(nil, a, old, now, 7); len(ch) != 0 {
		t.Fatal("дата вне покрытия старого снимка")
	}
	// отмена всего дня
	ch := DiffSchedules(a, nil, meta, now, 7)
	if len(ch) != 1 || len(ch[0].Cancelled) != 1 || !strings.Contains(FormatDayChange(ch[0], now), "Пар нет") {
		t.Fatal("отмена дня")
	}

	// invalid meta range dates
	badMeta := scheduleMeta{RangeStart: "invalid", RangeEnd: "invalid"}
	if ch := DiffSchedules(a, a, badMeta, now, 7); ch != nil {
		t.Errorf("expected nil for bad meta dates")
	}
}

func TestFormattingHelpers(t *testing.T) {
	// lessonLabel
	if got := lessonLabel(Lesson{LessonNumber: "2"}); got != "2 пара" {
		t.Errorf("lessonLabel(2) = %q, want '2 пара'", got)
	}
	if got := lessonLabel(Lesson{LessonNumber: "0", TimeStart: "10:00"}); got != "10:00" {
		t.Errorf("lessonLabel(0) = %q, want '10:00'", got)
	}
	if got := lessonLabel(Lesson{LessonNumber: "", TimeStart: "11:00"}); got != "11:00" {
		t.Errorf("lessonLabel() = %q, want '11:00'", got)
	}

	// fmtRoomPart
	if got := fmtRoomPart(""); got != "" {
		t.Errorf("fmtRoomPart('') = %q, want ''", got)
	}
	if got := fmtRoomPart("101"); got != "<i>(ауд. 101)</i>" {
		t.Errorf("fmtRoomPart('101') = %q", got)
	}
	if got := fmtRoomPart("дистанционно"); got != "<i>(дистанционно)</i>" {
		t.Errorf("fmtRoomPart('дистанционно') = %q", got)
	}

	// roomOrDash
	if got := roomOrDash(""); got != "—" {
		t.Errorf("roomOrDash('') = %q, want —", got)
	}
	if got := roomOrDash("205"); got != "205" {
		t.Errorf("roomOrDash('205') = %q, want 205", got)
	}

	// withSpace
	if got := withSpace(""); got != "" {
		t.Errorf("withSpace('') = %q", got)
	}
	if got := withSpace("test"); got != " test" {
		t.Errorf("withSpace('test') = %q", got)
	}

	// teacherOrDash
	if got := teacherOrDash(""); got != "—" {
		t.Errorf("teacherOrDash('') = %q", got)
	}
	if got := teacherOrDash("Иванов"); got != "Иванов" {
		t.Errorf("teacherOrDash('Иванов') = %q", got)
	}

	// roomChangePart
	o := Lesson{Room: "101"}
	nSame := Lesson{Room: "101"}
	nDiff := Lesson{Room: "202"}
	if got := roomChangePart(o, nSame); got != " <i>(ауд. 101)</i>" {
		t.Errorf("roomChangePart same = %q", got)
	}
	if got := roomChangePart(o, nDiff); got != " <i>(ауд. 101 → 202)</i>" {
		t.Errorf("roomChangePart diff = %q", got)
	}

	// dayRelative
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, moscowLocation())
	if got := dayRelative("28.10.2026", now); got != " · сегодня" {
		t.Errorf("dayRelative today = %q", got)
	}
	if got := dayRelative("29.10.2026", now); got != " · завтра" {
		t.Errorf("dayRelative tomorrow = %q", got)
	}
	if got := dayRelative("30.10.2026", now); got != "" {
		t.Errorf("dayRelative other = %q", got)
	}

	// shortDate
	if got := shortDate("28.10.2026"); got != "Ср 28.10" {
		t.Errorf("shortDate('28.10.2026') = %q", got)
	}
	if got := shortDate("invalid"); got != "invalid" {
		t.Errorf("shortDate('invalid') = %q", got)
	}

	// FormatChangesSummary
	summary := FormatChangesSummary([]DayChange{
		{Date: "28.10.2026", Added: []Lesson{{Subject: "A"}}},
		{Date: "29.10.2026", Cancelled: []Lesson{{Subject: "B"}}},
	})
	if !strings.Contains(summary, "Массовые изменения") || !strings.Contains(summary, "Ср 28.10") {
		t.Errorf("FormatChangesSummary output unexpected: %s", summary)
	}
}

func TestScheduleStorageAtomic(t *testing.T) {
	tempDir := t.TempDir()
	origSchedule := ScheduleFile
	origMeta := ScheduleMetaFile
	ScheduleFile = filepath.Join(tempDir, "schedule.json")
	ScheduleMetaFile = filepath.Join(tempDir, "schedule_meta.json")
	defer func() {
		ScheduleFile = origSchedule
		ScheduleMetaFile = origMeta
	}()

	// 1. Loading when files don't exist
	if _, ok := loadScheduleMeta(); ok {
		t.Errorf("expected loadScheduleMeta to return false on missing file")
	}

	// 2. Saving schedule and metadata
	lessons := []Lesson{
		{Date: "28.10.2026", LessonNumber: "1", Subject: "Математика"},
	}
	meta := scheduleMeta{
		FetchedAt:  "2026-10-26T14:00:00Z",
		RangeStart: "26.10.2026",
		RangeEnd:   "09.11.2026",
	}

	if err := saveSchedule(lessons, meta); err != nil {
		t.Fatalf("saveSchedule failed: %v", err)
	}

	// 3. Loading saved metadata
	loadedMeta, ok := loadScheduleMeta()
	if !ok {
		t.Fatalf("expected loadScheduleMeta to succeed")
	}
	if loadedMeta.RangeStart != "26.10.2026" || loadedMeta.RangeEnd != "09.11.2026" {
		t.Errorf("loaded meta mismatch: %+v", loadedMeta)
	}

	// 4. Loading saved schedule.json
	data, err := ioutil.ReadFile(ScheduleFile)
	if err != nil {
		t.Fatalf("read schedule.json failed: %v", err)
	}
	var loadedLessons []Lesson
	if err := json.Unmarshal(data, &loadedLessons); err != nil || len(loadedLessons) != 1 {
		t.Errorf("unmarshal schedule.json failed: %v", err)
	}

	// 5. Corrupted metadata file
	if err := ioutil.WriteFile(ScheduleMetaFile, []byte("{corrupted json"), 0644); err != nil {
		t.Fatalf("write corrupted meta failed: %v", err)
	}
	if _, ok := loadScheduleMeta(); ok {
		t.Errorf("expected loadScheduleMeta to return false for corrupted file")
	}
}

func TestShouldRefreshSchedule(t *testing.T) {
	loc := moscowLocation()
	nowDay := time.Date(2026, 10, 26, 14, 0, 0, 0, loc)
	nowNight := time.Date(2026, 10, 26, 3, 0, 0, 0, loc)

	// Daytime: 15 min interval
	last10m := nowDay.Add(-10 * time.Minute)
	last16m := nowDay.Add(-16 * time.Minute)

	if shouldRefreshSchedule(nowDay, last10m) {
		t.Errorf("expected false for 10 min during daytime")
	}
	if !shouldRefreshSchedule(nowDay, last16m) {
		t.Errorf("expected true for 16 min during daytime")
	}

	// Nighttime: 60 min interval
	last40m := nowNight.Add(-40 * time.Minute)
	last65m := nowNight.Add(-65 * time.Minute)

	if shouldRefreshSchedule(nowNight, last40m) {
		t.Errorf("expected false for 40 min during nighttime")
	}
	if !shouldRefreshSchedule(nowNight, last65m) {
		t.Errorf("expected true for 65 min during nighttime")
	}
}

func TestRefreshScheduleIntegration(t *testing.T) {
	tempDir := t.TempDir()
	origSchedule := ScheduleFile
	origMeta := ScheduleMetaFile
	origFetcher := scheduleFetcher
	ScheduleFile = filepath.Join(tempDir, "schedule.json")
	ScheduleMetaFile = filepath.Join(tempDir, "schedule_meta.json")
	defer func() {
		ScheduleFile = origSchedule
		ScheduleMetaFile = origMeta
		scheduleFetcher = origFetcher
	}()

	bot := NewTimetableBot("token", "12345")
	var sentMessages []string
	bot.sendMessageFunc = func(msg string) error {
		sentMessages = append(sentMessages, msg)
		return nil
	}

	mockLessons := []Lesson{
		L("28.10.2026", "1", "09:00", "10:30", "Математика [лек]", "205"),
	}

	scheduleFetcher = func() ([]Lesson, error) {
		return mockLessons, nil
	}

	// 1. Initial run: no metadata file exists yet -> saves baseline without sending notifications
	if err := bot.RefreshSchedule(); err != nil {
		t.Fatalf("first RefreshSchedule failed: %v", err)
	}
	if len(sentMessages) != 0 {
		t.Errorf("expected 0 messages on initial baseline run, got %d", len(sentMessages))
	}
	if len(bot.schedule) != 1 {
		t.Errorf("expected bot.schedule to have 1 lesson, got %d", len(bot.schedule))
	}

	// 2. Second run: identical schedule -> 0 notifications
	if err := bot.RefreshSchedule(); err != nil {
		t.Fatalf("second RefreshSchedule failed: %v", err)
	}
	if len(sentMessages) != 0 {
		t.Errorf("expected 0 messages when no changes, got %d", len(sentMessages))
	}

	// 3. Third run: a lesson is added on tomorrow's date
	mockLessons = append(mockLessons, L("28.10.2026", "2", "10:45", "12:15", "Физика [лек]", "101"))
	if err := bot.RefreshSchedule(); err != nil {
		t.Fatalf("third RefreshSchedule failed: %v", err)
	}
	if len(sentMessages) != 1 {
		t.Fatalf("expected 1 change notification sent, got %d", len(sentMessages))
	}
	if !strings.Contains(sentMessages[0], "Физика [лек]") {
		t.Errorf("expected message to mention added lesson, got: %s", sentMessages[0])
	}

	// 4. Send failure simulation: error should be returned and metadata not corrupted
	sentMessages = nil
	bot.sendMessageFunc = func(msg string) error {
		return fmt.Errorf("telegram network error")
	}
	mockLessons = append(mockLessons, L("28.10.2026", "3", "13:00", "14:30", "Химия", "303"))
	err := bot.RefreshSchedule()
	if err == nil {
		t.Errorf("expected error when SendMessage fails")
	}

	// Restore SendMessage
	bot.sendMessageFunc = func(msg string) error {
		sentMessages = append(sentMessages, msg)
		return nil
	}

	// 5. Empty fetch handling
	bot.schedule = mockLessons
	scheduleFetcher = func() ([]Lesson, error) {
		return []Lesson{}, nil
	}
	emptyFetchStreak = 0

	// Streaks 1 to 3 should be rejected as transient failures
	for i := 1; i < EmptyFetchLimit; i++ {
		err := bot.RefreshSchedule()
		if err == nil {
			t.Errorf("expected error on empty fetch streak %d", i)
		}
		if emptyFetchStreak != i {
			t.Errorf("expected emptyFetchStreak=%d, got %d", i, emptyFetchStreak)
		}
	}

	// Streak 4: accepted
	err = bot.RefreshSchedule()
	if err != nil {
		t.Errorf("expected streak %d to be accepted, got error %v", EmptyFetchLimit, err)
	}

	// 6. Test UpdateSchedule wrapper
	bot.UpdateSchedule()
}

func TestFormatDayChangeComplete(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, moscowLocation())

	ch := DayChange{
		Date: "28.10.2026",
		Added: []Lesson{
			{LessonNumber: "1", Subject: "Новый предмет", Room: "201", Teacher: "Иванов И.И."},
		},
		Cancelled: []Lesson{
			{LessonNumber: "2", Subject: "Отмененный предмет"},
		},
		Moved: []MovedLesson{
			{Old: Lesson{LessonNumber: "3", Subject: "Сдвинутый", Room: "101", Teacher: "Петров"}, New: Lesson{LessonNumber: "4", Subject: "Сдвинутый", Room: "102", Teacher: "Сидоров"}},
		},
		MovedAway: []CrossMove{
			{From: Lesson{LessonNumber: "1", Subject: "Уехавший"}, To: Lesson{Date: "29.10.2026", LessonNumber: "2", Subject: "Уехавший"}},
		},
		MovedHere: []CrossMove{
			{From: Lesson{Date: "27.10.2026", LessonNumber: "1", Subject: "Приехавший"}, To: Lesson{LessonNumber: "3", Subject: "Приехавший", Room: "303"}},
		},
		Replaced: []ReplacedLesson{
			{Old: Lesson{LessonNumber: "5", Subject: "Старый"}, New: Lesson{LessonNumber: "5", Subject: "Заменивший", Room: "404", Teacher: "Кузнецов"}},
		},
		Modified: []ModifiedLesson{
			{Old: Lesson{LessonNumber: "6", Subject: "Измененный", Room: "101", Teacher: "Т1"}, New: Lesson{LessonNumber: "6", Subject: "Измененный", Room: "102", Teacher: "Т2"}},
		},
		NewDay: []Lesson{
			{LessonNumber: "1", TimeStart: "09:00", TimeEnd: "10:30", Subject: "Новый предмет", Room: "201", Teacher: "Иванов И.И."},
		},
		Marks: map[string]string{
			"09:00|Новый предмет": "🟥",
		},
		TeacherShown: map[string]bool{
			"09:00|Новый предмет": true,
		},
	}

	formatted := FormatDayChange(ch, now)

	mustContain := []string{
		"⚠️ <b>Изменения в расписании на Среду (28.10)</b> · сегодня",
		"🟥 + Добавлена: <code>1 пара</code> — Новый предмет <i>(ауд. 201)</i>",
		"👨‍🏫 Иванов И.И.",
		"🟩 − Отменена: <code>2 пара</code> — Отмененный предмет",
		"🔄 Перенос: Сдвинутый — <code>3 пара</code> → <code>4 пара</code> <i>(ауд. 101 → 102)</i>",
		"👨‍🏫 Петров → Сидоров",
		"🔄 Перенос на другой день: Уехавший — <code>1 пара</code> → <b>Чт 29.10</b>, <code>2 пара</code>",
		"🔄 Перенос с другого дня: Приехавший — с <b>Вт 27.10</b> (<code>1 пара</code>) → <code>3 пара</code> <i>(ауд. 303)</i>",
		"🟥 Замена: <code>5 пара</code> — Старый → Заменивший <i>(ауд. 404)</i>",
		"👨‍🏫 Кузнецов",
		"🔄 Изменено: <code>6 пара</code> — Измененный: ауд. 101 → 102; преподаватель Т1 → Т2",
		"📋 <b>Итоговое расписание:</b>",
		"<code>1 пара 09:00–10:30</code> Новый предмет 🟥 <i>(ауд. 201)</i>",
	}

	for _, sub := range mustContain {
		if !strings.Contains(formatted, sub) {
			t.Errorf("FormatDayChange missing expected text:\n%s\n\nFull formatted message:\n%s", sub, formatted)
		}
	}
}
