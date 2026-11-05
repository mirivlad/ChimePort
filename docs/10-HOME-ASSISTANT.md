# 10. Home Assistant → ChimePort

**Рабочая интеграция для ChimePort v0.1.0 и Home Assistant 2026.1.3.** Никаких сторонних интеграций HA не требуется: используем стандартный `rest_command` и REST API ChimePort.

Полная инструкция с проверкой, примерами отправки, секретами и автоматизациями:

**[examples/home-assistant/README.md](../examples/home-assistant/README.md)**

В каталоге примеров также находятся:

- [configuration.fragment.yaml](../examples/home-assistant/configuration.fragment.yaml) — HTTP-команда для `configuration.yaml`;
- [secrets.yaml.example](../examples/home-assistant/secrets.yaml.example) — заглушка Bearer-токена;
- [automations.example.yaml](../examples/home-assistant/automations.example.yaml) — примеры для двери, температуры, батареек, погоды и запуска/остановки HA.

Весь пример использует **только вымышленный домен `notify.example.org` и недействительный токен `cp_t_EXAMPLE_NOT_A_REAL_TOKEN`**. Не сохраняйте реальные токены в Git.

## Кратко

1. В ChimePort создайте источник `home-assistant`, один канал `home` (название «Дом»); выдайте ему ограниченный токен.
2. Сохраните `Bearer cp_t_...` в локальном HA `secrets.yaml` как `chimeport_bearer`.
3. Добавьте `rest_command.chimeport_publish` из каталога примеров в `configuration.yaml`, установив **свой** адрес вместо `https://notify.example.org`.
4. Проверьте YAML, перезапустите HA, выполните тест в инструментах разработчика.
5. В существующие автоматизации **добавьте второе действие** ChimePort. Не удаляйте Telegram: он остаётся параллельным каналом уведомлений.

При успехе HA покажет `status: 201`, а ответ ChimePort содержит `stored: true`; уведомление также должно прийти в Android-клиент.
