# Живые проверки карточек — 2026-11-10

Проверки выполняются на рабочем локальном Compose после перехода на контракт
`github.com/wasss4bi/values-from-body-fetcher v0.1.1` и пять закреплённых GHCR
образов `v0.1.1`. Реальные Telegram-уведомления явно разрешены пользователем.
Это проверка интеграции и текущей доступности конкретных сайтов, не измерение SLA.

## Подготовка и границы проверки

- До запуска в БД было 26 мониторов, все в terminal `success/error`, без активного
  backlog; RabbitMQ и неопубликованный outbox были пусты.
- Перед миграциями сделан private custom-format PostgreSQL backup:
  `/tmp/monitoring_service-before-fetcher-live-20261110.dump` **в существующем контейнере
  postgres**. Оглавление проверено через `pg_restore -l`. Этот backup исчезнет
  при удалении контейнера: это не независимая внешняя резервная копия.
- Go migration runner успешно применил версии 22–25; schema version 25,
  `dirty=false`. Однократно создан администратор из локального ENV. Выполнен
  ранее разрешённый managed-only cutover: 21 managed-сессия сброшена,
  пять ready provided-сессий и статусы исходных мониторов сохранены.
- Созданы development mTLS сертификаты в игнорируемом `tmp/fetcher-certs`;
  leaf действуют семь дней. Старые browser volumes не удалялись.
- Fetcher, API, scheduler, outbox publisher, JSON/HTML workers, notification
  worker и dependency worker запущены в рабочем Compose.
- Fixture driver вызывает обычный `MonitorRepository.Create`, создавая только
  помеченные копии карточек пользователя. Дальше выполняется настоящий путь
  scheduler → outbox → RabbitMQ → worker → mTLS gRPC Fetcher → атомарное
  завершение → notification worker → Telegram `sendMessage`.
- Исходные URL и селекторы сохранены в тестовых копиях; для явного положительного
  сценария установлен **тестовый порог `цена > 0`** и один необходимый price rule.
  Успех такого теста не означает достижения исходного целевого порога цены.
  Исходные правила и завершённые мониторы не возобновлялись и не редактировались.
- Для дополнительной XPath-проверки Citilink исходный CSS `span.class...`
  преобразован в эквивалентный XPath с точным сопоставлением токенов классов.
  Это отдельный тестовый монитор, не изменение пользовательского селектора.

## Подтверждённые результаты

| Тестовый монитор | Исходный монитор / карточка | Способ | Результат |
| --- | --- | --- | --- |
| 28 | 22 / Lamoda Lacoste MP002XM0281A | inline JSON + JSONPath | `error`, HTTP 200, `INVALID_DOCUMENT`; диагностический ответ содержит CAPTCHA, без `__NUXT__`, product и SKU |
| 30 | 8 / Citilink BLOODY MN340W | HTML + CSS | `success`, HTTP 200, реальная цена `27 990`, `VALUE_ORIGIN_NETWORK` |
| 31 | 13 / Ozon iPhone 14 Pro 5892688031 | JSONPath, provided session | `error/access_denied`, HTTP 403, `SESSION_REJECTED`; provided-сессия тестовой копии заблокирована, без guest refresh |
| 32 | 8 / Citilink BLOODY MN340W | HTML + XPath | `success`, HTTP 200, реальная цена `27 990`, `VALUE_ORIGIN_NETWORK` |
| 29 | 17 / «Лемана ПРО» Twin 87164932 | HTML + XPath | После ограниченного повтора `error/access_denied`, HTTP 401, `UPSTREAM_HTTP`; цена не получена |
| 27 | 12 / DNS кабель Keyron | browser JSON capture + JSONPath, provided session | После target timeout и retry `error/access_denied`, HTTP 401, `SESSION_REJECTED`; цена не получена |

Первый запрос Citilink получил `DNS_FAILURE`; штатный retry получил цену и
завершил монитор успешно. Это также проверяет восстановление после временного
сетевого отказа. Оба успешных результата получены из сети, не из value cache.

Уведомления 26/27/28/29 (мониторы 28/30/31/32) имеют `status=sent`, по одной
попытке, без last_error. Время приёма Telegram API в UTC соответственно:
`16:23:47`, `16:34:38`, `16:36:26`, `16:37:09`. Два сообщения об успехе и два
об ошибке отправлены существующим worker; прямой ручной отправки не было.
`sent` подтверждает успешный ответ Telegram API, а не прочтение пользователем.

Уведомление 30 для теста 29 («Лемана ПРО») также `sent` с первой попытки,
`2026-11-10T16:39:49Z`. Таким образом подтверждены **пять отправленных
сообщений: два успеха и три ошибки**.

DNS (тест 27, исходный 12, provided browser capture) и «Лемана ПРО» (тест 29,
исходный 17, managed HTML/XPath) первоначально не завершились. Наблюдались
транспортные откладывания, один `DNS_FAILURE` для «Лемана ПРО»; отдельный
ограниченный HTML-запрос видел HTTP 401. После диагностической остановки
воркеров и перезапуска HTML engine тестовым мониторам разрешён один
ограниченный повтор. «Лемана ПРО» штатно завершился отказом HTTP 401 и одной
terminal evaluation. У DNS полный RPC budget закончился `UPSTREAM_TIMEOUT`,
после чего штатно назначен target retry; лог parser при этом видел HTTP 401.
Следующий DNS target retry штатно завершился HTTP 401 / `SESSION_REJECTED`
в `16:41:56Z`, без guest refresh предоставленной сессии. В итоге все шесть
тестовых мониторов завершены, активных тестовых проверок не оставлено.

## Проверки кода и ограничения

Локально удалён лишний одиночный символ `F` в конце
`internal/fetcherclient/client.go`, мешавший компиляции. Сохранена приватная
копия файла до исправления. Runtime image уже содержал корректную версию
файла; переключение поведения для этого исправления не требовалось.

`go test ./internal/fetcherclient ./internal/service ./internal/ratelimit
./internal/httpapi/...` прошёл с локальным игнорируемым GOCACHE. Первый запуск
в стандартном Windows cache получил `Access is denied`; это ошибка доступа
к cache, а не провал assertion. Изолированные PostgreSQL/RabbitMQ integration
scenarios и contract/browser fixtures проверены ранее в
[отчёте выделения](validation-2026-11-10.md) и
[аудите Go module](contract-module-audit.md); этот прогон их не заменяет.

HTTP/OpenAPI и gRPC контракт не менялись. Commit/push не выполнялись.
Игнорируемые fixture helpers и redacted логи находятся в
`tmp/live-card-validation`; bot token, chat ID и session state в этот отчёт
не включены. Успех извлечения JSON-цены из живой карточки этим прогоном пока
не подтверждён: отказы Lamoda/Ozon/DNS нельзя считать успешной экстракцией.

Проверка сохранности исходных данных: исходных мониторов 26, все остались
terminal; исходных правил 37; все пять исходных provided-сессий сохранили
`ready`. Для каждого завершённого теста 28/29/30/31/32 записана ровно одна
terminal evaluation и ровно одно уведомление. Для DNS 27 также одна terminal
evaluation и одно уведомление после штатного retry. Заблокированы только
provided-сессии тестовых копий Ozon 31 и DNS 27, не исходных 13 и 12.

## Окончательное состояние

Пять существовавших в БД карточек, шесть тестовых мониторов: **два успеха и
четыре отказа сайтов**. Успешные значения получены через CSS и XPath на
Citilink; Lamoda отдаёт CAPTCHA, Ozon — HTTP 403, DNS и «Лемана ПРО» — HTTP 401.
Сквозная обработка и уведомления работают, но успешное получение данных со
всех выбранных сайтов не подтверждено.

Уведомление 31 для DNS 27 добавляет шестую отправку: всего **шесть сообщений**
(два успеха, четыре ошибки), без ручного вызова Telegram. Итоговые состояния,
время отправки и число evaluations сохранены в игнорируемом redacted
`tmp/live-card-validation/final-state.json`. Активных/pending тестовых fetch
нет, response bodies в тестовых fetch не сохранялись. API `/health` вернул
HTTP 200, обе dependency capability `direct/browser` находятся в `up`;
основной стек оставлен запущенным.
