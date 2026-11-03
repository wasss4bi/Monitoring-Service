# Восстановление DNS и ручная проверка Lamoda

Состояние на 2026-11-11 (Asia/Omsk). Ozon и «Лемана ПРО» исключены из этого
этапа по указанию пользователя. Предыдущие результаты:
[живые карточки](live-cards-2026-11-10.md).

Последний подтверждённый результат Lamoda: JSON API через HTTP_JSON/mTLS
gRPC с предоставленной анонимной сессией дважды вернул HTTP200,
`sku=MP002XM0281A`, `price=21591`, `sizes[9].stock_quantity=1`.
После согласования [конкретный план](lamoda-api-switch-proposal.md) применён:
endpoint9 использует HTTP_JSON, provided-копия34 успешно проверила оба
исходных условия по сетевым данным. Telegram notification33 отправлена
11 ноября10:04:08 Asia/Omsk, status=sent, attempts=1.

## DNS

Исправлены две причины starvation при hostname quota 1/min:

- Если Fetcher явно сообщает `network_performed=false` и необходимость
  сессии, уже зарезервированный permit используется для PrepareSession.
  Раньше единственный permit уходил на обнаружение отсутствующей сессии,
  после defer этот цикл начинался заново.
- После настоящего сетевого отказа сохранённой сессии worker может дождаться
  следующего quota window перед подготовкой. Ожидание разрешено только когда
  текущий attempt budget вмещает его и полный 100-секундный Prepare RPC.
  Затем permit запрашивается повторно; при конкуренции quota не обходится.
  DB transaction во время ожидания не удерживается. Общие budgets и lease
  не увеличены; cancellation соблюдается. RPC budget проверяется до ожидания.

Через настоящий admin login/PATCH `/target-hostnames/7` добавлено единственное
bootstrap исключение: origin `https://www.dns-shop.ru`, path prefix
`/__qrator/validate`, метод `POST`. Оно нужно для запроса самой гостевой
страницы; ordinary monitoring остаётся GET-only. Quota сохранена 1/min.
Остальные сетевые запреты не изменены. Старый allowlist сохранён в ignored
`tmp/site-recovery/dns-policy-before.json`.

Монитор 33 — помеченная managed-копия исходного18, с исходным условием
`data.offers.price <= 5799`, без изменения порога. Штатный bootstrap сохранил
новую сессию; после временной connect failure обычный retry получил
**HTTP200, цену699 RUB, VALUE_ORIGIN_NETWORK** и завершил монитор успешно.
Уведомление32 отправлено worker с `status=sent`,
`2026-11-10T17:05:59Z` (23:05:59 Asia/Omsk). Ручного sendMessage не было.

Отдельный последующий wire Fetch после перезапуска parser получил timeout;
лог видел HTTP401. Этот standalone контроль не запускает бизнес-процедуру
managed refresh и не доказывает бессрочную валидность cookies. Сам механизм
refresh проверен отдельно на реальном минутном quota window.

## Проверки исправления

- Регрессия до исправления воспроизведена: `fetches=1 preparations=0` при
  явном отсутствии сетевого обращения.
- Unit suites service/ratelimit/fetcherclient прошли; проверены достаточный
  и недостаточный budget, повторное занятие quota другим запросом и cancellation.
- На отдельном PostgreSQL прошли девять browser/session lifecycle сценариев
  и три коротких варианта нового quota-теста.
- Дополнительный `network_with_budget` прошёл за62 секунды с настоящим
  минутным окном: preparation выполняется, сессия сохраняется ready, повторная
  доставка не повторяет работу, post-prepare Fetch откладывается по quota.
- Окончательный Monitoring-Service image собран и deployed. GHCR Fetcher v0.1.1 и wire
  contract не изменялись. Commit/push не выполнялись.

## Lamoda: ручной этап

Прежний HTTP/json_source код совпадает с историческим успешным вариантом.
Текущая страница возвращала HTTP200 с CAPTCHA без SKU/`__NUXT__`; product API
возвращал HTTP403. Также наблюдались временные proxy/DNS ошибки и отсутствие
UDP AAAA ответа при корректном NODATA через HTTPS DNS-контроль.
Результат не заменялся тестовым или кэшированным товаром.

Пользователь выбрал подготовку отдельного анонимного профиля и самостоятельное
прохождение проверки. Использован существующий optional `manual` image target
из репозитория Fetcher. Контейнер `fetcher-lamoda-manual` и отдельный volume
`monitoring_service_lamoda_manual_20261110` не открывают рабочие Browserless profiles.
Личные логины не нужны. Ключи/cookies и пароль desktop не выводятся в логи.

Desktop доступен по `http://127.0.0.1:6081/vnc.html?autoconnect=1&resize=scale`.
Пароль хранится в local ignored `tmp/site-recovery/lamoda-vnc-password.txt`
с Windows ACL только оператора и SYSTEM. Проверка сайта выполняется человеком.

Первый localhost relay создавал Node-процесс внутри контейнера на каждое TCP
соединение. При загрузке GUI исчерпался PID/thread limit256, поэтому desktop
перестал отвечать. Исправлено 2026-11-11: remote relay использует установленный
Python без дополнительных потоков, ограничен восемью соединениями и завершает
их после disconnect; нормальное закрытие stdout больше не обрывает ответ.
Лимит256 сохранён. Три серии по шесть параллельных HTTP assets прошли с
проверкой полного Content-Length, WebSocket upgrade и настоящий RFB banner
проверены; pids.current до/после195/194, listener остался доступен.

Контейнер остаётся только в internal `monitoring_service_browser_egress`. Предложение
подключить стандартную bridge было отклонено automatic approval review;
это действие не выполнялось. Localhost tunnel через фиксированный `docker exec`
не расширяет outbound доступ. Его временный код/логи находятся в репозитории
Fetcher, `tmp/site-recovery/lamoda-manual-forward.cjs`, PID-файл — в Monitoring-Service
`tmp/site-recovery/manual-forward.pid`.

Ожидается подтверждение пользователя, что карточка открылась. **Успешное
получение Lamoda и импорт проверенной сессии пока не выполнены.** После
подтверждения нужно проверить SKU, цену/наличие, передать только opaque session
в существующий контракт и повторить рабочий мониторинг; CAPTCHA не решается
скриптом. До конца handoff desktop и его профиль сохраняются.

### Контроль после сообщения о бесконечной CAPTCHA

Пассивная проверка не обнаружила ошибок scripts/network, заблокированных
resources, выключенных cookies или рассогласования часов. В cgroup событиях
после исправления desktop `pids.events max=0`, memory/OOM events=0. На экране
действительно страница проверки Lamoda. Эти наблюдения не устанавливают,
по какому признаку сайт отклоняет проверку.

В рамках выбранной ручной подготовки выполнен один контроль в уже существующем
Direct mode: обычный Chrome157, тот же profile volume, Squid, пароль, internal
network и ресурсные лимиты; `PARSER_MANUAL_DIRECT=true`. Старый observed
контейнер штатно остановлен и сохранён как
`fetcher-lamoda-manual-observed-20261111`. Активное имя desktop и localhost URL
сохранились. Подтверждены `mode=manual_direct`, `browser_running=true`, один
владелец профиля, отсутствие `--remote-debugging*` и HTTP200 desktop.

Пользователю предложено повторить проверку один раз. После открытия карточки
нужно закрыть Chrome его штатным меню «Выход», сохранив профиль. До окончания
этого контроля CDP к Direct Chrome не подключается. Результат Direct и
работоспособность Lamoda через gRPC пока не подтверждены.

Пользователь сообщил, что в Direct CAPTCHA также зациклена. На этом этапе
успешной проверенной сессии не было, её экспорт/импорт не выполнялся. Это не доказывает
отказ по IP, CDP, cookies или другому конкретному признаку.

Повторные обращения остановлены: диагностический контейнер оставлен в
`paused` с сохранением RAM и volume, localhost relay остановлен. Рабочий
Fetcher не остановлен. Следующий шаг зависит от текущего доступа к той же
карточке в обычном браузере Windows без входа в аккаунт; этот факт запрошен
у пользователя. Для возврата к diagnostic desktop нужны unpause и запуск
того же relay; нельзя открывать этот profile volume вторым владельцем.

## Lamoda: проверка предоставленного запроса браузера

Пользователь подтвердил, что карточка открывается в обычном Windows-браузере
без входа в аккаунт, и сохранил успешный GET основного документа как cURL.
Текст команды разобран как данные, не исполнялся. Проверены GET, точные
origin/path, отсутствие Authorization. Из 30 first-party cookies и технических
browser headers собрана opaque provided-сессия существующего формата.
Сравнение имён/значений cookies, User-Agent и client hints прошло. Исходные
cookie attributes (HttpOnly/expiry/path) из request header восстановить нельзя.
Файлы защищены ACL и исключены из Git/build context; содержимое не логируется.

С предоставленной сессией INLINE_JSON получает CAPTCHA, rendered HTML
не находит метаданные товара. Повтор с точными исходными HTML headers также
получил CAPTCHA. Причина решения сайта не установлена.

Однако **прежний product JSON API работает через HTTP_JSON**. Два gRPC
запроса вернули из сети цену21591, SKU и остаток1. Второй включал новый regex
ключ извлечения SKU, исключающий ложный вывод по старому кэшу; все четыре
результата VALUE_ORIGIN_NETWORK. Возвращённая обновлённая сессия сохранена
приватно. Полное тело не сохранялось. Корневые JSON paths совпадают с
исходными правилами монитора22; пороги не менялись.

Контроль API без перенесённых cookies: SessionModeNONE и пустая managed
сессия завершились DeadlineExceeded. Это не доказывает обязательность cookies,
но рабочего полностью автоматического bootstrap этим контролем не получено.

Endpoint9 используется пятью завершёнными мониторами (9,10,16,22,28),
активных/pending/fetching проверок перед переключением не было. Его старый
`json_source` выбирал неработающий сейчас HTML-путь. Пользователь разрешил
очистку json_source и создание одной provided-копии с исходными условиями.

Применение выполнено в короткой транзакции с проверкой URL, старого descriptor,
точных ID/generation/status пяти подписчиков и отсутствия активных jobs.
Повышена только generation (+updated_at) исходных мониторов, json_source
endpoint очищен. Сохранён rollback snapshot; контрольные суммы остальных
полей мониторов и всех полей правил совпали до/после.

Новая копия34 сохранила оба правила и rules_count_to_finish=2. Обычный
scheduler/outbox/RabbitMQ/worker/mTLS Fetcher/finalization/notification worker
получил HTTP200, elapsed673ms, price21591 и stock1, оба VALUE_ORIGIN_NETWORK,
2026-11-11T04:03:59Z. Успех, одна evaluation, один notification33 telegram
со статусом sent и attempts1 в04:04:08.264351Z (10:04:08 Asia/Omsk).
Pending jobs=0, provided session ready. Проверено точное равенство copied rules,
сохранность пяти оригиналов и отсутствие body/updatedSessionState в fetch
metadata. Новых ручных сообщений или подмены значений не было.

Это рабочий provided-путь: при отказе сессии нужен новый анонимный экспорт.
Автоматический managed bootstrap Lamoda остаётся неподтверждённым.

## Задержка подключения gRPC

Холодное подключение к `fetcher:8443` могло исчерпывать RPC budget ещё до
внешнего запроса: DNS resolver gRPC синхронно ожидает необязательный TXT
`_grpc_config.fetcher`. Сравнение с одинаковыми hostname, сертификатами и
5-секундным context: DNS service config включён — DeadlineExceeded около8с;
выключен — health SERVING за7мс. Диагностический IPv4 override не сохраняется
в конфигурации; TLS hostname verification не отключалась.

В thin client добавлен `grpc.WithDisableServiceConfig()`: конфигурация
соединения принадлежит приложению. Retry и RPC budgets не меняются.
Regression test с локальным UDP DNS (A отвечает, TXT молчит) до исправления
падает, после проходит без единого TXT запроса. Он и mTLS identity test прошли
пять повторов; suites fetcherclient/service/ratelimit также прошли.

Штатная сборка дважды не смогла загрузить Alpine indexes из-за DNS; бинарники
пересобраны с `--network=none` поверх ранее работающего локального image с
теми же скачанными зависимостями. Постоянные Dockerfiles/Compose не менялись.
Все семь Monitoring-Service процессов обновлены. Новый холодный клиент с действующим
hostname `fetcher:8443` и mTLS подтвердил direct SERVING за29мс, browser за1мс.
Fetcher GHCR images и PostgreSQL не пересоздавались.
Внутри обновлённого Linux image suites fetcherclient/service/ratelimit также
прошли; все семь обновлённых процессов running.
