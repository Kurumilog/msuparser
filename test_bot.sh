#!/bin/bash

# Скрипт для тестирования Go бота без отправки реальных Telegram сообщений
# Использование: ./test_bot.sh

set -e

echo "🧪 Тестирование Go бота"
echo "======================"

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Проверяем наличие бинарника
if [ ! -f "$PROJECT_DIR/bot" ]; then
    echo "❌ Go бот не скомпилирован!"
    echo "💡 Компилируй: go build -o bot main.go"
    exit 1
fi

# Проверяем наличие schedule.json
if [ ! -f "$PROJECT_DIR/schedule.json" ]; then
    echo "❌ Расписание не найдено!"
    echo "💡 Запусти парсер: python parser_full.py"
    exit 1
fi

# Проверяем структуру schedule.json
echo "📋 Проверяю schedule.json..."
python3 << 'EOF'
import json

try:
    with open('schedule.json', 'r', encoding='utf-8') as f:
        schedule = json.load(f)
    
    if not schedule:
        print("⚠️  Расписание пустое!")
        exit(1)
    
    print(f"✅ Расписание загружено: {len(schedule)} пар")
    
    # Проверяем структуру первого элемента
    first = schedule[0]
    required_fields = ['subject', 'teacher', 'room', 'lesson_number', 
                      'time_start', 'time_end', 'date', 'weekday']
    
    for field in required_fields:
        if field not in first:
            print(f"❌ Отсутствует поле: {field}")
            exit(1)
    
    print("✅ Структура расписания корректна")
    
    # Показываем примеры
    print("\n📚 Примеры из расписания:")
    for i, lesson in enumerate(schedule[:3], 1):
        print(f"\n{i}. {lesson['date']} - {lesson['subject']}")
        print(f"   Время: {lesson['time_start']}-{lesson['time_end']}")
        print(f"   Преподаватель: {lesson['teacher']}")
        
except json.JSONDecodeError as e:
    print(f"❌ JSON ошибка: {e}")
    exit(1)
except Exception as e:
    print(f"❌ Ошибка: {e}")
    exit(1)
EOF

echo ""
echo "📝 Проверяю конфиг..."

# Проверяем конфиг Go
python3 << 'EOF'
import re

config_content = open('main.go', 'r').read()

# Ищем токен
if re.search(r'BOT_TOKEN = "[0-9]', config_content):
    print("✅ BOT_TOKEN установлен")
else:
    print("⚠️  BOT_TOKEN похож на заглушку")

# Ищем USER_ID
if re.search(r'USER_ID = "[0-9]{6,}', config_content):
    print("✅ USER_ID установлен")
else:
    print("⚠️  USER_ID похож на заглушку")
EOF

echo ""
echo "✅ Тестирование завершено успешно!"
echo ""
echo "💡 Для реального запуска бота с отправкой сообщений:"
echo "   ./bot"
echo ""
