#!/bin/bash

# Скрипт для установки systemd сервисов
# Использование: ./install_services.sh

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_DIR="$HOME/.config/systemd/user"

echo "📦 Установка systemd сервисов..."

# Создаем директорию если её нет
mkdir -p "$SERVICE_DIR"

# Копируем файлы сервисов
echo "📋 Копирую сервис обновления расписания..."
cp "$PROJECT_DIR/msuparser-update.service" "$SERVICE_DIR/"
cp "$PROJECT_DIR/msuparser-update.timer" "$SERVICE_DIR/"

echo "📋 Копирую сервис бота..."
cp "$PROJECT_DIR/msuparser-bot.service" "$SERVICE_DIR/"

# Перезагружаем systemd
echo "🔄 Перезагружаю systemd..."
systemctl --user daemon-reload

# Включаем сервисы
echo "✅ Включаю сервисы..."
systemctl --user enable msuparser-bot.service
systemctl --user enable msuparser-update.timer

# Стартуем сервисы
echo "🚀 Запускаю сервисы..."
systemctl --user start msuparser-bot.service
systemctl --user start msuparser-update.timer

echo ""
echo "========================================="
echo "✅ Установка завершена!"
echo "========================================="
echo ""
echo "📋 Полезные команды:"
echo "   • Статус бота:        systemctl --user status msuparser-bot"
echo "   • Логи бота:          journalctl --user -u msuparser-bot -f"
echo "   • Статус таймера:     systemctl --user status msuparser-update.timer"
echo "   • Логи парсера:       journalctl --user -u msuparser-update -f"
echo "   • Остановить бота:    systemctl --user stop msuparser-bot"
echo "   • Перезапустить:      systemctl --user restart msuparser-bot"
echo ""
