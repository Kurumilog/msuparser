#!/bin/bash

# Скрипт первичной настройки проекта
# Использование: ./setup.sh

set -e

echo "🔧 MSU Timetable Bot - Первичная настройка"
echo "=========================================="

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Проверяем Python
echo ""
echo "1️⃣  Проверяю Python..."
if ! command -v python3 &> /dev/null; then
    echo "❌ Python3 не найден! Установи Python 3.8+"
    exit 1
fi

PYTHON_VERSION=$(python3 --version)
echo "   ✅ $PYTHON_VERSION"

# Проверяем Go
echo ""
echo "2️⃣  Проверяю Go..."
if ! command -v go &> /dev/null; then
    echo "❌ Go не найден! Установи Go 1.21+"
    exit 1
fi

GO_VERSION=$(go version)
echo "   ✅ $GO_VERSION"

# Создаём виртуальное окружение
echo ""
echo "3️⃣  Создаю виртуальное окружение Python..."
if [ -d "$PROJECT_DIR/venv" ]; then
    echo "   ⚠️  Виртуальное окружение уже существует, пропускаю"
else
    python3 -m venv "$PROJECT_DIR/venv"
    echo "   ✅ Виртуальное окружение создано"
fi

# Активируем venv и устанавливаем зависимости
echo ""
echo "4️⃣  Устанавливаю зависимости Python..."
source "$PROJECT_DIR/venv/bin/activate"
pip install --upgrade pip > /dev/null 2>&1
pip install -r "$PROJECT_DIR/requirements.txt"
echo "   ✅ Зависимости установлены"

# Компилируем Go бот
echo ""
echo "5️⃣  Компилирую Go бот..."
cd "$PROJECT_DIR"
go build -o bot main.go
echo "   ✅ Go бот скомпилирован"

# Проверяем конфиг
echo ""
echo "6️⃣  Проверяю конфигурацию..."
if [ -f "$PROJECT_DIR/config.py" ]; then
    echo "   ✅ config.py найден"
    
    if grep -q "ТУТ_ТВОЙ_ТОКЕН\|ТУТ_ТВОЙ_ID" "$PROJECT_DIR/config.py"; then
        echo "   ⚠️  config.py содержит заглушки, обнови его!"
    fi
else
    echo "   ⚠️  config.py не найден"
    echo "   💡 Скопируй config.example.py в config.py и заполни данные"
    cp "$PROJECT_DIR/config.example.py" "$PROJECT_DIR/config.py"
    echo "   📝 Создан config.py из примера (заполни токен и ID)"
fi

deactivate

# Создаём необходимые директории
echo ""
echo "7️⃣  Создаю директории..."
mkdir -p "$PROJECT_DIR/logs"
echo "   ✅ Директории созданы"

echo ""
echo "=========================================="
echo "✅ Первичная настройка завершена!"
echo ""
echo "📋 Следующие шаги:"
echo "   1. Отредактируй config.py с твоими данными Telegram"
echo "   2. Запусти парсер: python parser_full.py"
echo "   3. Запусти бота: ./bot"
echo ""
echo "📖 Или используй скрипты:"
echo "   • Быстрый запуск:   ./start.sh"
echo "   • Установить systemd: ./install_services.sh"
echo "   • Прочитать README:  cat README.md"
echo ""
