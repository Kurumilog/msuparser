package main

// Постоянная индексация расписания: бот сам периодически загружает расписание,
// сравнивает с предыдущим снимком (schedule.json + schedule_meta.json) и
// отправляет push-сообщения об изменениях.

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// NotifyWindowDays — об изменениях в ближайших N днях (включая сегодня) шлём сообщения,
	// дальше расписание обновляется тихо.
	NotifyWindowDays = 7

	// MaxChangeMessages — если изменилось больше дней, шлём одну сводку вместо N сообщений.
	MaxChangeMessages = 4

	// EmptyFetchLimit — сколько раз подряд пустой ответ сайта считаем сбоем, а не реальностью.
	EmptyFetchLimit = 4

	dateLayout = "02.01.2006"
)

var (
	ScheduleFile     = "schedule.json"
	ScheduleMetaFile = "schedule_meta.json"

	refreshMu        sync.Mutex
	emptyFetchStreak int
	sentChangeHashes = map[string]bool{}

	scheduleFetcher = func() ([]Lesson, error) {
		parser, err := NewScheduleParser(DefaultParserConfig())
		if err != nil {
			return nil, err
		}
		return parser.GetSchedule()
	}
)

// scheduleMeta — информация о снимке расписания (какой период он покрывает)
type scheduleMeta struct {
	FetchedAt  string `json:"fetched_at"`
	RangeStart string `json:"range_start"`
	RangeEnd   string `json:"range_end"`
}

// CrossMove — перенос пары на другой день
type CrossMove struct {
	From Lesson
	To   Lesson
}

type ReplacedLesson struct{ Old, New Lesson }
type MovedLesson struct{ Old, New Lesson }
type ModifiedLesson struct{ Old, New Lesson }

// DayChange — все изменения за один день
type DayChange struct {
	Date      string
	Added     []Lesson
	Cancelled []Lesson
	Moved     []MovedLesson    // в пределах дня
	Replaced  []ReplacedLesson // в том же слоте другой предмет
	Modified  []ModifiedLesson // изменилась аудитория / преподаватель
	MovedAway []CrossMove      // перенесена на другой день (From — здесь)
	MovedHere []CrossMove      // перенесена с другого дня (To — здесь)
	NewDay    []Lesson         // итоговое расписание дня (по времени)
	Marks     map[string]string
	// Teacher — подпись преподавателя показываем в итоговом расписании только у изменённых пар
	TeacherShown map[string]bool
}

func (d *DayChange) Count() int {
	return len(d.Added) + len(d.Cancelled) + len(d.Moved) + len(d.Replaced) +
		len(d.Modified) + len(d.MovedAway) + len(d.MovedHere)
}

func lessonKey(l Lesson) string { return l.TimeStart + "|" + l.Subject }

func sameSubject(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func lessonsByDate(lessons []Lesson) map[string][]Lesson {
	m := map[string][]Lesson{}
	for _, l := range lessons {
		m[l.Date] = append(m[l.Date], l)
	}
	for d := range m {
		ls := m[d]
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].TimeStart < ls[j].TimeStart })
		m[d] = ls
	}
	return m
}

// diffDay сравнивает расписание одного дня
func diffDay(date string, oldDay, newDay []Lesson) DayChange {
	ch := DayChange{Date: date, NewDay: newDay, Marks: map[string]string{}, TeacherShown: map[string]bool{}}

	oldUsed := make([]bool, len(oldDay))
	newUsed := make([]bool, len(newDay))

	// 1. Совпадения по слоту + предмету (возможно, изменились аудитория / преподаватель)
	for ni, n := range newDay {
		for oi, o := range oldDay {
			if oldUsed[oi] || o.TimeStart != n.TimeStart || !sameSubject(o.Subject, n.Subject) {
				continue
			}
			oldUsed[oi], newUsed[ni] = true, true
			if o.Room != n.Room || o.Teacher != n.Teacher {
				ch.Modified = append(ch.Modified, ModifiedLesson{Old: o, New: n})
				ch.Marks[lessonKey(n)] = "🔄"
				if o.Teacher != n.Teacher {
					ch.TeacherShown[lessonKey(n)] = true
				}
			}
			break
		}
	}

	var removed, added []Lesson
	for oi, o := range oldDay {
		if !oldUsed[oi] {
			removed = append(removed, o)
		}
	}
	for ni, n := range newDay {
		if !newUsed[ni] {
			added = append(added, n)
		}
	}

	// 2. Перенос в пределах дня: тот же предмет, другое время
	remRemoved := []Lesson{}
	for _, r := range removed {
		idx := -1
		for i, a := range added {
			if sameSubject(a.Subject, r.Subject) {
				idx = i
				break
			}
		}
		if idx < 0 {
			remRemoved = append(remRemoved, r)
			continue
		}
		a := added[idx]
		added = append(added[:idx], added[idx+1:]...)
		ch.Moved = append(ch.Moved, MovedLesson{Old: r, New: a})
		ch.Marks[lessonKey(a)] = "🔄"
		ch.TeacherShown[lessonKey(a)] = r.Teacher != a.Teacher
	}
	removed = remRemoved

	// 3. Замена: в том же слоте другой предмет
	remRemoved = []Lesson{}
	for _, r := range removed {
		idx := -1
		for i, a := range added {
			if a.TimeStart == r.TimeStart {
				idx = i
				break
			}
		}
		if idx < 0 {
			remRemoved = append(remRemoved, r)
			continue
		}
		a := added[idx]
		added = append(added[:idx], added[idx+1:]...)
		ch.Replaced = append(ch.Replaced, ReplacedLesson{Old: r, New: a})
		ch.Marks[lessonKey(a)] = "🟥"
		ch.TeacherShown[lessonKey(a)] = true
	}
	removed = remRemoved

	ch.Cancelled = removed
	ch.Added = added
	for _, a := range added {
		ch.Marks[lessonKey(a)] = "🟥"
		ch.TeacherShown[lessonKey(a)] = true
	}
	return ch
}

// DiffSchedules сравнивает старое и новое расписание.
// Учитываются только даты из окна [today, today+windowDays), которые покрывал старый снимок.
func DiffSchedules(oldL, newL []Lesson, meta scheduleMeta, today time.Time, windowDays int) []DayChange {
	loc := moscowLocation()
	d0 := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	winEnd := d0.AddDate(0, 0, windowDays)

	rs, err1 := time.ParseInLocation(dateLayout, meta.RangeStart, loc)
	re, err2 := time.ParseInLocation(dateLayout, meta.RangeEnd, loc)
	if err1 != nil || err2 != nil {
		return nil
	}

	oldBy := lessonsByDate(oldL)
	newBy := lessonsByDate(newL)

	dates := map[string]bool{}
	for d := range oldBy {
		dates[d] = true
	}
	for d := range newBy {
		dates[d] = true
	}

	var changes []DayChange
	for ds := range dates {
		d, err := time.ParseInLocation(dateLayout, ds, loc)
		if err != nil || d.Before(d0) || !d.Before(winEnd) || d.Before(rs) || d.After(re) {
			continue
		}
		ch := diffDay(ds, oldBy[ds], newBy[ds])
		if ch.Count() > 0 {
			changes = append(changes, ch)
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		a, _ := time.Parse(dateLayout, changes[i].Date)
		b, _ := time.Parse(dateLayout, changes[j].Date)
		return a.Before(b)
	})

	linkCrossDayMoves(changes)
	return changes
}

// linkCrossDayMoves находит пары, "отменённые" в один день и "добавленные" в другой с тем же
// названием, и превращает их в переносы между днями.
func linkCrossDayMoves(changes []DayChange) {
	for i := range changes {
		var keepCancelled []Lesson
		for _, c := range changes[i].Cancelled {
			found := false
			for j := range changes {
				if j == i {
					continue
				}
				for k, a := range changes[j].Added {
					if !sameSubject(a.Subject, c.Subject) {
						continue
					}
					changes[j].Added = append(changes[j].Added[:k], changes[j].Added[k+1:]...)
					changes[j].MovedHere = append(changes[j].MovedHere, CrossMove{From: c, To: a})
					changes[j].Marks[lessonKey(a)] = "🔄"
					changes[i].MovedAway = append(changes[i].MovedAway, CrossMove{From: c, To: a})
					found = true
					break
				}
				if found {
					break
				}
			}
			if !found {
				keepCancelled = append(keepCancelled, c)
			}
		}
		changes[i].Cancelled = keepCancelled
	}
}

// ---------- форматирование ----------

var weekdayAccusative = []string{"Воскресенье", "Понедельник", "Вторник", "Среду", "Четверг", "Пятницу", "Субботу"}
var weekdayShort = []string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}

func lessonLabel(l Lesson) string {
	if l.LessonNumber != "" && l.LessonNumber != "0" {
		return l.LessonNumber + " пара"
	}
	return l.TimeStart
}

func fmtRoomPart(room string) string {
	room = strings.TrimSpace(room)
	if room == "" {
		return ""
	}
	if isDistanceLearning(room) {
		return "<i>(" + escapeHTML(room) + ")</i>"
	}
	return "<i>(ауд. " + escapeHTML(room) + ")</i>"
}

func roomOrDash(room string) string {
	if strings.TrimSpace(room) == "" {
		return "—"
	}
	return escapeHTML(room)
}

func withSpace(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

func dayRelative(date string, now time.Time) string {
	loc := moscowLocation()
	d, err := time.ParseInLocation(dateLayout, date, loc)
	if err != nil {
		return ""
	}
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	switch int(d.Sub(today).Hours() / 24) {
	case 0:
		return " · сегодня"
	case 1:
		return " · завтра"
	}
	return ""
}

func shortDate(date string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return date
	}
	return fmt.Sprintf("%s %s", weekdayShort[d.Weekday()], d.Format("02.01"))
}

// FormatDayChange формирует сообщение об изменениях за день
func FormatDayChange(ch DayChange, now time.Time) string {
	d, err := time.Parse(dateLayout, ch.Date)
	dayName, short := "", ch.Date
	if err == nil {
		dayName = weekdayAccusative[d.Weekday()]
		short = d.Format("02.01")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ <b>Изменения в расписании на %s (%s)</b>%s\n\n", dayName, short, dayRelative(ch.Date, now))
	b.WriteString("<b>Что изменилось:</b>\n")

	for _, l := range ch.Added {
		fmt.Fprintf(&b, "🟥 + Добавлена: <code>%s</code> — %s%s\n", lessonLabel(l), escapeHTML(l.Subject), withSpace(fmtRoomPart(l.Room)))
		if l.Teacher != "" {
			fmt.Fprintf(&b, "      👨‍🏫 %s\n", escapeHTML(l.Teacher))
		}
	}
	for _, l := range ch.Cancelled {
		fmt.Fprintf(&b, "🟩 − Отменена: <code>%s</code> — %s\n", lessonLabel(l), escapeHTML(l.Subject))
	}
	for _, m := range ch.Moved {
		fmt.Fprintf(&b, "🔄 Перенос: %s — <code>%s</code> → <code>%s</code>%s\n",
			escapeHTML(m.New.Subject), lessonLabel(m.Old), lessonLabel(m.New), roomChangePart(m.Old, m.New))
		if m.Old.Teacher != m.New.Teacher {
			fmt.Fprintf(&b, "      👨‍🏫 %s → %s\n", teacherOrDash(m.Old.Teacher), teacherOrDash(m.New.Teacher))
		}
	}
	for _, m := range ch.MovedAway {
		fmt.Fprintf(&b, "🔄 Перенос на другой день: %s — <code>%s</code> → <b>%s</b>, <code>%s</code>\n",
			escapeHTML(m.From.Subject), lessonLabel(m.From), shortDate(m.To.Date), lessonLabel(m.To))
	}
	for _, m := range ch.MovedHere {
		fmt.Fprintf(&b, "🔄 Перенос с другого дня: %s — с <b>%s</b> (<code>%s</code>) → <code>%s</code>%s\n",
			escapeHTML(m.To.Subject), shortDate(m.From.Date), lessonLabel(m.From), lessonLabel(m.To), withSpace(fmtRoomPart(m.To.Room)))
	}
	for _, r := range ch.Replaced {
		fmt.Fprintf(&b, "🟥 Замена: <code>%s</code> — %s → %s%s\n",
			lessonLabel(r.New), escapeHTML(r.Old.Subject), escapeHTML(r.New.Subject), withSpace(fmtRoomPart(r.New.Room)))
		if r.New.Teacher != "" {
			fmt.Fprintf(&b, "      👨‍🏫 %s\n", escapeHTML(r.New.Teacher))
		}
	}
	for _, m := range ch.Modified {
		parts := []string{}
		if m.Old.Room != m.New.Room {
			parts = append(parts, fmt.Sprintf("ауд. %s → %s", roomOrDash(m.Old.Room), roomOrDash(m.New.Room)))
		}
		if m.Old.Teacher != m.New.Teacher {
			parts = append(parts, fmt.Sprintf("преподаватель %s → %s", teacherOrDash(m.Old.Teacher), teacherOrDash(m.New.Teacher)))
		}
		fmt.Fprintf(&b, "🔄 Изменено: <code>%s</code> — %s: %s\n", lessonLabel(m.New), escapeHTML(m.New.Subject), strings.Join(parts, "; "))
	}

	b.WriteString("\n━━━━━━━━━━━━━━━━\n")
	b.WriteString("📋 <b>Итоговое расписание:</b>\n")
	if len(ch.NewDay) == 0 {
		b.WriteString("<i>Пар нет 🎉</i>")
		return b.String()
	}
	for _, l := range ch.NewDay {
		k := lessonKey(l)
		fmt.Fprintf(&b, "<code>%s %s–%s</code> %s", lessonLabel(l), l.TimeStart, l.TimeEnd, escapeHTML(l.Subject))
		if mark := ch.Marks[k]; mark != "" {
			b.WriteString(" " + mark)
		}
		b.WriteString(withSpace(fmtRoomPart(l.Room)))
		b.WriteString("\n")
		if ch.TeacherShown[k] && l.Teacher != "" {
			fmt.Fprintf(&b, "      👨‍🏫 %s\n", escapeHTML(l.Teacher))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func teacherOrDash(t string) string {
	if strings.TrimSpace(t) == "" {
		return "—"
	}
	return escapeHTML(t)
}

func roomChangePart(o, n Lesson) string {
	if o.Room == n.Room {
		return withSpace(fmtRoomPart(n.Room))
	}
	return fmt.Sprintf(" <i>(ауд. %s → %s)</i>", roomOrDash(o.Room), roomOrDash(n.Room))
}

// FormatChangesSummary — короткая сводка, когда изменений слишком много
func FormatChangesSummary(changes []DayChange) string {
	var b strings.Builder
	b.WriteString("⚠️ <b>Массовые изменения в расписании</b>\n\n")
	for _, ch := range changes {
		fmt.Fprintf(&b, "• <b>%s</b> — изменений: %d\n", shortDate(ch.Date), ch.Count())
	}
	b.WriteString("\n📚 Посмотреть подробно: /schedule")
	return b.String()
}

// ---------- хранение ----------

func loadScheduleMeta() (scheduleMeta, bool) {
	var meta scheduleMeta
	data, err := ioutil.ReadFile(ScheduleMetaFile)
	if err != nil {
		return meta, false
	}
	if err := json.Unmarshal(data, &meta); err != nil || meta.RangeStart == "" || meta.RangeEnd == "" {
		return meta, false
	}
	return meta, true
}

func saveSchedule(lessons []Lesson, meta scheduleMeta) error {
	data, err := json.MarshalIndent(lessons, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(ScheduleFile, data); err != nil {
		return err
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(ScheduleMetaFile, metaData)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := ioutil.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------- обновление ----------

// shouldRefreshSchedule: днём (07–23 МСК) раз в 15 минут, ночью раз в час
func shouldRefreshSchedule(nowMoscow, last time.Time) bool {
	interval := 60 * time.Minute
	if h := nowMoscow.Hour(); h >= 7 && h < 23 {
		interval = 15 * time.Minute
	}
	return nowMoscow.Sub(last) >= interval-10*time.Second
}

// UpdateSchedule загружает расписание и уведомляет об изменениях
func (bot *TimetableBot) UpdateSchedule() {
	if err := bot.RefreshSchedule(); err != nil {
		logError("UpdateSchedule: %v", err)
	}
}

// RefreshSchedule скачивает расписание, сравнивает со старым и шлёт уведомления
func (bot *TimetableBot) RefreshSchedule() error {
	if !refreshMu.TryLock() {
		return fmt.Errorf("обновление уже выполняется")
	}
	defer refreshMu.Unlock()

	newLessons, err := scheduleFetcher()
	if err != nil {
		return fmt.Errorf("не удалось получить расписание: %w", err)
	}
	logInfo("RefreshSchedule: получено %d пар", len(newLessons))

	oldLessons := bot.schedule

	if len(newLessons) == 0 && len(oldLessons) > 0 {
		emptyFetchStreak++
		if emptyFetchStreak < EmptyFetchLimit {
			return fmt.Errorf("сайт вернул пустое расписание (%d/%d), считаю сбоем и не обновляю", emptyFetchStreak, EmptyFetchLimit)
		}
		logInfo("RefreshSchedule: пустой ответ %d раз подряд — принимаю как есть", emptyFetchStreak)
	}
	emptyFetchStreak = 0

	loc := moscowLocation()
	now := time.Now().In(loc)

	var changes []DayChange
	if meta, ok := loadScheduleMeta(); ok {
		changes = DiffSchedules(oldLessons, newLessons, meta, now, NotifyWindowDays)
	} else {
		logInfo("RefreshSchedule: нет schedule_meta.json — сохраняю базовый снимок без уведомлений")
	}

	if len(changes) > 0 {
		var messages []string
		if len(changes) > MaxChangeMessages {
			messages = []string{FormatChangesSummary(changes)}
		} else {
			for _, ch := range changes {
				messages = append(messages, FormatDayChange(ch, now))
			}
		}
		allSent := true
		for _, msg := range messages {
			if sentChangeHashes[msg] {
				continue
			}
			if err := bot.SendMessage(msg); err != nil {
				logError("RefreshSchedule: не удалось отправить уведомление об изменениях: %v", err)
				allSent = false
				continue
			}
			sentChangeHashes[msg] = true
		}
		if !allSent {
			return fmt.Errorf("уведомления отправлены не полностью, повторю при следующей проверке")
		}
	}
	sentChangeHashes = map[string]bool{}

	start, end := generateDateRange()
	meta := scheduleMeta{FetchedAt: now.Format(time.RFC3339), RangeStart: start, RangeEnd: end}
	if err := saveSchedule(newLessons, meta); err != nil {
		return fmt.Errorf("не удалось сохранить расписание: %w", err)
	}
	bot.schedule = newLessons
	logInfo("RefreshSchedule: расписание обновлено, дней с изменениями: %d", len(changes))
	return nil
}
