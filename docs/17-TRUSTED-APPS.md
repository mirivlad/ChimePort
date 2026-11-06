# Trusted Applications (schema 5)

ChimePort теперь может доставлять персональные уведомления от внешних приложений, **не создавая пользовательские аккаунты ChimePort**. Приёмником служит неизменная пара \`(external_app_id, recipient)\`; пароль и даже имя пользователя внешнего приложения ChimePort не получает.

## Основной протокол

Администратор ChimePort создаёт \`POST /api/v1/admin/external-apps\` с JSON \`{"slug":"tms","origin":"https://tasks.example.org"}\`. Требуется обычная администраторская сессия ChimePort. В ответ возвращаются metadata и единственный секрет \`cp_a_...\`. \`GET /api/v1/admin/external-apps\` показывает список без токенов. \`PATCH /api/v1/admin/external-apps/{id}\` с \`{"disabled":true}\` немедленно прекращает работу приложения.

Приложение публикует сообщения через \`POST /api/v1/external/messages\` c \`Authorization: Bearer cp_a_...\` и JSON \`{"recipient":"42","title":"Новая задача","body":"...","priority":"normal","url":"https://tasks.example.org/tasks/42"}\`. \`recipient\` — стабильный непрозрачный идентификатор приложения; ссылка должна принадлежать заранее зарегистрированному HTTPS origin. Для повторов используйте \`Idempotency-Key\`. Записи хранятся в SQLite и ограничены по скорости.

Для выпуска доступа устройству **только авторизованный сервер приложения** вызывает \`POST /api/v1/external/tickets\` с \`{"recipient":"42","code_challenge":"<base64url SHA256 of verifier>"}\`. ChimePort возвращает одноразовый \`cp_k_\` пропуск на 90 секунд. Устройство получает его через уже подтверждённый вход в приложение и вызывает \`POST /api/v1/external/exchange\` с \`{"ticket":"cp_k_...","code_verifier":"<verifier>"}\`. Результат — \`cp_e_\` сессия конкретного внешнего получателя на 7 суток.

Сессия поддерживает \`GET /api/v1/external/info\`, \`GET /api/v1/external/messages?after=<cursor>\`, \`POST/DELETE /api/v1/external/messages/{id}/read\`, \`GET /api/v1/external/events\` (SSE), \`POST /api/v1/external/logout\`, \`GET /api/v1/external/webpush/vapid-public-key\`, \`POST/DELETE /api/v1/external/webpush/subscriptions\`. VAPID public key также доступен по служебному токену приложения, но подписку может создать только scoped-сессия конкретного получателя.

**Безопасность:** обычные ChimePort пользователи и внешние получатели изолированы на уровне запросов SQL и аутентификации. Один внешний получатель не видит сообщения или курсор другого. Подписка Web Push не может быть перепривязана к чужому получателю. Секрет \`cp_a_\` нельзя передавать Android или браузеру. Не доверяйте URL как идентификатору сервера без зарегистрированного приложения.

## Совместимость и эксплуатация

Миграция SQLite 4 → 5 не изменяет обычные аккаунты/каналы/историю и поддерживается резервным копированием (supported schema = 5). Очистка внешней истории — \`CHIMEPORT_RETENTION_DAYS\`, по умолчанию 30 суток. При обновлении обязательно выполнить резервную копию SQLite вместе с ключом \`token-key\`.

**Текущие ограничения:** нет управляемой ротации токена доверенного приложения и немедленного индивидуального отзыва scoped-сессий при смене пароля во внешнем TMS; сессия истечёт максимум через 7 суток или отключается вместе с приложением. Android и Web Push являются best effort. Не используйте их как единственный аварийный канал связи.

Сквозная инструкция для TMS: [TMS docs/CHIMEPORT_BRIDGE.md](https://github.com/mirivlad/tms/blob/feat/chimeport-external-bridge/docs/CHIMEPORT_BRIDGE.md).
