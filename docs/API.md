# API

База: `/api`. JSON, поля в `camelCase`, время — **миллисекунды Unix**, деньги — целые рубли.
Авторизация — пара httpOnly-cookie (подробности и схема в [AUTH.md](AUTH.md)): `session` — access-токен
(JWT, 15 минут, `Path=/`) и `refresh_token` — refresh-токен (7 суток бездействия, не дольше 30 суток с входа,
`Path=/api/auth`). Для не-браузерных клиентов поддержан заголовок `Authorization: Bearer <access-токен>`.
Идентификатор пользователя **никогда** не передаётся в запросах — сервер берёт его из сессии.

Ошибки: `{ "error": "текст для пользователя", "code": "machine_code" }` + HTTP-статус
(400 валидация, 401 не вошли, 403 нет прав (`forbidden`), аккаунт заблокирован (`account_blocked`) или чужой Origin (`bad_origin`), 404, 409 конфликт состояния, 429 лимит входа, 413/415 загрузка).

## Таймеры
Не считайте оставшееся время по `Date.now()` клиента: запросите `GET /api/time` → `{ "now": ms }`,
запомните разницу с `Date.now()` и вносите её в расчёт. Статус лота сервер уже отдаёт с учётом `endsAt`.
Обратный отсчёт считается на клиенте до `endsAt`; `endsAt` может **вырасти** (Anti-snipe, см. ниже), поэтому
всегда берите актуальное значение из последнего ответа или WebSocket-события. Серверное время приходит и в
каждом WebSocket-сообщении (`now`), так что поправку можно уточнять без отдельных запросов.

## Справочники
| Метод | Путь | Ответ |
|---|---|---|
| GET | `/api/categories` | `[{ id, name, description, subs: [{ id, name }] }]` |
| GET | `/api/dorms` | `[{ id, name }]` |
| GET | `/api/time` | `{ now }` |

## Авторизация
| Метод | Путь | Тело | Ответ |
|---|---|---|---|
| POST | `/auth/register` | `{ name, username, telegram, dormId, password, confirm }` | 201 `User` + cookie |
| POST | `/auth/login` | `{ username, password }` | `User` + cookie (10 попыток / 5 мин с одного IP) |
| POST | `/auth/refresh` | — (нужна cookie `refresh_token`) | `{ status, accessExpiresAt }` + новые cookie (ротация) |
| POST | `/auth/logout` | — | `{ status }`; завершает текущую сессию, стирает cookie; идемпотентен |
| POST | `/auth/logout-all` | — | `{ status }`; завершает все сессии пользователя |
| GET | `/auth/me` | — | `User` или 401 |

Ошибки `/auth/refresh`: `401 invalid_refresh` (нет/неизвестный/просроченный/отозванный токен),
`401 refresh_reused` (предъявлен уже использованный токен — вся сессия отозвана), `409 refresh_conflict`
(другая вкладка только что обновила тот же токен; cookie не менялись — повторите исходный запрос, а при новом 401 —
`/auth/refresh` ещё раз). При 401 cookie стираются. Другие запросы с чужого `Origin` → `403 bad_origin`.

Правила: никнейм 3–32 (буквы/цифры/`_.-`, без учёта регистра уникален); Telegram 5–32 (`@` можно),
уникален; пароль 6–72 байта; `dormId` — из `/dorms`.

`User = { id, username, name, telegram, dormId, createdAt, role }` — отдаётся только владельцу.
`role`: `user` или `admin` (администраторов назначает только команда `make admin-create`, см. «Админ-панель»).
Администратор, созданный командой, может не иметь Telegram и общежития (`telegram: ""`, `dormId: 0`).

**Блокировка.** Заблокированный пользователь не может войти (`403 account_blocked`, пароль при этом проверяется
первым — неверный пароль по-прежнему даёт 401), а уже выданные ему токены перестают работать сразу:
любой запрос с сессией возвращает `403 account_blocked` и стирает cookie, ставки по WebSocket отклоняются.
Чужие профили не публичны: имя автора лота — `authorUsername`, имена в ставках — `username`.

## Профиль (нужна сессия)
| Метод | Путь | Тело / ответ |
|---|---|---|
| PUT | `/me/profile` | `{ name, telegram, dormId }` → `User` |
| PUT | `/me/username` | `{ newUsername }` → `User` |
| PUT | `/me/password` | `{ current, newPassword, confirm }` → `{ status }`; текущий пароль проверяется |
| GET | `/me/stats` | `{ created, participated, won, sold, rating, complaints }` (`rating = won + sold`, жалоб пока нет → 0) |
| GET | `/me/lots` | `[Lot]` — мои лоты (все статусы) |
| GET | `/me/bids` | `[Lot]` — лоты, где я ставил; `myMaxBid` — моя максимальная ставка |

## Лоты
`Lot = { id, title, description, image, category, subcategory, authorId, authorUsername, dormId, startPrice,
step, currentPrice, minBid, bidsCount, leaderId|null, myMaxBid, createdAt, endsAt, status, contact?,
durationSec, startsAt|null, hidden?, moderationReason? }`

* `status`: `pending | approved | rejected | active | finished | cancelled` (`active` уже с учётом `endsAt`).
  Жизненный цикл: новый лот → `pending` (ждёт администратора) → `approved` (одобрен, ждёт назначенного
  начала) или сразу `active` → `finished`; `rejected` — отклонён; `cancelled` — отменён автором или администратором.
  Публично (каталог, `GET /lots/{id}`, ставки) видны только `active`, `finished` и `cancelled`, не снятые с публикации.
  Лот в статусах `pending`, `approved`, `rejected` или снятый с публикации (`hidden`) видят только автор, администратор и
  пользователи, уже делавшие на него ставки; для остальных — `404`. «Мои лоты» (`/me/lots`) показывают все статусы.
* `durationSec` — заявленная длительность аукциона; `startsAt` — когда аукцион начался (или начнётся, если лот `approved`).
  Пока лот не `active`, `endsAt` лишь расчётный. Таймер на странице показывайте только для `active`.
* `moderationReason` — причина отказа, снятия или отмены; видна автору лота (и администратору).
* `currentPrice` — лидирующая ставка или `startPrice`; `minBid` — `startPrice`, пока ставок нет (первая ставка может равняться стартовой цене), далее `currentPrice + step`.
* `dormId` — общежитие продавца. `myMaxBid` = 0 для анонима или если ставок нет.
* `contact = { role, name, telegram }` — **только** продавцу и победителю завершённого лота со ставками
  (`role`: «Победитель» / «Продавец»). Остальным поля нет.

| Метод | Путь | Описание |
|---|---|---|
| GET | `/lots` | Каталог (только опубликованные лоты). Query: `category`, `sub`, `q`, `status` (`active` по умолчанию, `finished`, `all`), `sort`, `priceMin`, `priceMax`, `dormId`, `page`. → `{ lots, page, totalPages, total }`, 20 на страницу |
| GET | `/lots/{id}` | Один лот |
| POST | `/lots` | Подать заявку на лот (сессия) → 201 `Lot` со статусом `pending`; аукцион начнётся после одобрения администратором |
| DELETE | `/lots/{id}` | Отменить: только автор, только активный и **без ставок** → `Lot` |
| GET | `/lots/{id}/similar` | До 4 активных лотов той же категории |
| GET | `/lots/random` | `{ id }` случайного активного (`category`, `sub` опционально), `{ id: null }` если нет |

`sort`: `ending` (по умолчанию: активные по близости окончания), `new`, `price_asc`, `price_desc`,
`title_asc`, `title_desc`, `bids`. Поиск `q` — по названию и описанию без учёта регистра.

### Создание лота — два шага
1. `POST /api/uploads` (multipart, поле `file`, JPEG/PNG/GIF/WebP до 5 МБ, тип определяется по содержимому) → `201 { url: "/uploads/<имя>" }`.
2. `POST /api/lots` `{ title, description, image: <url из шага 1>, category, subcategory, startPrice, step, hours }`.

Лимиты: название ≤300, описание ≤5000, стартовая цена 1…100 000 000, подкатегория должна принадлежать категории.
Локальные `blob:`-ссылки не принимаются. Границы **шага** и **длительности** задаёт администратор
(`GET /admin/settings`; по умолчанию шаг ≥ 1 ₽, `hours` от 1 минуты (0.0167) до 720 (30 дней)).
`hours` можно не передавать — тогда берётся время автозавершения по умолчанию (1 сутки, если не менялось).
Явный `hours: 0` — ошибка валидации.

## Ставки
| Метод | Путь | Описание |
|---|---|---|
| GET | `/lots/{id}/bids` | `[{ id, lotId, userId, username, amount, createdAt }]`, новые первыми |
| POST | `/lots/{id}/bids` | `{ amount }` → 201 `{ bid, lot, extended }` (лот уже обновлён; `extended: true` — сработал Anti-snipe) |

Правила проверяет сервер (транзакция с блокировкой лота): аккаунт не заблокирован (403 `account_blocked`),
не свой лот (403 `own_lot`), лот опубликован (иначе 404), аукцион не закрыт (409 `lot_closed`), не лидер (409 `already_leading`), `amount ≥ minBid` (400 `bid_too_low`).
Клиентские проверки оставьте только для удобства — доверять им нельзя.

### Anti-snipe
Ставка, принятая меньше чем за «окно» (по умолчанию 120 с) до конца аукциона, переносит конец на
«момент ставки + продление» (по умолчанию 120 с). Срок только растёт и никогда не сокращается;
продлений может быть сколько угодно. Окно 0 выключает защиту. Значения по умолчанию берутся из
`ANTI_SNIPE_WINDOW_SEC` / `ANTI_SNIPE_EXTEND_SEC`; администратор меняет их в настройках (`PUT /admin/settings`) без
перезапуска — новые правила действуют со следующей ставки, а клиенты узнают их из `hello` при подключении. Время сверяется по часам БД
внутри транзакции ставки, поэтому ставка не может «проскочить» после конца аукциона.
Настройки сервер сообщает в WebSocket-сообщении `hello` (`antiSnipe`).

## WebSocket (реалтайм)
`GET /api/ws` → апгрейд до WebSocket (RFC 6455), текстовые JSON-сообщения. Access-токен берётся из cookie `session`
при подключении: без неё можно следить за лотами, но не ставить. Браузер откроет сокет сам:
`new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/ws')`.
Запросы с чужого `Origin` отклоняются (403); дополнительные источники — переменная `WS_ALLOWED_ORIGINS`.
Лимиты: сообщение ≤ 4 КБ, ≤ 50 подписок на соединение, ≤ 100 сообщений за 10 с (иначе `rate_limited`,
при многократном превышении соединение закрывается кодом 1008). Пользователь определяется один раз, при
рукопожатии (по access-cookie); дальше соединение не перепроверяется. Сервер шлёт ping каждые 25 с (браузер отвечает
сам); молчащее соединение закрывается через 60 с. Реалтайм работает в одном экземпляре бэкенда.

**Клиент → сервер**
| Сообщение | Ответ |
|---|---|
| `{ "type": "subscribe", "lotId": 12, "reqId": "1" }` | `snapshot` (текущее состояние), затем события по лоту |
| `{ "type": "unsubscribe", "lotId": 12 }` | `unsubscribed` |
| `{ "type": "bid", "lotId": 12, "amount": 1500, "reqId": "2" }` | `bid_ok` или `error` (правила и коды те же, что у REST) |
| `{ "type": "ping" }` | `pong` с серверным временем |

`reqId` — необязательная метка до 64 символов, возвращается в ответе. Ставить можно и через REST — оба пути
равноценны, события рассылаются в любом случае.

**Сервер → клиент** (`now` — серверное время, мс Unix)
| `type` | Поля | Когда |
|---|---|---|
| `hello` | `now, userId\|null, antiSnipe { enabled, windowMs, extendMs }` | сразу после подключения |
| `snapshot` | `lotId, lot, now, reqId?` | ответ на `subscribe` |
| `bid` | `lotId, bid, lot, extended, now` | новая ставка на лот (всем подписчикам, включая ставившего) |
| `bid_ok` | `lotId, bid, lot?, extended, now, reqId?` | подтверждение ставки, сделанной через WS (приходит после `bid`) |
| `lot_finished` | `lotId, lot, now` | аукцион закончился по таймеру сервера |
| `lot_cancelled` | `lotId, lot, now` | автор отменил лот |
| `unsubscribed`, `pong` | `lotId` / `now` | ответы |
| `error` | `code, message, reqId?, lotId?` | отказ; коды REST (`bid_too_low`, `lot_closed`, …) и служебные: `bad_json`, `bad_request`, `validation`, `unauthorized`, `unknown_type`, `not_found`, `too_many_subscriptions`, `rate_limited`, `internal` |

`lot` в событиях — публичное состояние `{ id, status, currentPrice, minBid, bidsCount, leaderId, endsAt }`,
одинаковое для всех. Полей, зависящих от зрителя (`myMaxBid`, `contact`), там нет: получив `lot_finished`,
перезапросите `GET /api/lots/{id}` (там появятся контакты победителя и продавца).

Гарантии: события одного лота приходят в порядке фиксации в БД, `snapshot` не может обогнать или потерять
событие. Статусы `finished` и `cancelled` терминальны (единственное исключение — администратор может аннулировать итог
завершённого аукциона при разборе спора: тогда подписчики получат `lot_cancelled`). Администратор может также
принудительно завершить (`lot_finished`) или отменить (`lot_cancelled`) идущий аукцион. Подписаться на лот,
не прошедший модерацию, нельзя (`not_found`). При обрыве соединения переподключитесь и снова
отправьте `subscribe` — снимок восстановит актуальное состояние (пропущенные события заново не присылаются).

## Админ-панель (`/api/admin/*`, только роль `admin`)

Без сессии — `401`, с ролью `user` — `403 forbidden`. Роль проверяется по БД при каждом запросе, поэтому снятие
роли и блокировка действуют сразу. Изменяющие запросы пишутся в журнал событий. Списки постраничные:
`?page=1&pageSize=20` (до 100) → `{ <ключ>, total, page, pageSize, totalPages }`. Периоды: `from`, `to` —
миллисекунды Unix или дата `ГГГГ-ММ-ДД` (UTC; `to` с датой включает весь день). Везде, где нужна причина
(`reason`), она обязательна, кроме блокировки и принудительного завершения, и не длиннее 1000 символов.

### Администраторы: создание и откат
Не через API, а командой в терминале (в Docker — внутри контейнера `backend`):

| Команда | Что делает |
|---|---|
| `make admin-create` | Спрашивает `username` и пароль (повтор пароля — в интерактивном режиме). Нет такого пользователя — дополнительно спрашивает имя и Telegram (необязательно, Enter — пропустить) и создаёт пользователя с ролью `admin`; есть — меняет роль на `admin`, остальные данные не трогает. Хеш пароля (bcrypt) сохраняется в PostgreSQL в обоих случаях; у существующего пользователя сессии отзываются, блокировка снимается |
| `make admin-delete` | Откат (спрашивает `username` и подтверждение). Аккаунт, который создала `admin-create` и у которого нет лотов и ставок, удаляется; иначе снимается только роль `admin` |

Вход — на общей странице входа по `username`. Правила `username` и Telegram те же, что при регистрации.
Для запуска без Docker:
`make admin-create-local`.

### Пользователи
| Метод | Путь | Описание |
|---|---|---|
| GET | `/admin/users` | Список и поиск. Query: `q` (ID, имя, username или Telegram), `role`, `blocked` (`true`/`false`) → `{ users: [AdminUser], … }` |
| GET | `/admin/users/{id}` | `{ user: AdminUser, stats: UserStats }` — подробная информация |
| GET | `/admin/users/{id}/activity` | История активности (события, где пользователь — субъект или исполнитель). Query: `type`, `from`, `to` → `{ events, … }` |
| GET | `/admin/users/{id}/lots` | Все лоты пользователя любых статусов (`status` — фильтр) → `{ lots, … }` |
| GET | `/admin/users/{id}/bids` | История ставок → `{ bids: [{ id, lotId, lotTitle, lotStatus, amount, createdAt, leading }], … }` |
| POST | `/admin/users/{id}/block` | `{ reason? }` → `AdminUser`; отзывает все сессии. Администратора и себя заблокировать нельзя (409) |
| POST | `/admin/users/{id}/unblock` | → `AdminUser` |

`AdminUser = { id, username, name, telegram, dormId, createdAt, role, blocked, blockedAt, blockedReason?, blockedBy?,
lotsCount, bidsCount, lastActivity }`.

### Модерация лотов
| Метод | Путь | Описание |
|---|---|---|
| GET | `/admin/lots` | Все лоты. Query: `status` (`pending`, `approved`, `rejected`, `active`, `finished`, `cancelled`, `all`), `q`, `category`, `sub`, `authorId`, `sort` (для `pending` по умолчанию `old` — давно ждущие первыми) → `{ lots, … }` |
| GET | `/admin/lots/{id}` | `{ lot, author }`; `lot` с `moderationReason`, `moderatedBy`, `moderatedAt` |
| POST | `/admin/lots/{id}/approve` | `{ startsAt? }` — только `pending`. Без `startsAt` (или в прошлом) аукцион начинается сразу (`active`), иначе лот становится `approved` и стартует сам в назначенное время (до года вперёд). Срок отсчитывается от фактического старта |
| POST | `/admin/lots/{id}/reject` | `{ reason }` — только `pending` → `rejected` |
| PUT | `/admin/lots/{id}` | Редактирование: любые из `title`, `description`, `image`, `category`+`subcategory` (только вместе), `startPrice`, `step`, `hours` → `{ lot, changed }`. Цена, шаг и длительность — только пока аукцион не начался (иначе 409 `locked`). Правила те же, что при создании, включая настройки |
| POST | `/admin/lots/{id}/unpublish` | `{ reason }` — принудительное снятие с публикации: лот скрывается из публичного доступа (`hidden`); идущий или ожидающий старта аукцион отменяется (`cancelled`) |
| DELETE | `/admin/lots/{id}` | `{ reason }` — безвозвратное удаление вместе со ставками; история остаётся в журнале |
| GET | `/admin/lots/{id}/history` | История изменений лота (события без ставок; работает и для удалённого лота) → `{ events, … }` |

### Аукционы
| Метод | Путь | Описание |
|---|---|---|
| GET | `/admin/auctions` | `status`: `active` (по умолчанию), `finished`, `cancelled`, `all`; `q`, `category`, `sub`, `sort` → `{ auctions: [Lot], … }`. Лоты на модерации сюда не входят |
| GET | `/admin/auctions/{id}` | `AuctionDetail = { lot, bids, author, winner, finalPrice, closed }`; `author` и `winner` — `{ id, username, name, telegram }`; `winner`/`finalPrice` = `null`, пока ставок нет |
| GET | `/admin/auctions/{id}/bids` | История ставок → `{ bids, total }` |
| POST | `/admin/auctions/{id}/finish` | `{ reason? }` — принудительное завершение идущего аукциона; победитель — текущий лидер → `AuctionDetail` |
| POST | `/admin/auctions/{id}/cancel` | `{ reason }` — отмена идущего аукциона; ставки остаются в истории → `AuctionDetail` |
| POST | `/admin/auctions/{id}/resolve` | Разрешение спора по **завершённому** аукциону: `{ action, reason, bidId? }`. `dismiss` — результат остаётся; `remove_bid` — удалить спорную ставку (`bidId`), победитель и цена пересчитываются; `annul` — аннулировать результат (аукцион становится `cancelled`) → `AuctionDetail` |

### Журнал событий
`Event = { id, createdAt, type, label, userId, username?, actorId, actorUsername?, byAdmin, lotId, details }`.
`userId` — о ком событие, `actorId` — кто совершил (для действий администратора — он сам).

| Метод | Путь | Описание |
|---|---|---|
| GET | `/admin/events` | Журнал, новые первыми. Фильтры: `userId` (субъект или исполнитель), `lotId`, `type` (точный тип или группа с точкой: `lot.`), `byAdmin=true` (действия администраторов), `from`, `to` → `{ events, … }` |
| GET | `/admin/events/types` | `{ types: [{ type, label }], groups: [{ type, label }] }` для фильтра |

Типы: `user.registered`, `user.login`, `user.blocked`, `user.unblocked`, `lot.created`, `lot.updated`, `lot.approved`,
`lot.rejected`, `lot.unpublished`, `lot.deleted`, `lot.cancelled`, `bid.placed`, `bid.removed`, `auction.started`,
`auction.finished`, `auction.force_finished`, `auction.cancelled`, `auction.annulled`, `auction.dispute_resolved`,
`settings.updated`, `category.*`, `subcategory.*`, `dorm.*`, `admin.cli_*`. Завершение аукциона по сроку
записывается сервером (`auction.finished`, с победителем и ценой), в том числе если аукцион закончился, пока сервер был остановлен.

### Статистика
| Метод | Путь | Описание |
|---|---|---|
| GET | `/admin/stats` | Общая статистика за период (`from`, `to`; без них — за всё время) |
| GET | `/admin/stats/users/{id}` | Статистика пользователя за период |

`OverallStats = { usersTotal, usersRegistered, usersActive, usersBlocked, lotsCreated, lotsPending, auctionsActive,
auctionsFinished, auctionsCancelled, bidsTotal, avgBidsPerAuction, topCategories: [{ id, name, lots, bids }],
topUsers: [{ id, username, name, lots, bids, score }], activeWindowDefault, from, to }`.
`usersActive` — пользователи с собственными действиями в системе за период (если период не задан — за последние 30 суток,
`activeWindowDefault: true`). `avgBidsPerAuction` — ставок на один аукцион, начавшийся в период. Категории — по числу
ставок, затем лотов; активные пользователи — по сумме лотов и ставок. `lotsPending` и `usersBlocked` — текущие значения.
`UserStats = { userId, lotsCreated, bidsPlaced, participated, won, sold, spentTotal, earnedTotal, events, registeredAt, lastActivity, from, to }`.

### Системные настройки
| Метод | Путь | Описание |
|---|---|---|
| GET | `/admin/settings` | `{ settings, defaults }` |
| PUT | `/admin/settings` | Любые из полей `settings`; остальные не меняются → `{ settings, defaults }` |

`settings = { minBidStep, minDurationMinutes, maxDurationMinutes, defaultDurationMinutes, antiSnipeWindowSec, antiSnipeExtendSec }`:
минимальный шаг ставки (₽); минимальная и максимальная длительность аукциона; время автоматического завершения по
умолчанию (длительность, если автор не указал `hours`); правила продления при ставке в последние минуты (окно и
продление, 0 в окне — выключено). Должно выполняться `min ≤ default ≤ max`. Настройки действуют на новые лоты и
следующие ставки; уже созданные лоты не пересчитываются. Пока администратор ничего не менял, действуют значения
по умолчанию (продление — из `ANTI_SNIPE_*`).

### Справочники: категории, подкатегории, общежития
Заполняет администратор: в коде и в миграциях ничего не зашито, после установки справочники пусты (пока нет общежитий, регистрация невозможна; пока нет категорий — нельзя создать лот). Новые `id` строятся из названия (`Книги и учебники` →
`knigi-i-uchebniki`, при совпадении добавляется `-2`, `-3`…); можно передать свой `id` (латиница, цифры, дефис) — занятый даёт 409.

| Метод | Путь | Тело |
|---|---|---|
| POST | `/admin/categories` | `{ name, description?, id? }` → 201 `{ id, name, description, subs: [] }` |
| PUT | `/admin/categories/{id}` | `{ name, description? }` |
| DELETE | `/admin/categories/{id}` | Вместе с подкатегориями. `409 in_use`, если в категории есть лоты |
| POST | `/admin/categories/{id}/subcategories` | `{ name, id? }` → 201 `{ id, name }` (`id` подкатегории уникален во всём справочнике) |
| PUT | `/admin/categories/{id}/subcategories/{sub}` | `{ name }` |
| DELETE | `/admin/categories/{id}/subcategories/{sub}` | `409 in_use`, если есть лоты |
| POST | `/admin/dorms` | `{ name }` → 201 `{ id, name }` (номер выдаётся автоматически; название уникально без учёта регистра) |
| PUT | `/admin/dorms/{id}` | `{ name }` |
| DELETE | `/admin/dorms/{id}` | `409 in_use`, если в общежитии есть пользователи |

Публичные `GET /categories` и `GET /dorms` отдают актуальное содержимое.

## Что менять во фронтенде
`store.js` сейчас синхронный и содержит бизнес-логику. При интеграции: цены, лидера, число ставок, статус,
фильтры, сортировку и пагинацию брать с сервера (поля `Lot`), а не считать по моковым массивам;
`placeBid`/`cancelLot`/`register`/`login` превратить в вызовы `src/api.js` с показом `error`;
сессию определять через `GET /auth/me` при старте (не хранить `currentUserId` и токены в localStorage); при 401 один раз вызвать `POST /auth/refresh` и повторить запрос (см. AUTH.md);
в форме создания лота загружать файл через `uploadImage`, а не `URL.createObjectURL`.