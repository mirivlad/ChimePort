# ChimePort — резервное копирование и восстановление

**Актуально для сервера v0.1.2 (2026-11-05).** Ниже показаны действия на Docker-хосте; Portainer управляет контейнером, но бэкап выполняется отдельным временным контейнером. Не удаляйте существующий том `/data`.

Статус: **команды обслуживания реализованы, веб-мастер восстановления ещё не создан**.
Они запускаются через временный контейнер из Docker-хоста и работают даже при
недоступной веб-панели. Восстановление не перезаписывает действующий том.

## Что находится в резервной копии

- SQLite /data/chimeport.db (пользователи, сообщения, прочтения, каналы, токены,
  идентификатор сервера, секреты курсоров, версия схемы).
- /data/token-key — 32-байтный ключ для зашифрованных токенов.

Бэкап действующего SQLite в WAL-режиме выполняется **VACUUM INTO**, а не
побайтовым копированием открытого файла базы. Временные WAL/SHM файлы в архив
не нужны. Ключ читается до и после snapshot: при его изменении процесс
прерывается. Архив содержит manifest.json с временем, схемой, instance ID,
размерами и SHA-256. Перед публикацией он полностью проверяется, включая
SQLite integrity_check, foreign_key_check и расшифровку одного существующего
токена, если он имеется. Уже существующий архив нельзя перезаписать.

**Архив содержит секреты и персональные сообщения.** SHA-256 обеспечивает
обнаружение повреждения, но не шифрование. Храните резервные копии в каталоге
с ограниченным доступом и шифруйте перед отправкой на внешний носитель или
в облако (например, restic/age). Вторая независимая копия обязательна.

## Шаг 1. Подготовить Docker-хост

Для стека с именем chimeport действующий Docker volume обычно называется
chimeport_chimeport_data. Проверьте это в Portainer → Volumes или командой:

~~~sh
docker volume ls --format '{{.Name}}' | grep chimeport
docker ps --format '{{.Names}} {{.Image}}' | grep chimeport
sudo install -d -m 0700 -o 10001 -g 10001 /srv/chimeport/backups
~~~

В примерах используется Docker-хост (не shell внутри контейнера Portainer).
Если volume имеет другое имя — исправьте CHIMEPORT_DATA_SOURCE.
На всех операциях действуют UID/GID 10001, без root внутри контейнера.

## Шаг 2. Создать онлайн-бэкап без остановки сервера

**Важно:** `/srv/chimeport/backups` в примере — каталог **на Docker-хосте**. Создайте его с UID/GID 10001 и правами 0700. В архив входят секреты; после создания сделайте независимую зашифрованную копию.

Из локального клона репозитория на Docker-хосте:

~~~sh
export CHIMEPORT_DATA_SOURCE=chimeport_chimeport_data
export CHIMEPORT_BACKUP_DIR=/srv/chimeport/backups
export CHIMEPORT_IMAGE=ghcr.io/mirivlad/chimeport:v0.1.2
sh deploy/scripts/chimeport-backup.sh
~~~

Если ChimePort уже работает с bind mount, CHIMEPORT_DATA_SOURCE может быть
абсолютным путём. Скрипт сначала проверяет, что указанный источник существует
(не создаёт случайно новый пустой volume). Он запускает отдельный
непривилегированный Docker-контейнер без сети; живой сервер продолжает работать.

Пример вывода:
~~~text
Backup verified: /backups/chimeport-20261104T033000Z.tar.gz (instance=... schema=3)
Verified backup: /srv/chimeport/backups/chimeport-20261104T033000Z.tar.gz
~~~

Для cron после проверки ручного запуска:
~~~cron
0 3 * * * CHIMEPORT_DATA_SOURCE=chimeport_chimeport_data CHIMEPORT_BACKUP_DIR=/srv/chimeport/backups CHIMEPORT_IMAGE=ghcr.io/mirivlad/chimeport:v0.1.2 /bin/sh /opt/chimeport/deploy/scripts/chimeport-backup.sh >> /var/log/chimeport-backup.log 2>&1
~~~

Здесь /opt/chimeport — **пример** пути клона репозитория: укажите реальный
путь. Пока нет проверенного внешнего хранилища, не включайте автоматическое
удаление старых копий. Ротация 7 дневных + 4 недельных + 3 месячных
и отправка в NAS/S3 — отдельный будущий этап.

## Шаг 3. Проверить любой архив отдельно

~~~sh
docker run --rm --network none --user 10001:10001 \
  --security-opt no-new-privileges:true --cap-drop ALL \
  --mount type=bind,source=/srv/chimeport/backups,target=/backups,readonly \
  ghcr.io/mirivlad/chimeport:v0.1.2 maintenance verify \
  --archive /backups/chimeport-20261104T033000Z.tar.gz
~~~

Команда ничего не меняет в действующей БД. Она выведет instance ID,
schema version, время резервной копии. Сохраните instance ID для шага
восстановления. Архив с неверным hash, неполным набором файлов, чужой схемой
или повреждённой базой отвергается.

## Сценарий A. Неудачное обновление без изменения схемы БД

1. До обновления: снимите бэкап, запишите полный SHA рабочего контейнера
   (в GitHub Actions публикуются неизменяемые GHCR-теги sha-<40-символьный
   SHA коммита> и движущийся main; для максимальной строгости используйте digest).
2. Если новый код сломан, в Portainer → Stacks → chimeport остановите контейнер.
3. В Environment variables задайте, например:
   CHIMEPORT_IMAGE=ghcr.io/mirivlad/chimeport:sha-<SHA предыдущей версии>
4. Выберите Update the stack с pull/redeploy, **не удаляя volume**.
5. Проверьте /health/ready, вход, каналы, историю и тестовую отправку из HA.

Данные останутся текущими, никаких сообщений откатом кода не теряется.
Однако **старую программу нельзя запускать поверх новой несовместимой схемы
SQLite**. При изменившейся схеме используйте сценарий B.

## Сценарий B. Повреждение БД или откат несовместимой схемы

Восстанавливаем предыдущий образ вместе с соответствующей резервной копией
в **новую отдельную директорию**. Старый volume остаётся нетронутым.

**1.** Выберите архив, выполните verify (шаг 3), скопируйте его instance ID.

**2.** Создайте новый пустой каталог на Docker-хосте:

~~~sh
sudo install -d -m 0700 -o 10001 -g 10001 /srv/chimeport/restored-20261104
~~~

**3.** Распакуйте только проверенный архив:

~~~sh
docker run --rm --network none --user 10001:10001 \
  --security-opt no-new-privileges:true --cap-drop ALL \
  --mount type=bind,source=/srv/chimeport/backups,target=/backups,readonly \
  --mount type=bind,source=/srv/chimeport/restored-20261104,target=/data \
  ghcr.io/mirivlad/chimeport:v0.1.2 maintenance restore \
  --archive /backups/chimeport-20261104T033000Z.tar.gz \
  --data-dir /data \
  --expect-instance ВСТАВИТЬ_INSTANCE_ID_ИЗ_VERIFY
~~~

Restore откажется работать, если каталог непуст, instance ID не совпадает
или есть ошибки архива. При прерывании останется .restore-in-progress:
сервер не запустится с половиной восстановленного состояния. В таком случае
используйте другой **новый** пустой каталог и повторите restore.

**4.** В Portainer → Stacks → chimeport остановите старый контейнер.
В Environment variables установите одновременно:

~~~dotenv
CHIMEPORT_DATA_MOUNT=/srv/chimeport/restored-20261104
CHIMEPORT_IMAGE=ghcr.io/mirivlad/chimeport:sha-<совместимый_полный_SHA>
~~~

**5.** Update the stack. Новая версия compose подключит путь как bind mount
к /data, не стирая прежний Docker volume. Не запускайте старый и новый сервер
одновременно с одним публичным URL.

**6.** Проверьте /health/ready, /api/v1/instance (должен быть тот же ID),
вход, пользователей, каналы, историю, API-токены и тестовое сообщение.
Android-клиент должен узнать исходный instance ID.

**7.** Если восстановление не устраивает: остановите контейнер, удалите
CHIMEPORT_DATA_MOUNT из переменных, верните прежний CHIMEPORT_IMAGE, затем
обновите стек. Он снова подключит исходный именованный volume. **Не удаляйте
этот volume до полного окончания тестирования.**

Данные после момента снимка в новом восстановленном каталоге отсутствуют,
но остаются в старом volume. Автоматического слияния историй пока нет.

## Список команд

~~~text
chimeport maintenance backup  --data-dir /data --output /backups/имя.tar.gz
chimeport maintenance verify  --archive /backups/имя.tar.gz
chimeport maintenance restore --data-dir /data --archive /backups/имя.tar.gz --expect-instance ID
~~~

Все команды нужно выполнять с нужными Docker mount. Не выполняйте restore
внутри рабочего контейнера и не удаляйте текущий /data. Никаких дополнительных
прав доступа к веб-админке не добавляется — резервирование доступно
оператору Docker-хоста.

## Ограничения и незавершённая работа

- Пока нет веб-интерфейса администратора для архивации и восстановления,
  расписания/удалённой синхронизации/шифрования внутри самого ChimePort.
- В релизе v0.1.2 используется схема SQLite **версии 3** (проверено на рабочей инсталляции 2026-11-05). Будущие изменения схемы требуют снимка
  перед миграцией и явного описания совместимости конкретных образов.
- Полный disaster recovery после потери Docker-хоста появится только после
  реальной проверки восстановлением копии **с другого носителя**.
- Не считать один успешный unit test доказательством работоспособности
  на реальном Portainer-хосте: требуется ручное испытание на тестовом каталоге.
