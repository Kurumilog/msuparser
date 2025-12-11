package main

// Пример интеграции парсера с существующим кодом

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
)

// ExampleBasicUsage демонстрирует базовое использование парсера
func ExampleBasicUsage() {
	config := ParserConfig{
		FacultyID: 3,
		Course:    3,
		GroupID:   52,
	}

	parser, err := NewScheduleParser(config)
	if err != nil {
		log.Fatal(err)
	}

	lessons, err := parser.GetSchedule()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Найдено %d занятий\n", len(lessons))
	for _, lesson := range lessons {
		fmt.Printf("%s %s: %s\n", lesson.Date, lesson.Time, lesson.Name)
	}
}

// ExampleSaveToJSON демонстрирует сохранение в JSON файл
func ExampleSaveToJSON() {
	config := ParserConfig{
		FacultyID: 3,
		Course:    3,
		GroupID:   52,
	}

	parser, err := NewScheduleParser(config)
	if err != nil {
		log.Fatal(err)
	}

	lessons, err := parser.GetSchedule()
	if err != nil {
		log.Fatal(err)
	}

	// Сохраняем в JSON
	data, err := json.MarshalIndent(lessons, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile("schedule.json", data, 0644)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Расписание сохранено в schedule.json")
}

// ExampleFilterByDate демонстрирует фильтрацию по дате
func ExampleFilterByDate(targetDate string) {
	config := ParserConfig{
		FacultyID: 3,
		Course:    3,
		GroupID:   52,
	}

	parser, err := NewScheduleParser(config)
	if err != nil {
		log.Fatal(err)
	}

	lessons, err := parser.GetSchedule()
	if err != nil {
		log.Fatal(err)
	}

	// Фильтруем по дате
	var filtered []Lesson
	for _, lesson := range lessons {
		if lesson.Date == targetDate {
			filtered = append(filtered, lesson)
		}
	}

	fmt.Printf("Занятий на %s: %d\n", targetDate, len(filtered))
	for _, lesson := range filtered {
		fmt.Printf("%s - %s [%s] ауд.%s\n",
			lesson.Time, lesson.Name, lesson.Type, lesson.Room)
	}
}

// ExampleGroupByDate демонстрирует группировку по датам
func ExampleGroupByDate() {
	config := ParserConfig{
		FacultyID: 3,
		Course:    3,
		GroupID:   52,
	}

	parser, err := NewScheduleParser(config)
	if err != nil {
		log.Fatal(err)
	}

	lessons, err := parser.GetSchedule()
	if err != nil {
		log.Fatal(err)
	}

	// Группируем по датам
	byDate := make(map[string][]Lesson)
	for _, lesson := range lessons {
		byDate[lesson.Date] = append(byDate[lesson.Date], lesson)
	}

	// Выводим по датам
	for date, dayLessons := range byDate {
		fmt.Printf("\n=== %s ===\n", date)
		for _, lesson := range dayLessons {
			fmt.Printf("%s: %s\n", lesson.Time, lesson.Name)
		}
	}
}

// ExampleMultipleGroups демонстрирует получение расписания для нескольких групп
func ExampleMultipleGroups() {
	groups := []ParserConfig{
		{FacultyID: 3, Course: 3, GroupID: 52},
		{FacultyID: 3, Course: 3, GroupID: 53},
	}

	for i, config := range groups {
		fmt.Printf("\n=== Группа %d ===\n", i+1)

		parser, err := NewScheduleParser(config)
		if err != nil {
			log.Printf("Ошибка для группы %d: %v", i+1, err)
			continue
		}

		lessons, err := parser.GetSchedule()
		if err != nil {
			log.Printf("Ошибка получения расписания для группы %d: %v", i+1, err)
			continue
		}

		fmt.Printf("Занятий: %d\n", len(lessons))
	}
}

// ExampleConvertToExistingFormat демонстрирует конвертацию в формат существующего Lesson struct
func ExampleConvertToExistingFormat(lessons []Lesson) {
	// Конвертируем в формат из main.go, если нужно
	for _, lesson := range lessons {
		// Пример маппинга:
		// Subject = lesson.Name
		// Teacher = lesson.Teacher
		// Room = lesson.Room
		// TimeStart и TimeEnd можно извлечь из lesson.Time
		// Date = lesson.Date
		// Weekday = lesson.DayOfWeek

		fmt.Printf("Subject: %s, Teacher: %s, Room: %s\n",
			lesson.Name, lesson.Teacher, lesson.Room)
	}
}
