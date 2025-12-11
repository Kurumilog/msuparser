package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
)

func main() {
	// Конфигурация для группы 303 (пример из ТЗ)
	config := ParserConfig{
		FacultyID: 3,
		Course:    3,
		GroupID:   52,
	}

	// Создаем парсер
	parser, err := NewScheduleParser(config)
	if err != nil {
		log.Fatalf("Ошибка создания парсера: %v", err)
	}

	fmt.Println("📚 Получение расписания с tt.audit.msu.ru...")

	// Получаем расписание
	lessons, err := parser.GetSchedule()
	if err != nil {
		log.Fatalf("❌ Ошибка получения расписания: %v", err)
	}

	fmt.Printf("✅ Найдено занятий: %d\n\n", len(lessons))

	// Сохраняем в schedule.json для бота
	jsonData, err := json.MarshalIndent(lessons, "", "  ")
	if err != nil {
		log.Fatalf("❌ Ошибка маршалинга JSON: %v", err)
	}

	err = ioutil.WriteFile("schedule.json", jsonData, 0644)
	if err != nil {
		log.Fatalf("❌ Ошибка сохранения в schedule.json: %v", err)
	}

	fmt.Println("💾 Расписание сохранено в schedule.json")

	// Выводим в читаемом формате
	fmt.Println("\n=== Расписание ===\n")
	currentDate := ""
	for i, lesson := range lessons {
		// Печатаем заголовок даты
		if lesson.Date != currentDate {
			currentDate = lesson.Date
			fmt.Printf("\n📅 %s (%s)\n", lesson.Date, lesson.Weekday)
			fmt.Println(strings.Repeat("=", 50))
		}

		fmt.Printf("%d. %s пара (%s - %s)\n", i+1, lesson.LessonNumber, lesson.TimeStart, lesson.TimeEnd)
		fmt.Printf("   📚 %s\n", lesson.Subject)
		if lesson.Teacher != "" {
			fmt.Printf("   👨‍🏫 %s\n", lesson.Teacher)
		}
		if lesson.Room != "" {
			fmt.Printf("   🚪 %s\n", lesson.Room)
		}
		fmt.Println()
	}

	fmt.Println("\n✅ Готово! Бот может использовать schedule.json")
}
