"""
Полный парсер расписания МГУ ВШГА
Извлекает всё расписание группы 303 с номерами пар
"""

from selenium import webdriver
from selenium.webdriver.common.by import By
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC
from selenium.common.exceptions import WebDriverException

# Firefox
from selenium.webdriver.firefox.service import Service as FirefoxService
from selenium.webdriver.firefox.options import Options as FirefoxOptions
from webdriver_manager.firefox import GeckoDriverManager

# Chrome (fallback)
from selenium.webdriver.chrome.service import Service as ChromeService
from selenium.webdriver.chrome.options import Options as ChromeOptions
from webdriver_manager.chrome import ChromeDriverManager

from selenium.webdriver.common.keys import Keys
from selenium.webdriver.common.action_chains import ActionChains
import json
import time
import re
from datetime import datetime, timedelta


class ScheduleParser:
    def __init__(self, headless=True, days_ahead=5):
        """Инициализация парсера"""
        self.url = "https://tt.audit.msu.ru/time-table/group"
        self.driver = None
        self.headless = headless
        self.wait = None
        self.days_ahead = days_ahead  # Собирать пары на N дней вперед
        
        # Вычисляем диапазон дат
        today = datetime.now().date()
        self.min_date = today
        self.max_date = today + timedelta(days=days_ahead)
        
        # Маппинг времени пар
        self.lesson_times = {
            '1': {'start': '09:00', 'end': '10:30'},
            '2': {'start': '10:45', 'end': '12:15'},
            '3': {'start': '13:00', 'end': '14:30'},
            '4': {'start': '14:45', 'end': '16:15'},
            '5': {'start': '16:30', 'end': '18:00'},
        }
        
    def setup_driver(self):
        """Попытка запуска Firefox, при ошибке — fallback на Chrome."""
        print("🔧 Настройка браузера (попытка Firefox, затем Chrome)...")

        # Сначала пробуем Firefox
        try:
            firefox_options = FirefoxOptions()
            if self.headless:
                firefox_options.headless = True

            # Общие опции
            firefox_options.add_argument("--no-sandbox")
            firefox_options.add_argument("--disable-dev-shm-usage")
            firefox_options.add_argument("--width=1920")
            firefox_options.add_argument("--height=1080")

            gecko_path = GeckoDriverManager().install()
            print(f"Использую geckodriver: {gecko_path}")
            service = FirefoxService(gecko_path)
            self.driver = webdriver.Firefox(service=service, options=firefox_options)
            self.wait = WebDriverWait(self.driver, 20)
            print("✅ Firefox запущен")
            return

        except Exception as e:
            print(f"⚠️  Не удалось запустить Firefox: {e}")
            # Продолжаем к попытке Chrome

        # Попытка запуска Chrome (fallback)
        try:
            chrome_options = ChromeOptions()
            if self.headless:
                chrome_options.add_argument("--headless")

            chrome_options.add_argument("--no-sandbox")
            chrome_options.add_argument("--disable-dev-shm-usage")
            chrome_options.add_argument("--window-size=1920,1080")
            chrome_options.add_argument("--disable-blink-features=AutomationControlled")

            chrome_path = ChromeDriverManager(driver_version="141").install()
            print(f"Использую chromedriver: {chrome_path}")
            service = ChromeService(chrome_path)
            self.driver = webdriver.Chrome(service=service, options=chrome_options)
            self.wait = WebDriverWait(self.driver, 20)
            print("✅ Chrome запущен (fallback)")
            return

        except Exception as e:
            print(f"❌ Не удалось запустить Chrome: {e}")
            # Перекидываем исключение выше
            raise
        
    def select_group(self):
        """Выбор факультета, курса и группы"""
        print("\n🎯 Открываю сайт расписания...")
        self.driver.get(self.url)
        
        print("⏳ Жду загрузки страницы...")
        time.sleep(3)
        
        try:
            # Факультет
            print("\n📚 Выбираю факультет...")
            faculty_select2 = self.wait.until(
                EC.element_to_be_clickable((By.CSS_SELECTOR, "#select2-timetableform-facultyid-container"))
            )
            faculty_select2.click()
            time.sleep(0.5)
            
            faculty_option = self.wait.until(
                EC.element_to_be_clickable((
                    By.XPATH, 
                    "//li[contains(@class, 'select2-results__option') and contains(text(), 'Высшая школа')]"
                ))
            )
            faculty_option.click()
            print("   ✅ Выбран: Высшая школа государственного аудита")
            time.sleep(2)
            
            # Курс
            print("\n📖 Выбираю курс...")
            course_select2 = self.wait.until(
                EC.element_to_be_clickable((By.CSS_SELECTOR, "#select2-timetableform-course-container"))
            )
            course_select2.click()
            time.sleep(0.5)
            
            course_option = self.wait.until(
                EC.element_to_be_clickable((
                    By.XPATH,
                    "//li[contains(@class, 'select2-results__option') and text()='3']"
                ))
            )
            course_option.click()
            print("   ✅ Выбран: 3 курс")
            time.sleep(2)
            
            # Группа
            print("\n👥 Выбираю группу...")
            group_select2 = self.wait.until(
                EC.element_to_be_clickable((By.CSS_SELECTOR, "#select2-timetableform-groupid-container"))
            )
            group_select2.click()
            time.sleep(0.5)
            
            group_option = self.wait.until(
                EC.element_to_be_clickable((
                    By.XPATH,
                    "//li[contains(@class, 'select2-results__option') and text()='303']"
                ))
            )
            group_option.click()
            print("   ✅ Выбрана: группа 303")
            
            print("\n⏳ Загружается расписание...")
            time.sleep(5)
            
            print("✅ Расписание загружено")
            return True
            
        except Exception as e:
            print(f"\n❌ Ошибка при выборе параметров: {e}")
            return False
    
    def parse_full_schedule(self):
        """Парсинг полного расписания"""
        print("\n📊 Парсинг полного расписания...")
        
        schedule = []
        
        try:
            # Находим все ячейки с парами
            lesson_cells = self.driver.find_elements(
                By.XPATH,
                "//td[contains(., '[')]"
            )
            
            print(f"   Найдено ячеек с парами: {len(lesson_cells)}")
            
            if len(lesson_cells) == 0:
                print("❌ Не найдено ячеек с парами!")
                return []
            
            processed = 0
            skipped = 0
            
            for i, cell in enumerate(lesson_cells):
                try:
                    cell_text = cell.text.strip()
                    if not cell_text or len(cell_text) < 5:
                        continue
                    
                    # Скроллим к элементу
                    self.driver.execute_script("arguments[0].scrollIntoView({block: 'center'});", cell)
                    time.sleep(0.3)
                    
                    # Кликаем
                    cell.click()
                    time.sleep(1)
                    
                    # Ищем popup
                    try:
                        popup = self.driver.find_element(
                            By.XPATH,
                            "//div[contains(@class, 'popover') and contains(@class, 'show')]"
                        )
                        
                        if popup and popup.is_displayed():
                            popup_text = popup.text
                            
                            # Парсим данные
                            lesson_data = self.parse_lesson_details(popup_text, cell_text)
                            
                            if lesson_data:
                                # Фильтрация
                                if self.should_include_lesson(lesson_data):
                                    schedule.append(lesson_data)
                                    processed += 1
                                    
                                    if processed % 10 == 0:
                                        print(f"   ✓ Обработано: {processed} пар")
                                else:
                                    skipped += 1
                            
                            # Закрываем popup
                            ActionChains(self.driver).send_keys(Keys.ESCAPE).perform()
                            time.sleep(0.2)
                        
                    except Exception as popup_error:
                        pass
                        
                except Exception as e:
                    continue
            
            print(f"\n✅ Успешно обработано: {processed} пар")
            print(f"⏭️  Пропущено (факультативы/военная): {skipped} пар")
            return schedule
            
        except Exception as e:
            print(f"\n❌ Критическая ошибка парсинга: {e}")
            return []
    
    def parse_lesson_details(self, popup_text, cell_text):
        """Парсинг деталей пары"""
        try:
            lines = [line.strip() for line in popup_text.split('\n') if line.strip()]
            
            lesson_data = {
                'subject': '',
                'teacher': '',
                'room': '',
                'lesson_number': '',
                'time_start': '',
                'time_end': '',
                'date': '',
                'weekday': '',
                'group': '303',
            }
            
            # Извлекаем данные
            for line in lines:
                # Дата и номер пары (первая строка: "24.11.2025 1 пара")
                if '.' in line and 'пара' in line.lower():
                    date_match = re.search(r'(\d{2}\.\d{2}\.\d{4})', line)
                    if date_match:
                        lesson_data['date'] = date_match.group(1)
                        
                        # День недели
                        try:
                            date_obj = datetime.strptime(lesson_data['date'], '%d.%m.%Y')
                            weekdays = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']
                            lesson_data['weekday'] = weekdays[date_obj.weekday()]
                        except:
                            pass
                    
                    # Номер пары
                    lesson_num_match = re.search(r'(\d+)\s*пара', line, re.IGNORECASE)
                    if lesson_num_match:
                        lesson_data['lesson_number'] = lesson_num_match.group(1)
                        
                        # Время из маппинга
                        if lesson_data['lesson_number'] in self.lesson_times:
                            time_info = self.lesson_times[lesson_data['lesson_number']]
                            lesson_data['time_start'] = time_info['start']
                            lesson_data['time_end'] = time_info['end']
                    continue
                
                # Предмет (содержит [...])
                if '[' in line and ']' in line and not lesson_data['subject']:
                    lesson_data['subject'] = line
                    continue
                
                # Аудитория
                if 'ауд.' in line.lower() and not lesson_data['room']:
                    lesson_data['room'] = line
                    continue
                
                # Преподаватель (ФИО)
                if ' ' in line and len(line.split()) >= 2:
                    words = line.split()
                    if any(w[0].isupper() for w in words if w):
                        if not lesson_data['teacher'] and 'добавлено' not in line.lower():
                            lesson_data['teacher'] = line
                            continue
            
            # Проверяем что получили основное
            if lesson_data['subject'] and lesson_data['date'] and lesson_data['lesson_number']:
                return lesson_data
            else:
                return None
            
        except Exception as e:
            return None
    
    def should_include_lesson(self, lesson_data):
        """Проверка нужно ли включать пару (фильтры)"""
        
        # Фильтр по датам: только следующие N дней
        try:
            lesson_date = datetime.strptime(lesson_data['date'], '%d.%m.%Y').date()
            if not (self.min_date <= lesson_date <= self.max_date):
                return False
        except:
            return False
        
        # Пропускаем МФК (факультатив по средам)
        if 'МФК' in lesson_data['subject'].upper():
            return False
        
        # Пропускаем военную кафедру (по четвергам)
        if lesson_data['weekday'] == 'Чт':
            subject_lower = lesson_data['subject'].lower()
            if any(keyword in subject_lower for keyword in ['военная', 'военное', 'воен']):
                return False
        
        return True
    
    def save_schedule(self, schedule, filename='schedule.json'):
        """Сохранение расписания в JSON"""
        print(f"\n💾 Сохраняю расписание...")
        
        # Сортируем по дате и времени
        schedule.sort(key=lambda x: (x['date'], x['lesson_number']))
        
        with open(filename, 'w', encoding='utf-8') as f:
            json.dump(schedule, f, ensure_ascii=False, indent=2)
        
        print(f"✅ Сохранено в {filename}: {len(schedule)} пар")
    
    def run(self):
        """Основной метод запуска"""
        try:
            self.setup_driver()
            
            if not self.select_group():
                return []
            
            schedule = self.parse_full_schedule()
            
            if schedule:
                self.save_schedule(schedule)
            else:
                print("\n⚠️  Расписание пустое")
            
            return schedule
            
        except Exception as e:
            print(f"\n❌ Критическая ошибка: {e}")
            import traceback
            traceback.print_exc()
            return []
        finally:
            if self.driver:
                self.driver.quit()
                print("\n🔒 Браузер закрыт")


if __name__ == "__main__":
    print("=" * 60)
    print("🎓 ПАРСЕР РАСПИСАНИЯ МГУ ВШГА - ПОЛНАЯ ВЕРСИЯ")
    print("=" * 60)
    
    parser = ScheduleParser(headless=True, days_ahead=5)
    print(f"📅 Собираю расписание с {parser.min_date.strftime('%d.%m.%Y')} по {parser.max_date.strftime('%d.%m.%Y')} (5 дней)")
    schedule = parser.run()
    
    print("\n" + "=" * 60)
    print(f"📋 ИТОГО: {len(schedule)} пар")
    print("=" * 60)
    
    # Показываем примеры
    if schedule:
        print("\n📚 Первые 5 пар:")
        for i, lesson in enumerate(schedule[:5], 1):
            print(f"\n{i}. {lesson['date']} ({lesson['weekday']}) - Пара {lesson['lesson_number']}")
            print(f"   Предмет: {lesson['subject']}")
            print(f"   Время: {lesson['time_start']}-{lesson['time_end']}")
            print(f"   Преподаватель: {lesson['teacher']}")
            print(f"   Аудитория: {lesson['room']}")
