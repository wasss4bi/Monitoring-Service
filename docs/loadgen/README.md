# Синтетическая нагрузочная база PostgreSQL

Генератор для текущей схемы Monitoring-Service (миграции 1–2). Он создаёт отдельный снимок данных, обновляет статистику и проверяет целостность. SQL приложения и его индексы не оптимизируются.


## Что исследовано

Источники: `migrations/*.up.sql`, `internal/domain/monitor.go`, `internal/domain/idempotency.go`, репозитории `monitor_repository`, `monitoring_execution_repository`, `endpoint_fetch_repository`, `outbox_repository`, `notification_repository`, `processed_message_repository`, `idempotency_repository`, `monitoring_maintenance_repository`, обработчики API и сервисы исполнения.

Текущая схема использует отдельные мониторы:

* `ScheduleDue` выбирает **monitors**, присоединяет endpoints/target_hostnames, проверяет активность hostname и наличие активного подтверждённого контакта. Фильтры: `status='monitoring'`, `deleted_at IS NULL`, `check_status IN ('idle','retry_wait')`, срок проверки; сортировка `next_check_at NULLS FIRST,id`; `FOR UPDATE OF m SKIP LOCKED LIMIT`.
* Новые fetches имеют `monitor_id`, уникальность `(monitor_id,generation)`. Уникальность `(endpoint_id,generation)` действует только для старых строк с `monitor_id IS NULL`. Общие endpoints у разных мониторов закономерно имеют одинаковые поколения проверок.
* `ActiveMonitors` в текущем исходнике, вопреки старому имени аргумента, выбирает **один monitor по m.id**, присоединяя правила. Выборка активных мониторов endpoint осталась в repository-методе `SwitchEndpointType`, однако текущие runtime-вызовы этого метода не найдены. Файл 14 — дополнительный repository-only пример, не горячий путь актуального воркера.
* API получает правила через `monitor_id=ANY($1) ORDER BY monitor_id,id`; списки мониторов и уведомлений используют курсор `(created_at,id)`.
* Outbox `Claim` ищет неопубликованные события по `next_attempt_at`, учитывает lease, сортирует по сроку/id и атомарно захватывает одну строку через CTE/UPDATE.
* Ожидающее уведомление захватывается по ID через `ClaimNotification`; отдельной массовой выборки `pending` для отправки нет: ID приходит из RabbitMQ. Восстановление ищет `sending` с просроченным `next_attempt_at`.
* Inbox проверяет `(consumer_name,message_id)`, запись использует `INSERT … ON CONFLICT DO NOTHING`. HTTP-идемпотентность проверяет `key` и `expires_at`.
* Полноценной выборки «история endpoint с пагинацией» в репозиториях нет. Файл 09 сохраняет существующий `EndpointFetchRepository.Get` по fetch UUID; его текущие runtime-вызовы также не найдены, поэтому он помечен repository-only. Дополнительно сохранён реально вызываемый retention. Придуманный запрос истории не выдаётся за используемый приложением.

Существующие индексы, значимые для этих путей:

| Таблица | Индексы / ограничения |
|---|---|
| monitored_endpoints | PK; UNIQUE canonical_url; due/schedule partial indexes; target_hostname_id |
| monitors | PK; user/created_at DESC/id DESC partial; endpoint/status active partial; next_check_at/id due partial |
| monitor_rules | PK; monitor_id |
| endpoint_fetches | PK fetch_id; UNIQUE monitor_id/generation; legacy partial UNIQUE endpoint_id/generation; endpoint/created_at DESC; monitor/created_at; fetching lease partial; retention created_at |
| monitor_fetch_evaluations | PK fetch_id/monitor_id; FK к fetch и monitor |
| notifications | PK; UNIQUE idempotency_key; UNIQUE monitor/event/contact; pending next_attempt_at/id; contact pending/sending; monitor/created/id; retention created_at |
| outbox_events | PK; UNIQUE event_id; partial next_attempt_at/id для неопубликованных |
| processed_messages | PK consumer_name/message_id; processed_at |
| idempotency_keys | PK key; key/expires_at |
| user_contacts | PK; UNIQUE user/contact-type/contact; user_id; user/is_active |
| target_hostnames | PK; UNIQUE hostname; refresh lease partial |

Полные определения индексов выводит `validate.sql`. Все FK, NOT NULL, CHECK и UNIQUE остаются включёнными; после COPY корректируются только значения sequence. Таблица `schema_migrations` читается для проверки версии, сравнивается до/после, но генератор её не записывает. Начальную схему создаёт существующий штатный migration runner в новой БД.

## Запуск

Нужен Docker Compose. Команды выполняются из корня репозитория. Для сборки image задайте в окружении `GO_MODULE_GITHUB_TOKEN` с правом чтения приватного Go-модуля Fetcher: Compose передаёт его в BuildKit secret, а не в runtime-контейнер. Используйте только этот Compose-файл: он не подключает основной стек и `.env` приложения. Отдельная внутренняя сеть, отдельный volume, без опубликованных портов PostgreSQL. Для больших parallel hash joins выделен `/dev/shm` до 1 GiB: стандартных Docker 64 MiB может не хватить для полной проверки. Это ёмкость IPC-сегмента, а не изменение PostgreSQL planner settings. Запросы выполняются через `docker compose exec postgres psql`.

PowerShell:

```powershell
$env:LOADGEN_PASSWORD = [guid]::NewGuid().ToString('N')
docker compose -f tools/loadgen/compose.yaml up -d postgres
docker compose -f tools/loadgen/compose.yaml run --rm --build migrate
docker compose -f tools/loadgen/compose.yaml run --rm --build generate --config=/results/large.json --report=/results/large-report.json
```

В bash: `export LOADGEN_PASSWORD="$(openssl rand -hex 24)"`, далее те же команды Docker. Сохраните пароль в своём безопасном окружении для дальнейших подключений; в Git он не нужен. Сгенерированная hex-строка уже пригодна для URL. Если задаёте пароль сами, его необходимо URL-кодировать для connection URL.

Для повторного заполнения с явным подтверждением очистки **только неизменённого синтетического снимка**:

```powershell
docker compose -f tools/loadgen/compose.yaml run --rm generate --config=/results/large.json --reset --confirm-reset=monitoring_service_loadtest --report=/results/large-repeat-report.json
```

Поддерживается единственный профиль `large`, выбранный по умолчанию. Объёмы по таблицам остаются настраиваемыми в `large.json`. Отчёт должен иметь новый путь: существующие файлы не перезаписываются. После генерации файл появится в `docs/loadgen/`. Первый build может занять время из-за загрузки Go modules.

Для набора с примерно миллионом строк в основных таблицах используйте `--config=/results/million.json`. В нём 500 тыс. пользователей дают около 1,1 млн контактов, 1 млн мониторов требует 1,25 млн fetch и 1,5 млн правил, а outbox автоматически содержит 2,05 млн событий. Каталог типов контактов и служебные таблицы намеренно малы. `million.json` задаёт объёмы существующего режима `large`, а не новый режим генератора.

В новом терминале Compose снова потребует `LOADGEN_PASSWORD` для разбора конфигурации. Для SQL-доступа к уже работающему контейнеру без этой подстановки используйте `docker exec -it monitoring_service-loadgen-postgres-1 psql -X -U loadgen -d monitoring_service_loadtest`.

Для выделенной БД на другой машине можно собрать `go build -o loadgen ./cmd/loadgen` и задать **отдельные** `LOADGEN_ENV=test` и `LOADGEN_DATABASE_URL`. БД должна иметь имя с суффиксом `_loadtest`, роль `loadgen`, специальный database comment из `tools/loadgen/init.sql` и чистую миграцию 2. Connection URL приложения `DATABASE_URL` генератор не использует.

## Объёмы

| Таблица | large |
|---|---:|
| users, без migration seed | 100 000 |
| target_hostnames | 1 000 |
| monitored_endpoints | 1 000 000 |
| monitors | 2 000 000 |
| monitor_rules | 3 000 000 |
| endpoint_fetches | 2 500 000 |
| monitor_fetch_evaluations | 2 000 000 |
| notifications | 1 000 000 |
| outbox_events | 3 500 000 |
| processed_messages | 1 000 000 |
| idempotency_keys | 1 000 000 |

`contacts` содержит два реальных канала: `email`, `telegram`. У пользователя два подтверждённых активных контакта и примерно у 20% ещё один неподтверждённый неактивный email: около 2,2 контакта на пользователя. Существующий migration seed user id=1 сохраняется без изменений и входит в фактический count users.

Fetches больше исходной цели: 2 млн оценок должны принадлежать успешным fetches, при этом нужны pending/fetching/failed. Outbox больше 1 млн, потому что scheduler создаёт событие для каждой проверки, а завершение монитора — для каждого уведомления. В текущем коде Outbox retention отсутствует. Уменьшать его число за счёт отсутствующих событий было бы несогласованно. Inbox и notifications — сохраняемый поднабор истории; датасет содержит и просроченные записи для исследования действующего retention в семь суток.

## Настройки и воспроизводимость

Используется один профиль `large.json`; `--mode=large` необязателен. Снятые с поддержки профили не являются способом уменьшения объёма. Для технических проверок тесты задают ограниченные counts напрямую; пользовательские объёмы также можно менять в JSON.

Профиль large.json содержит все объёмы и доли. Параметры `as_of` и `date_from` можно добавить в RFC3339, например `2026-06-04T03:00:00Z`. Без них `as_of` — UTC начала запуска с точностью до секунды, `date_from` — за 90 суток до него. Отчёт сохраняет оба разрешённых значения. Для точного повтора скопируйте объект `config` из отчёта в новый JSON: **одинаковые counts, seed, as_of, date_from и распределения дают те же строки независимо от batch_size**. Планы после `ANALYZE` могут несколько отличаться из-за выборочной статистики PostgreSQL.

| Параметр | По умолчанию и смысл |
|---|---|
| batch_size | 10 000; допустимо 1–100 000; отдельная транзакция COPY на батч |
| seed | 42; независимые детерминированные потоки для свойств строк |
| monitor_statuses | monitoring 55%, success 30%, error 10%, stopped 5%; точные количества с округлением |
| endpoint_statuses | idle 60%, scheduled 15%, fetching 5%, retry_wait 15%, error 4%, access_denied 1%; вероятностное распределение старых общих endpoint-полей |
| fetch_statuses | succeeded 80%, failed 10%, pending 6%, fetching 4%; точные количества с округлением |
| notification_statuses | pending 15%, sending 5%, sent 70%, failed 7%, skipped 3%; вероятностное распределение |
| overdue_fraction | 40% сроков проверок; также доля просроченных lease среди leased jobs |
| error_fraction | 15% повторных ошибок среди pending notifications и неопубликованных событий; общая доля failed fetches задаётся fetch_statuses |
| unpublished_pending_fraction | 60% событий для **ещё pending** fetches/notifications; уже выполненные/захваченные задания всегда имеют опубликованное событие |
| html_fraction | примерно 30% endpoints HTML; остальные JSON |
| soft_deleted_fraction | примерно 3% активных idle мониторов без незавершённого fetch |

Эти пропорции — настраиваемая модель нагрузочного сценария, основанная на переходах из кода, а не измеренная production-статистика. До завершения исторического монитора уведомления могли отсутствовать/очищаться retention; skipped отражает факт пропущенной доставки в истории, а не обязательно текущее состояние контакта.

Около 70% мониторов принадлежат первым 10% пользователей; URL уникальны и нормализованы, но разделяются мониторами. Мониторы получают 1–3 правила в зависимости от заданного общего объёма. Используются реальные операторы `<`, `>`, `=`, `<=`, `>=`, `!=`; JSON-пути `price`, `stock.available`, `offers[0].price` без неподдерживаемого `$`, CSS/XPath для HTML, форматы `number`/`money`. Сессии остаются `managed/missing`, ciphertext отсутствует: пригодные токены для нагрузочного SQL не нужны.

Основные состояния исполнения и поколения находятся в monitors. Разнообразные поля старого `monitored_endpoints.status` сохраняют смысл исторического общего снимка; текущий scheduler не должен выбирать по ним. Все новые fetches имеют владельца monitor. У терминального монитора есть успешная HTTP-проверка и оценка; ошибка извлечения/сравнения отделена от транспортной ошибки fetch. Незавершённые fetches принадлежат только действующим неделетированным мониторам с актуальным поколением.

Ограничения совместимости конфигурации проверяются до подключения:

* правил от M до 3M;
* успешных fetches не меньше M; каждый монитор имеет хотя бы одну историческую HTTP-проверку;
* failed + pending + fetching не больше числа monitoring мониторов: этот генератор создаёт максимум одно дополнительное незавершённое/ошибочное задание на активный монитор;
* terminal monitors <= evaluations <= succeeded fetches;
* notifications <= 2 × terminal monitors;
* outbox_events=0 автоматически означает fetches + notifications; меньше этого числа нельзя; дополнительные события — повторные доставки pending fetches;
* processed_messages не больше доступных завершённых событий.

Для изменения распределения передавайте полный объект соответствующих весов; сумма 1. Нереализуемые сочетания отклоняются, генератор не подделывает связанные записи ради counts.

Предварительный просмотр без БД:

```powershell
go run ./cmd/loadgen --mode=large --config=docs/loadgen/large.json --dry-run
```

## Как формируются строки

Данные создаёт сам Go-код из `cmd/loadgen/data.go`, без чтения реальных пользователей и без вызовов API приложения. Для изменяемых свойств вычисляется SHA-256 от сочетания `seed / имя свойства / номер записи`: из результата получаются псевдослучайные доли, UUID, числа и даты. Словари статусов, операторы, пути и шаблоны URL/message заданы явно по текущей реализации приложения.

Связи вычисляются согласованно: endpoint для monitor, владелец fetch, контакт владельца monitor, ссылки Outbox на существующие fetch/notification. Распределение статусов monitors/fetches задаётся точными количествами с перемешиванием; часть других полей выбирается вероятностно по весам. После этого строки потоково передаются через `COPY` в порядке зависимостей таблиц, отдельными транзакциями по `batch_size`. Миллионы объектов одновременно в памяти не хранятся.

Одинаковые seed, counts, диапазон дат и распределения дают одинаковые строки. Это синтетическая модель нагрузки, а не проигрывание всех событий через настоящие scheduler/workers.

## Изменение схемы

Генератор автоматически не адаптирует модель данных к новым миграциям. Список таблиц, явные списки колонок COPY и правила формирования строк написаны под текущую схему на миграции 2.

| Изменение | Текущее поведение |
|---|---|
| Новая миграция, например 3, или dirty migration | guard откажет до COPY и reset: требуется чистая версия 2; генератор привязан к текущей схеме |
| Дополнительная таблица в public при прежней версии | guard откажет из-за неизвестной таблицы; заполнять её самостоятельно не станет |
| Новая nullable-колонка или колонка с DEFAULT при прежней версии | COPY передаёт только перечисленные колонки; PostgreSQL может заполнить новую NULL/default, осмысленные значения генератор не выведет |
| Новая NOT NULL-колонка без DEFAULT, удаление/переименование используемой колонки, несовместимый тип/CHECK/FK при прежней версии | PostgreSQL отклонит соответствующий COPY/запись; уже зафиксированные предыдущие батчи сохранятся |

Это не полный предварительный diff схемы: изменения колонок в обход нормальных миграций могут обнаружиться лишь при работе с соответствующей таблицей. Дополнительные несвязанные таблицы вне public не входят в модель; внешние FK на известные application-таблицы проверяются отдельно.

При развитии схемы нужно вместе с миграцией актуализировать `counts`/конфигурацию, `tables`, задания `jobs()` и порядок зависимостей, функции `*Row`, бизнес-проверки `validation.sql`, а при изменении application SQL — файлы EXPLAIN. Только после проверки совместимости следует обновлять разрешённую версию миграции в `guard`. Одна замена номера совместимость не обеспечивает. FK/UNIQUE/NOT NULL для итоговой проверки уже читаются из актуального PostgreSQL catalog, но это не заменяет обновление правил генерации.

## Проверка результата

```powershell
docker compose -f tools/loadgen/compose.yaml run --rm generate --validate-only
docker compose -f tools/loadgen/compose.yaml exec -T postgres psql -U loadgen -d monitoring_service_loadtest
```

В psql можно выполнить `\i` после копирования SQL в контейнер или передать содержимое файла через stdin. В PowerShell:

```powershell
Get-Content -Raw docs/loadgen/validate.sql | docker compose -f tools/loadgen/compose.yaml exec -T postgres psql -X -v ON_ERROR_STOP=1 -U loadgen -d monitoring_service_loadtest
```

`--validate-only` читает исходную конфигурацию из завершённого manifest, повторяет проверки и сравнивает содержимое таблиц с сохранёнными контрольными суммами. `validate.sql` выводит логические нарушения, counts/распределения и определения индексов. Полные автоматические проверки всех FK, NOT NULL и уникальных индексов выводятся из PostgreSQL catalog в `cmd/loadgen/validate.go` и выполняются генератором. Нулевые counts нарушений означают успех; ненулевые приводят к ненулевому exit code CLI. Отчёт содержит точные количества, распределения, кандидатов scheduler/workers/Outbox/notifications, время по таблицам, время загрузки, общее время с проверками и `pg_database_size`.

## Исходные планы SQL

`docs/loadgen/sql/01…17` содержат SQL из актуальных Go string literals, исходный файл и имя метода указаны в комментарии. Вспомогательная выборка параметров вне EXPLAIN предназначена только для выбора существующего ID. Запуск одного файла:

```powershell
Get-Content -Raw docs/loadgen/sql/01-scheduler.sql | docker compose -f tools/loadgen/compose.yaml exec -T postgres psql -X -v ON_ERROR_STOP=1 -U loadgen -d monitoring_service_loadtest
```

Для всех файлов:

```powershell
Get-ChildItem docs/loadgen/sql/*.sql | Sort-Object Name | ForEach-Object {
    Get-Content -Raw $_.FullName | docker compose -f tools/loadgen/compose.yaml exec -T postgres psql -X -v ON_ERROR_STOP=1 -U loadgen -d monitoring_service_loadtest
    if ($LASTEXITCODE -ne 0) { throw "EXPLAIN failed: $($_.Name)" }
}
```

SQL содержит `EXPLAIN (ANALYZE, BUFFERS, SETTINGS)` и `ROLLBACK`. Варианты claim/retention/RecordOnce действительно исполняют DML, поэтому их следует запускать только в этой тестовой БД; PostgreSQL описывает этот режим в [EXPLAIN](https://www.postgresql.org/docs/16/sql-explain.html). Rollback сохраняет логические данные, но не исключает WAL, изменения страниц, эффект cache и bloat: сравнивайте планы при одинаковых условиях. Не считайте первый запуск гарантированно cold cache. `NOW()` в оригинальных запросах остаётся настоящим временем: доля просроченных задач со временем меняется относительно фиксированного `as_of`.

Повторно извлечь SQL после изменения приложения:

```powershell
go run ./tools/loadgen/export-queries.go
```

Инструмент разбирает Go AST, копирует найденные SQL-строки и не меняет их фильтры, JOIN или индексы. Файлы 02/03 показывают отдельные SQL-фазы ClaimFetch, а не всю бизнес-транзакцию целиком. Никаких SQL-оптимизаций и новых application indexes в этой задаче нет.

## Защита данных

Генератор отказывается работать при `LOADGEN_ENV` вне local/test, production в APP_ENV/ENVIRONMENT/GO_ENV/NODE_ENV, неверных роли/имени БД/marker, миграции вне 2, посторонних подключениях, дополнительных public-таблицах/внешних FK/пользовательских триггерах. На первом запуске допускается только начальный аккаунт администратора; остальные application-таблицы должны быть пустыми.

Reset требует одновременно `--reset --confirm-reset=monitoring_service_loadtest`. В транзакции берутся блокировки application-таблиц, проверяются counts и две независимые агрегированные контрольные суммы содержимого по manifest; любое изменение запрещает очистку. Используется явный TRUNCATE без CASCADE, seed user сохраняется. `loadgen_meta` — отдельная служебная схема manifest; она не участвует в запросах приложения.

После оборванной загрузки committed батчи остаются для диагностики. Автоматическая очистка частичного снимка запрещена: создайте новую отдельную БД/Compose project либо явно пересоздайте тестовый volume после ручной проверки. Генератор не содержит force-reset и не вызывает DROP DATABASE.

Все URL/email заканчиваются на `.invalid`, Telegram ID — фиктивные отрицательные значения, пароли непригодны для входа. В процессе нет HTTP-клиента, RabbitMQ, Redis, SMTP/Telegram-клиентов, scheduler или worker. В Compose отсутствуют эти сервисы, а внутренняя сеть закрывает внешние подключения. Никогда не подключайте приложение и publisher к этой БД: миллионы сохранённых событий специально остаются доступными для SQL-исследования.


