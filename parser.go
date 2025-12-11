package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Lesson представляет одну пару в расписании
type Lesson struct {
	Subject      string    `json:"subject"`
	Teacher      string    `json:"teacher"`
	Room         string    `json:"room"`
	LessonNumber string    `json:"lesson_number"`
	TimeStart    string    `json:"time_start"`
	TimeEnd      string    `json:"time_end"`
	Date         string    `json:"date"`
	Weekday      string    `json:"weekday"`
	Group        string    `json:"group"`
	Notification time.Time `json:"-"` // Для бота
}

// ParserConfig содержит конфигурацию для парсера
type ParserConfig struct {
	FacultyID int
	Course    int
	GroupID   int
}

// ScheduleParser парсер расписания
type ScheduleParser struct {
	client  *http.Client
	config  ParserConfig
	baseURL string
}

// NewScheduleParser создает новый экземпляр парсера
func NewScheduleParser(config ParserConfig) (*ScheduleParser, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать cookiejar: %w", err)
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	return &ScheduleParser{
		client:  client,
		config:  config,
		baseURL: "https://tt.audit.msu.ru",
	}, nil
}

// getCSRFToken получает CSRF токен со страницы
func (p *ScheduleParser) getCSRFToken() (string, error) {
	url := p.baseURL + "/time-table/group"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неожиданный статус код: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML: %w", err)
	}

	// Попробуем найти токен в input
	token, exists := doc.Find("input[name='_csrf-frontend']").Attr("value")
	if exists && token != "" {
		return token, nil
	}

	// Попробуем найти токен в meta
	token, exists = doc.Find("meta[name='csrf-token']").Attr("content")
	if exists && token != "" {
		return token, nil
	}

	return "", fmt.Errorf("CSRF токен не найден")
}

// generateDateRange генерирует даты для запроса (текущая дата + 1 месяц)
func generateDateRange() (startDate, endDate string) {
	now := time.Now()
	start := now.Format("02.01.2006")
	end := now.AddDate(0, 1, 0).Format("02.01.2006")
	return start, end
}

// fetchSchedule выполняет POST запрос и возвращает HTML с расписанием
func (p *ScheduleParser) fetchSchedule(csrfToken string) (io.ReadCloser, error) {
	startDate, endDate := generateDateRange()

	// Формируем payload
	data := url.Values{}
	data.Set("_csrf-frontend", csrfToken)
	data.Set("TimeTableForm[facultyId]", fmt.Sprintf("%d", p.config.FacultyID))
	data.Set("TimeTableForm[course]", fmt.Sprintf("%d", p.config.Course))
	data.Set("TimeTableForm[groupId]", fmt.Sprintf("%d", p.config.GroupID))
	data.Set("date-picker", fmt.Sprintf("%s - %s", startDate, endDate))
	data.Set("TimeTableForm[dateStart]", startDate)
	data.Set("TimeTableForm[dateEnd]", endDate)
	data.Set("TimeTableForm[indicationDays]", "5")
	data.Set("time-table-type", "0")

	reqURL := p.baseURL + "/time-table/group?type=0"
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", p.baseURL+"/time-table/group")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("неожиданный статус код: %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// parseLessonData парсит данные из атрибута data-content
func parseLessonData(dataContent string) (name, lessonType, room, teacher string) {
	// Разбиваем по <br>
	parts := strings.Split(dataContent, "<br>")

	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}

	// Index 0: Название предмета и тип занятия
	if len(parts) > 0 {
		firstPart := parts[0]
		// Ищем тип занятия в квадратных скобках
		re := regexp.MustCompile(`^(.+?)\[(.+?)\]`)
		matches := re.FindStringSubmatch(firstPart)
		if len(matches) >= 3 {
			name = strings.TrimSpace(matches[1])
			lessonType = strings.TrimSpace(matches[2])
		} else {
			name = firstPart
		}
	}

	// Index 1: Аудитория (удаляем префикс "ауд.")
	if len(parts) > 1 {
		room = strings.TrimSpace(parts[1])
		room = strings.TrimPrefix(room, "ауд.")
		room = strings.TrimSpace(room)
	}

	// Ищем преподавателя - он не содержит "Добавлено:", "ауд.", цифры группы
	for i := 2; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		// Пропускаем пустые, "Добавлено:", номера групп (только цифры)
		if part == "" || strings.HasPrefix(part, "Добавлено:") {
			continue
		}
		// Пропускаем если это только цифры (номер группы)
		if regexp.MustCompile(`^\d+$`).MatchString(part) {
			continue
		}
		// Если содержит буквы (кириллицу или латиницу) и пробелы - это преподаватель
		if regexp.MustCompile(`[А-Яа-яA-Za-z]`).MatchString(part) {
			teacher = part
			break
		}
	}

	return
}

// parseSchedule парсит HTML и извлекает расписание
func (p *ScheduleParser) parseSchedule(body io.ReadCloser) ([]Lesson, error) {
	defer body.Close()

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга HTML: %w", err)
	}

	var lessons []Lesson

	// Маппинг времени пар и номеров
	timeToLesson := map[string]string{
		"09:00": "1",
		"10:45": "2",
		"13:00": "3",
		"14:45": "4",
		"16:30": "5",
	}

	// Проходим по всем tr в таблице
	doc.Find("#timeTable tr").Each(func(rowIndex int, row *goquery.Selection) {
		// Проверяем, является ли это строкой заголовка с датами
		headday := row.Find("th.headday")
		if headday.Length() > 0 {
			// Это строка с датами, пропускаем
			return
		}

		// Проверяем наличие th.headcol (время пары)
		timeCell := row.Find("th.headcol")
		if timeCell.Length() == 0 {
			return
		}

		// Извлекаем время
		timeStart := strings.TrimSpace(timeCell.Find("span.start").Text())
		timeEnd := strings.TrimSpace(timeCell.Find("span.end").Text())

		if timeStart == "" || timeEnd == "" {
			return
		}

		// Определяем номер пары
		lessonNumber := timeToLesson[timeStart]
		if lessonNumber == "" {
			lessonNumber = "0"
		}

		// Проходим по всем td в этой строке
		row.Find("td").Each(func(cellIndex int, cell *goquery.Selection) {
			// Ищем div с data-content
			popover := cell.Find("div[data-toggle='popover']")
			if popover.Length() == 0 {
				return
			}

			dataContent, exists := popover.Attr("data-content")
			if !exists || dataContent == "" {
				return
			}

			// Извлекаем дату из title атрибута
			title, _ := popover.Attr("title")
			// Формат title: "15.12.2025 1 пара"
			titleParts := strings.Fields(title)
			var date, dayOfWeek string
			if len(titleParts) > 0 {
				date = titleParts[0] // "15.12.2025"
			}

			// Определяем день недели по дате
			if date != "" {
				// Парсим дату для получения дня недели
				parsedDate, err := time.Parse("02.01.2006", date)
				if err == nil {
					weekday := parsedDate.Weekday()
					weekdayNames := []string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}
					dayOfWeek = weekdayNames[weekday]
				}
			}

			// Парсим данные из data-content
			name, lessonType, room, teacher := parseLessonData(dataContent)

			// Пропускаем пустые пары
			if name == "" || strings.TrimSpace(name) == "[]" {
				return
			}

			// Формируем Subject с типом занятия для наглядности
			subject := name
			if lessonType != "" {
				subject = name + " [" + lessonType + "]"
			}

			lesson := Lesson{
				Subject:      subject,
				Teacher:      teacher,
				Room:         room,
				LessonNumber: lessonNumber,
				TimeStart:    timeStart,
				TimeEnd:      timeEnd,
				Date:         date,
				Weekday:      dayOfWeek,
				Group:        "303",
			}

			lessons = append(lessons, lesson)
		})
	})

	return lessons, nil
}

// GetSchedule получает расписание (главный метод)
func (p *ScheduleParser) GetSchedule() ([]Lesson, error) {
	// Шаг 1: Получаем CSRF токен
	csrfToken, err := p.getCSRFToken()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения CSRF токена: %w", err)
	}

	// Шаг 2: Получаем HTML с расписанием
	body, err := p.fetchSchedule(csrfToken)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения расписания: %w", err)
	}

	// Шаг 3: Парсим HTML
	lessons, err := p.parseSchedule(body)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга расписания: %w", err)
	}

	return lessons, nil
}
