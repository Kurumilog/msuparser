#!/usr/bin/env python3
"""
Вспомогательный скрипт для извлечения конфига из config.py
Используется Go ботом для получения токена и ID
"""

import json
import sys
import os

def get_config():
    """Получить конфигурацию из config.py"""
    try:
        # Добавляем текущую директорию в path
        sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
        
        import config
        
        return {
            'BOT_TOKEN': config.BOT_TOKEN,
            'USER_ID': config.USER_ID,
            'NOTIFICATION_MINUTES': config.NOTIFICATION_MINUTES,
        }
    except ImportError:
        print(json.dumps({'error': 'config.py not found'}), file=sys.stderr)
        return None
    except AttributeError as e:
        print(json.dumps({'error': f'Missing config value: {str(e)}'}), file=sys.stderr)
        return None
    except Exception as e:
        print(json.dumps({'error': str(e)}), file=sys.stderr)
        return None

if __name__ == '__main__':
    config = get_config()
    if config:
        print(json.dumps(config))
    else:
        sys.exit(1)
