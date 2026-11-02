# 10. Пример интеграции с Home Assistant

**Цель:** заменить скрипт отправки Telegram-сообщения вызовом собственного ChimePort API. Пример относится к **планируемому** API и заработает только после реализации сервера.

## 1. На сервере ChimePort

1. Создать личный канал `home/security`.
2. Создать источник с названием `Home Assistant` и описанием «Автоматизация дома».
3. Создать токен с правом **только** `messages:write` в `home/security`.
4. Записать токен в секреты Home Assistant; не вставлять его в YAML репозитория.

## 2. `secrets.yaml`

```yaml
chimeport_bearer_header: "Bearer REPLACE_WITH_YOUR_SECRET_SOURCE_TOKEN"
```

## 3. `configuration.yaml`

```yaml
rest_command:
  chimeport_security:
    url: "https://notify.example.org/api/v1/messages"
    method: POST
    content_type: "application/json"
    headers:
      Authorization: !secret chimeport_bearer_header
    payload: >-
      {{ {
        "channel": "home/security",
        "title": title,
        "body": message,
        "priority": "high"
      } | tojson }}
    timeout: 10
    verify_ssl: true
```

Пример действия автоматизации:

```yaml
- action: rest_command.chimeport_security
  data:
    title: "Входная дверь"
    message: "Дверь открыта"
```

Имена сущностей/структура автоматизации зависят от конфигурации конкретного HA. После внедрения сервера протестировать на поддерживаемой версии HA.

## 4. Важные нюансы

- Home Assistant может повторить запрос после ошибки; для событий, где возможны повторы, передавать стабильный `Idempotency-Key` в рамках одного исходного события.
- Для критичных датчиков не считать push единственным предупреждением: серверное принятие не гарантирует показа на телефоне.
- Для разных групп событий создать отдельные каналы (`home/security`, `home/climate`, `home/service`) и отдельные токены, если нужны разные права.
- Хранить URL и токен как конфигурацию, а не внутри множества автоматизаций.
