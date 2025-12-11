#!/bin/bash

# Скрипт автоматической установки MSU Timetable Bot на Ubuntu
# Использование: curl -sSL https://raw.githubusercontent.com/Kurumilog/msuparser/master/install.sh | bash

set -e

echo "🚀 Установка MSU Timetable Bot"
echo "================================"

# Проверка ОС
if [ ! -f /etc/os-release ]; then
    echo "❌ Не удалось определить ОС"
    exit 1
fi

. /etc/os-release
if [[ "$ID" != "ubuntu" ]] && [[ "$ID" != "debian" ]]; then
    echo "⚠️  Этот скрипт предназначен для Ubuntu/Debian"
    echo "Продолжить? (y/n)"
    read -r response
    if [[ "$response" != "y" ]]; then
        exit 1
    fi
fi

# Проверка Go
echo "📦 Проверка Go..."
if ! command -v go &> /dev/null; then
    echo "📥 Установка Go 1.24.0..."
    wget -q https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
    rm go1.24.0.linux-amd64.tar.gz
    
    # Добавляем в PATH
    if ! grep -q "/usr/local/go/bin" ~/.bashrc; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    fi
    export PATH=$PATH:/usr/local/go/bin
    
    echo "✅ Go установлен: $(go version)"
else
    echo "✅ Go уже установлен: $(go version)"
fi

# Клонирование репозитория
echo "📂 Клонирование репозитория..."
cd ~
if [ -d "msuparser" ]; then
    echo "⚠️  Директория msuparser уже существует"
    echo "Удалить и клонировать заново? (y/n)"
    read -r response
    if [[ "$response" == "y" ]]; then
        rm -rf msuparser
        git clone https://github.com/Kurumilog/msuparser.git
    else
        cd msuparser
        git pull
    fi
else
    git clone https://github.com/Kurumilog/msuparser.git
fi

cd ~/msuparser

# Установка зависимостей
echo "📦 Установка зависимостей..."
go get github.com/PuerkitoBio/goquery
go mod tidy

# Сборка
echo "🔨 Сборка приложения..."
go build -o test_parser test_parser.go parser.go
go build -o main main.go parser.go
chmod +x test_parser main

echo "✅ Сборка завершена"

# Настройка конфигурации
if [ ! -f config.json ]; then
    echo ""
    echo "⚙️  Настройка конфигурации"
    echo "============================"
    
    if [ -f config.py ]; then
        echo "📋 Найден старый config.py, конвертирую в config.json..."
        # Извлекаем данные из config.py
        BOT_TOKEN=$(grep "BOT_TOKEN" config.py | cut -d'"' -f2 | cut -d"'" -f2)
        USER_ID=$(grep "USER_ID" config.py | cut -d'"' -f2 | cut -d"'" -f2)
        NOTIFICATION_MINUTES=$(grep "NOTIFICATION_MINUTES" config.py | cut -d'=' -f2 | tr -d ' ')
        
        cat > config.json <<EOF
{
  "BOT_TOKEN": "$BOT_TOKEN",
  "USER_ID": "$USER_ID",
  "NOTIFICATION_MINUTES": ${NOTIFICATION_MINUTES:-15}
}
EOF
        echo "✅ Конфигурация сконвертирована"
    else
        echo "Введите Telegram Bot Token (получить у @BotFather):"
        read -r BOT_TOKEN
        
        echo "Введите ваш Telegram User ID (получить у @userinfobot):"
        read -r USER_ID
        
        echo "За сколько минут до пары отправлять уведомление? (по умолчанию 15):"
        read -r NOTIFICATION_MINUTES
        NOTIFICATION_MINUTES=${NOTIFICATION_MINUTES:-15}
        
        cat > config.json <<EOF
{
  "BOT_TOKEN": "$BOT_TOKEN",
  "USER_ID": "$USER_ID",
  "NOTIFICATION_MINUTES": $NOTIFICATION_MINUTES
}
EOF
        chmod 600 config.json
        echo "✅ Конфигурация создана"
    fi
else
    echo "✅ Конфигурация уже существует"
fi

# Тестирование
echo ""
echo "🧪 Тестирование парсера..."
./test_parser

if [ ! -f schedule.json ]; then
    echo "❌ Не удалось создать schedule.json"
    exit 1
fi

echo "✅ Парсер работает корректно"

# Установка сервисов
echo ""
echo "📦 Установка systemd сервисов"
echo "=============================="

# Обновляем пути в сервисах
sed -i "s|/home/ubuntu/msuparser|$HOME/msuparser|g" msuparser-bot.service
sed -i "s|User=ubuntu|User=$USER|g" msuparser-bot.service

sed -i "s|/home/ubuntu/msuparser|$HOME/msuparser|g" msuparser-update.service
sed -i "s|User=ubuntu|User=$USER|g" msuparser-update.service

# Устанавливаем сервисы
sudo cp msuparser-bot.service /etc/systemd/system/
sudo cp msuparser-update.service /etc/systemd/system/
sudo cp msuparser-update.timer /etc/systemd/system/

# Перезагружаем systemd
sudo systemctl daemon-reload

echo "✅ Сервисы установлены"

# Запуск сервисов
echo ""
echo "🚀 Запуск сервисов"
echo "=================="

sudo systemctl enable msuparser-bot.service
sudo systemctl enable msuparser-update.timer

sudo systemctl start msuparser-bot.service
sudo systemctl start msuparser-update.timer

sleep 2

# Проверка статуса
echo ""
echo "📊 Статус сервисов:"
sudo systemctl status msuparser-bot.service --no-pager -l
echo ""
sudo systemctl status msuparser-update.timer --no-pager -l

echo ""
echo "================================"
echo "✅ Установка завершена!"
echo "================================"
echo ""
echo "📝 Полезные команды:"
echo ""
echo "  # Статус бота"
echo "  sudo systemctl status msuparser-bot"
echo ""
echo "  # Логи бота"
echo "  sudo journalctl -u msuparser-bot -f"
echo ""
echo "  # Перезапуск бота"
echo "  sudo systemctl restart msuparser-bot"
echo ""
echo "  # Ручное обновление расписания"
echo "  sudo systemctl start msuparser-update"
echo ""
echo "  # Остановка всех сервисов"
echo "  sudo systemctl stop msuparser-bot msuparser-update.timer"
echo ""
echo "📚 Документация: ~/msuparser/DEPLOY_GUIDE.md"
echo ""
