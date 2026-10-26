package main

import (
	"testing"
	"time"
)

func TestDefaultParserConfig(t *testing.T) {
	cfg := DefaultParserConfig()
	if cfg.FacultyID != 3 {
		t.Errorf("expected FacultyID 3, got %d", cfg.FacultyID)
	}
	if cfg.Course != 3 {
		t.Errorf("expected Course 3, got %d", cfg.Course)
	}
	if cfg.GroupID != 52 {
		t.Errorf("expected GroupID 52, got %d", cfg.GroupID)
	}
}

func TestGenerateDateRange(t *testing.T) {
	startStr, endStr := generateDateRange()

	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.Local
	}

	start, err := time.ParseInLocation("02.01.2006", startStr, loc)
	if err != nil {
		t.Fatalf("failed to parse start date %q: %v", startStr, err)
	}
	end, err := time.ParseInLocation("02.01.2006", endStr, loc)
	if err != nil {
		t.Fatalf("failed to parse end date %q: %v", endStr, err)
	}

	diffDays := int(end.Sub(start).Hours() / 24)
	if diffDays != ScheduleDays {
		t.Errorf("expected date range of %d days, got %d (%s to %s)", ScheduleDays, diffDays, startStr, endStr)
	}

	now := time.Now().In(loc)
	expectedStart := now.Format("02.01.2006")
	if startStr != expectedStart {
		t.Errorf("expected start date %s, got %s", expectedStart, startStr)
	}
}

func TestParseLessonData(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantName   string
		wantType   string
		wantRoom   string
		wantTeach  string
	}{
		{
			name:      "standard lesson",
			content:   "Математика [лек]<br>ауд. 205<br>Иванов И.И.",
			wantName:  "Математика",
			wantType:  "лек",
			wantRoom:  "205",
			wantTeach: "Иванов И.И.",
		},
		{
			name:      "with time prefix",
			content:   "12:30-14:00<br>Английский язык [сем]<br>ауд. 412<br>Smith J.",
			wantName:  "Английский язык",
			wantType:  "сем",
			wantRoom:  "412",
			wantTeach: "Smith J.",
		},
		{
			name:      "no room prefix and skipping added line",
			content:   "Физика [лаб]<br>101<br>Добавлено: 12.09<br>Сидоров С.С.",
			wantName:  "Физика",
			wantType:  "лаб",
			wantRoom:  "101",
			wantTeach: "Сидоров С.С.",
		},
		{
			name:      "skipping group number",
			content:   "Химия [лек]<br>ауд. 301<br>303<br>Петров П.П.",
			wantName:  "Химия",
			wantType:  "лек",
			wantRoom:  "301",
			wantTeach: "Петров П.П.",
		},
		{
			name:      "no bracket type",
			content:   "Физкультура<br>спортзал<br>Кузнецов К.К.",
			wantName:  "Физкультура",
			wantType:  "",
			wantRoom:  "спортзал",
			wantTeach: "Кузнецов К.К.",
		},
		{
			name:      "empty content",
			content:   "",
			wantName:  "",
			wantType:  "",
			wantRoom:  "",
			wantTeach: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotType, gotRoom, gotTeach := parseLessonData(tt.content)
			if gotName != tt.wantName {
				t.Errorf("parseLessonData() gotName = %v, want %v", gotName, tt.wantName)
			}
			if gotType != tt.wantType {
				t.Errorf("parseLessonData() gotType = %v, want %v", gotType, tt.wantType)
			}
			if gotRoom != tt.wantRoom {
				t.Errorf("parseLessonData() gotRoom = %v, want %v", gotRoom, tt.wantRoom)
			}
			if gotTeach != tt.wantTeach {
				t.Errorf("parseLessonData() gotTeach = %v, want %v", gotTeach, tt.wantTeach)
			}
		})
	}
}
