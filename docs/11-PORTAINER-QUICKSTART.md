# ChimePort: установка в Portainer

**Актуально для сервера v0.1.3 (05.11.2026).** Веб-панель, Android alpha и PWA/Web Push доступны. Web Push требует HTTPS и разрешения браузера. Для установки нужен HTTPS reverse proxy; не выставляйте внутренний порт приложения в Интернет без TLS.

## 1. Подготовка

1. Настройте доменное имя (например, `notify.example.org`) на IP вашего reverse proxy.
2. Настройте корректный сертификат HTTPS в Caddy, Nginx или Traefik.
3. Убедитесь, что Docker/Portainer может получить образ `ghcr.io/mirivlad/chimeport:v0.1.3` из GHCR.
4. Выделите постоянный Docker volume и предусмотрите **отдельное защищённое место** для резервных копий.

GHCR иногда требует публичную видимость пакета или авторизацию Registry с правом `read:packages`. Для последующего отката используйте точную версию или неизменяемый digest.

## 2. Создать Git stack

В Portainer откройте **Stacks → Add stack → Git repository**:

| Поле | Значение |
| --- | --- |
| Name | `chimeport` |
| Repository URL | `https://github.com/mirivlad/ChimePort.git` |
| Repository reference | `refs/heads/main` |
| Compose path | `deploy/compose.portainer.yaml` |
| Environment | `CHIMEPORT_PUBLIC_URL=https://notify.example.org` |
| Рекомендованная фиксация образа | `CHIMEPORT_IMAGE=ghcr.io/mirivlad/chimeport:v0.1.3` |

Нажмите **Deploy the stack**. Сервис запустится на `127.0.0.1:8080` Docker-хоста, данные — в именованном томе (`chimeport_chimeport_data` при таком названии стека). UID/GID контейнера: `10001`.

**Reverse proxy на хосте:** проксируйте HTTPS к `http://127.0.0.1:8080`. Например, Caddy:

```caddyfile
notify.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

**Прокси в другом контейнере:** `127.0.0.1` внутри proxy означает *сам прокси*. Подключите оба контейнера к общей Docker-сети и проксируйте на имя сервиса `chimeport:8080`. Не открывайте контейнерный 8080 в Интернет.

`CHIMEPORT_PUBLIC_URL` должен быть **точным HTTPS origin** (схема + хост, при необходимости порт), **без конечного слеша**: сервер проверяет Origin браузера при входе и изменениях.

Проверка с Docker-хоста:

```sh
curl -fsS http://127.0.0.1:8080/health/ready
curl -fsS https://notify.example.org/api/v1/instance
```

## 3. Создание первого администратора

Самостоятельная регистрация отключена. Нужно один раз создать первого пользователя с ролью администратора. **Не передавайте пароль через аргументы процесса, `docker exec ... --password` или через Git/Portainer environment.** CLI принимает файл пароля.

Откройте **Portainer → Containers → chimeport-chimeport-1 → Console**, выберите `/bin/sh` и подключитесь в интерактивном режиме (или на Docker-хосте выполните `docker exec -it chimeport-chimeport-1 /bin/sh`). Введите:

```sh
umask 077
printf 'Новый пароль (12–128 байт): '
stty -echo
IFS= read -r ADMIN_PASSWORD
stty echo
printf '\n'
printf '%s' "$ADMIN_PASSWORD" > /data/.bootstrap-admin-password
unset ADMIN_PASSWORD
/usr/local/bin/chimeport bootstrap --username admin --password-file /data/.bootstrap-admin-password
rm -f /data/.bootstrap-admin-password
```

Пароль не выводится на экран. После ошибки также удалите временный файл: `rm -f /data/.bootstrap-admin-password`. Команда `bootstrap` доступна **только пока нет пользователей**; повторный вызов должен завершаться ошибкой `bootstrap already completed`. Логин — 3–32 символа строчных латинских букв, цифр, `_` или `-`, с первой буквой.

## 4. Первоначальная настройка

1. Откройте `https://notify.example.org`, войдите как `admin`.
2. Если нужны отдельные пользователи, создайте их в разделе **Пользователи** (роль user/admin).
3. В **Каналах** создайте канал `servers` («Серверы»). Технический slug генерируется автоматически и доступен в дополнительных настройках.
4. В **Источниках и токенах** создайте источник `monitoring`. Добавьте токен и **галочками** выберите разрешённый канал `servers`.
5. Скопируйте секрет токена. Позднее его можно вновь показать или скопировать; разрешения можно изменить, токен можно отозвать.

**Пользовательские данные изолированы.** Администратор не получает автоматического доступа к чужой истории. Для отправки сообщения в канал пользователя нужен токен его источника с соответствующими правами. Смена пользовательского пароля отзывает сессии, но не source tokens; блокировка аккаунта временно блокирует и его токены.

## 5. Тестовая отправка

```sh
export CHIMEPORT_URL='https://notify.example.org'
printf 'Токен источника: '; stty -echo; IFS= read -r CHIMEPORT_TOKEN; stty echo; printf '\n'
curl --fail-with-body -sS -X POST "$CHIMEPORT_URL/api/v1/messages" \
  -H "Authorization: Bearer $CHIMEPORT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"channel":"servers","title":"ChimePort подключён","body":"Тестовое уведомление","priority":"normal"}'
unset CHIMEPORT_TOKEN
```

Ожидается HTTP **201**, `stored: true`. Откройте **Входящие**. Для интеграций можно копировать URL канала в его карточке; дополнительные примеры есть в веб-панели по `/examples` и в [документации API](03-API.md).

Android-клиент устанавливается из [релиза v0.1.1](https://github.com/mirivlad/ChimePort/releases/tag/v0.1.1). Он подключается к HTTPS URL сервера под обычной пользовательской учётной записью. PWA/Web Push доступны через «Настройки» веб-панели.

## 6. Обновление, копии, откат

1. Создайте и **проверьте** архив командой `maintenance backup` / `maintenance verify` (см. [пошаговое руководство](13-BACKUP-RESTORE.md)).
2. Зафиксируйте текущий образ, schema version и instance ID.
3. Измените `CHIMEPORT_IMAGE` на опубликованную версию, например `ghcr.io/mirivlad/chimeport:v0.1.3`.
4. В Portainer выберите **Update the stack** с получением свежего Git compose и **pull/redeploy image**.
5. Проверьте `/health/ready`, вход, каналы, токены и тестовую отправку. **Не удаляйте volume.**

Если надо откатить только код и схема БД совместима — верните прежний образ. Если схема несовместима — восстанавливайте согласованную пару «образ + архив» **в другой пустой каталог**, как описано в [Backup & Restore](13-BACKUP-RESTORE.md).

На Docker-хосте эквивалентом pull/redeploy является:

```sh
docker compose -p chimeport -f deploy/compose.portainer.yaml pull chimeport
docker compose -p chimeport -f deploy/compose.portainer.yaml up -d --no-deps chimeport
```

Запускайте эти команды из клона репозитория **только если осознанно управляете стеком вне Portainer**: Portainer может не синхронизировать собственную копию compose-файла после ручной операции.

## 7. Неисправности

| Признак | Что проверить |
| --- | --- |
| `401` / не удаётся войти | пароль, блокировку аккаунта, первый bootstrap, срок пользовательской сессии |
| `403 invalid_origin` | точное совпадение `CHIMEPORT_PUBLIC_URL` и browser origin (HTTPS, домен, порт) |
| `502` у reverse proxy | upstream, Docker-сеть, `docker ps`, `/health/ready` |
| `unauthorized` при pull | публичность GHCR-пакета или registry credentials |
| Android не показывает уведомление | учётная запись, разрешения ОС, ограничения батареи, поток/синхронизацию; [Android README](../android/README.md) |
| Нет Web Push | проверьте HTTPS, поддержку Push API/Service Worker, разрешение уведомлений, установленную PWA на iOS и настройки ОС; подробнее — [PWA/Web Push](16-PWA-WEBPUSH.md) |
| Сообщение не публикуется | правильный source token, разрешённый канал, HTTP-коды `403` / `429` |

Для проверки на хосте используйте `docker ps`, `docker logs chimeport-chimeport-1`, `curl -fsS http://127.0.0.1:8080/health/ready`. Не пересылайте логи с секретами и паролями. ChimePort не является сертифицированной системой тревожного оповещения.

## PWA / Web Push (v0.1.3)

В веб-панели откройте **Настройки** и включите Web Push. На iOS/iPadOS 16.4+ сначала добавьте сайт на домашний экран и запустите его оттуда. VAPID-ключи создаются сервером автоматически и хранятся в SQLite, приватный ключ зашифрован файлом `/data/token-key`; для восстановления нужны **оба** компонента. Обновление с v0.1.2 мигрирует SQLite **3 → 4**, поэтому перед redeploy обязательно проверьте архив старой схемы. Для отката восстановите старый архив вместе со старым образом. [Подробности](16-PWA-WEBPUSH.md).
