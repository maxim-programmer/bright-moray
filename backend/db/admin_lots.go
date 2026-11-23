package db

import (
	"auction/model"
	"context"
	"database/sql"
	"strings"
	"time"
)

// durationExpr совпадает с выражением в lotSelect: заявленная длительность в секундах.
const durationExpr = `COALESCE(duration_seconds, GREATEST(1, EXTRACT(EPOCH FROM (ends_at - created_at))::bigint))`

// AdminGetLot возвращает лот целиком (с данными модерации), независимо от его видимости.
func (q *Queries) AdminGetLot(ctx context.Context, id int64) (model.Lot, error) {
	return q.getLotRaw(ctx, 0, id)
}

// AdminListLots возвращает лоты любых статусов по фильтру и их общее число.
func (q *Queries) AdminListLots(ctx context.Context, f model.LotFilter, limit, offset int) ([]model.Lot, int, error) {
	f.All = true
	total, err := q.CountLots(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	lots, err := q.listLotsRaw(ctx, 0, f, limit, offset)
	return lots, total, err
}

// lockedLot — строка лота, прочитанная под блокировкой внутри транзакции.
type lockedLot struct {
	Author   int64
	Status   string
	Hidden   bool
	Ended    bool // срок вышел (по часам БД)
	Duration int64
	Now      time.Time
	Title    string
}

func lockLot(ctx context.Context, tx *sql.Tx, lotID int64) (lockedLot, error) {
	var l lockedLot
	err := tx.QueryRowContext(ctx, `
		SELECT author_id, status, hidden, ends_at <= clock_timestamp(), `+durationExpr+`, clock_timestamp(), title
		FROM lots WHERE id = $1 FOR UPDATE`, lotID).
		Scan(&l.Author, &l.Status, &l.Hidden, &l.Ended, &l.Duration, &l.Now, &l.Title)
	return l, mapErr(err)
}

// finishedNow: торги закончились (статус или срок).
func (l lockedLot) finishedNow() bool {
	return l.Status == model.StatusFinished || (l.Status == model.StatusActive && l.Ended)
}

// leaderOf возвращает лидирующую ставку лота (nil — ставок нет).
func leaderOf(ctx context.Context, tx *sql.Tx, lotID int64) (userID int64, amount int, ok bool, err error) {
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, amount FROM bids WHERE lot_id = $1 ORDER BY amount DESC, id ASC LIMIT 1`, lotID).
		Scan(&userID, &amount)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	return userID, amount, err == nil, err
}

func reasonArg(reason string) sql.NullString {
	return sql.NullString{String: reason, Valid: reason != ""}
}

/* ---------- модерация ---------- */

// ApproveResult — итог одобрения лота.
type ApproveResult struct {
	Started bool          // аукцион начался сразу
	EndsAt  time.Time     // конец аукциона (расчётный, если он ещё не начался)
	Left    time.Duration // до конца аукциона по часам БД (только для Started)
}

// ApproveLot одобряет лот со статусом pending. Если startAt нулевое или уже наступило, аукцион
// начинается сразу (active), иначе лот получает статус approved и стартует в момент startAt.
func (q *Queries) ApproveLot(ctx context.Context, adminID, lotID int64, startAt time.Time) (ApproveResult, error) {
	var res ApproveResult
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return res, err
	}
	if l.Status != model.StatusPending {
		return res, model.Errf(409, "invalid_state", "Лот уже обработан")
	}
	res.Started = startAt.IsZero() || !startAt.After(l.Now)
	start := startAt
	if res.Started {
		start = l.Now
	}
	status := model.StatusApproved
	if res.Started {
		status = model.StatusActive
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE lots SET status = $2, starts_at = $3::timestamptz, ends_at = $3::timestamptz + make_interval(secs => $4::float8),
		       moderation_reason = NULL, moderated_by = $5, moderated_at = $6
		WHERE id = $1 RETURNING ends_at`,
		lotID, status, start, l.Duration, adminID, l.Now).Scan(&res.EndsAt); err != nil {
		return res, mapErr(err)
	}
	res.Left = res.EndsAt.Sub(l.Now)

	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvLotApproved, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true,
		Details: map[string]any{"title": l.Title, "startsAt": ms(start), "immediate": res.Started},
	}); err != nil {
		return res, err
	}
	if res.Started {
		if err := insertEvent(ctx, tx, EventInput{
			Type: model.EvAuctionStarted, UserID: l.Author, LotID: lotID,
			Details: map[string]any{"title": l.Title, "endsAt": ms(res.EndsAt)},
		}); err != nil {
			return res, err
		}
	}
	return res, tx.Commit()
}

// RejectLot отклоняет лот со статусом pending с указанием причины.
func (q *Queries) RejectLot(ctx context.Context, adminID, lotID int64, reason string) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return err
	}
	if l.Status != model.StatusPending {
		return model.Errf(409, "invalid_state", "Лот уже обработан")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lots SET status = 'rejected', moderation_reason = $2, moderated_by = $3, moderated_at = clock_timestamp()
		WHERE id = $1`, lotID, reasonArg(reason), adminID); err != nil {
		return mapErr(err)
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvLotRejected, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true,
		Details: map[string]any{"title": l.Title, "reason": reason},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// UnpublishResult — итог снятия лота с публикации.
type UnpublishResult struct {
	WasActive bool // шли торги: они отменены, подписчикам нужно сообщить
}

// UnpublishLot снимает опубликованный лот с публикации: он исчезает из публичного каталога
// (его видят только автор, участники торгов и администраторы). Идущий или ожидающий старта
// аукцион при этом отменяется; завершённый и отменённый лоты сохраняют статус.
func (q *Queries) UnpublishLot(ctx context.Context, adminID, lotID int64, reason string) (UnpublishResult, error) {
	var res UnpublishResult
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return res, err
	}
	switch {
	case l.Hidden:
		return res, model.Errf(409, "invalid_state", "Лот уже снят с публикации")
	case l.Status == model.StatusPending || l.Status == model.StatusRejected:
		return res, model.Errf(409, "invalid_state", "Лот ещё не опубликован: его можно отклонить или удалить")
	}
	cancel := l.Status == model.StatusApproved || (l.Status == model.StatusActive && !l.Ended)
	res.WasActive = l.Status == model.StatusActive && !l.Ended
	if cancel {
		_, err = tx.ExecContext(ctx, `
			UPDATE lots SET status = 'cancelled', cancelled_at = $4, hidden = true,
			       moderation_reason = $2, moderated_by = $3, moderated_at = $4
			WHERE id = $1`, lotID, reasonArg(reason), adminID, l.Now)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE lots SET hidden = true, moderation_reason = $2, moderated_by = $3, moderated_at = $4
			WHERE id = $1`, lotID, reasonArg(reason), adminID, l.Now)
	}
	if err != nil {
		return res, mapErr(err)
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvLotUnpublished, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true,
		Details: map[string]any{"title": l.Title, "reason": reason, "previousStatus": l.Status},
	}); err != nil {
		return res, err
	}
	return res, tx.Commit()
}

// DeleteResult — итог удаления лота.
type DeleteResult struct {
	Image string // файл картинки остаётся на диске: им может пользоваться только этот лот
}

// DeleteLot безвозвратно удаляет лот (и его ставки) при нарушении правил.
// История лота в журнале событий сохраняется.
func (q *Queries) DeleteLot(ctx context.Context, adminID, lotID int64, reason string) (DeleteResult, error) {
	var res DeleteResult
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return res, err
	}
	var bids int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bids WHERE lot_id = $1`, lotID).Scan(&bids); err != nil {
		return res, err
	}
	if err := tx.QueryRowContext(ctx, `DELETE FROM lots WHERE id = $1 RETURNING image`, lotID).Scan(&res.Image); err != nil {
		return res, mapErr(err)
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvLotDeleted, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true,
		Details: map[string]any{"title": l.Title, "reason": reason, "status": l.Status, "bids": bids},
	}); err != nil {
		return res, err
	}
	return res, tx.Commit()
}

// LotEdit — изменения лота администратором; nil — поле не меняется.
type LotEdit struct {
	Title, Description, Image, Category, Subcategory *string
	StartPrice, Step                                 *int
	DurationSec                                      *int64
}

// EditLot применяет изменения и записывает в журнал, что именно изменилось (старое и новое значение).
// Цену, шаг и длительность можно менять только до начала аукциона: у идущего аукциона они
// уже участвуют в расчёте ставок. Возвращает обновлённый лот и признак «что-то изменилось».
func (q *Queries) EditLot(ctx context.Context, adminID, lotID int64, e LotEdit) (model.Lot, bool, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Lot{}, false, err
	}
	defer tx.Rollback()

	var cur struct {
		title, desc, image, cat, sub string
		price, step                  int
		dur                          int64
		author                       int64
		status                       string
	}
	err = tx.QueryRowContext(ctx, `
		SELECT title, description, image, category_id, subcategory_id, start_price, step, `+durationExpr+`, author_id, status
		FROM lots WHERE id = $1 FOR UPDATE`, lotID).
		Scan(&cur.title, &cur.desc, &cur.image, &cur.cat, &cur.sub, &cur.price, &cur.step, &cur.dur, &cur.author, &cur.status)
	if err != nil {
		return model.Lot{}, false, mapErr(err)
	}
	notStarted := cur.status == model.StatusPending || cur.status == model.StatusApproved || cur.status == model.StatusRejected
	if (e.StartPrice != nil || e.Step != nil || e.DurationSec != nil) && !notStarted {
		return model.Lot{}, false, model.Errf(409, "locked", "Цену, шаг и длительность нельзя менять после начала аукциона")
	}

	changes := map[string]any{}
	var sets []string
	var args []any
	set := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args)+1))
	}
	if e.Title != nil && *e.Title != cur.title {
		changes["title"] = map[string]any{"from": cur.title, "to": *e.Title}
		set("title", *e.Title)
	}
	if e.Description != nil && *e.Description != cur.desc {
		changes["description"] = map[string]any{"from": cur.desc, "to": *e.Description}
		set("description", *e.Description)
	}
	if e.Image != nil && *e.Image != cur.image {
		changes["image"] = map[string]any{"from": cur.image, "to": *e.Image}
		set("image", *e.Image)
	}
	newCat, newSub := cur.cat, cur.sub
	if e.Category != nil {
		newCat = *e.Category
	}
	if e.Subcategory != nil {
		newSub = *e.Subcategory
	}
	if newCat != cur.cat || newSub != cur.sub {
		if newCat != cur.cat {
			changes["category"] = map[string]any{"from": cur.cat, "to": newCat}
			set("category_id", newCat)
		}
		if newSub != cur.sub {
			changes["subcategory"] = map[string]any{"from": cur.sub, "to": newSub}
			set("subcategory_id", newSub)
		}
	}
	if e.StartPrice != nil && *e.StartPrice != cur.price {
		changes["startPrice"] = map[string]any{"from": cur.price, "to": *e.StartPrice}
		set("start_price", *e.StartPrice)
	}
	if e.Step != nil && *e.Step != cur.step {
		changes["step"] = map[string]any{"from": cur.step, "to": *e.Step}
		set("step", *e.Step)
	}
	if e.DurationSec != nil && *e.DurationSec != cur.dur {
		changes["durationSec"] = map[string]any{"from": cur.dur, "to": *e.DurationSec}
		set("duration_seconds", *e.DurationSec)
		// расчётный конец пересчитывается от начала (или от создания, пока старт не назначен)
		args = append(args, *e.DurationSec)
		sets = append(sets, "ends_at = COALESCE(starts_at, created_at) + make_interval(secs => $"+itoa(len(args)+1)+"::float8)")
	}
	if len(sets) == 0 {
		lot, err := q.getLotRaw(ctx, 0, lotID)
		return lot, false, err
	}
	all := append([]any{lotID}, args...)
	if _, err := tx.ExecContext(ctx, `UPDATE lots SET `+strings.Join(sets, ", ")+` WHERE id = $1`, all...); err != nil {
		return model.Lot{}, false, mapErr(err)
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvLotUpdated, UserID: cur.author, ActorID: adminID, LotID: lotID, ByAdmin: true,
		Details: map[string]any{"changes": changes},
	}); err != nil {
		return model.Lot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return model.Lot{}, false, err
	}
	lot, err := q.getLotRaw(ctx, 0, lotID)
	return lot, true, err
}

/* ---------- аукционы ---------- */

// FinishResult — итог принудительного завершения.
type FinishResult struct {
	WinnerID   int64 // 0 — ставок не было
	FinalPrice int
}

// ForceFinish досрочно завершает идущий аукцион: победителем становится текущий лидер.
func (q *Queries) ForceFinish(ctx context.Context, adminID, lotID int64, reason string) (FinishResult, error) {
	var res FinishResult
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return res, err
	}
	if l.Status != model.StatusActive || l.Ended {
		return res, model.Errf(409, "invalid_state", "Завершить можно только идущий аукцион")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lots SET status = 'finished', ends_at = clock_timestamp(),
		       moderation_reason = $2, moderated_by = $3, moderated_at = clock_timestamp()
		WHERE id = $1`, lotID, reasonArg(reason), adminID); err != nil {
		return res, mapErr(err)
	}
	winner, price, ok, err := leaderOf(ctx, tx, lotID)
	if err != nil {
		return res, err
	}
	d := map[string]any{"title": l.Title, "reason": reason}
	if ok {
		res.WinnerID, res.FinalPrice = winner, price
		d["winnerId"], d["finalPrice"] = winner, price
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvAuctionForced, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true, Details: d,
	}); err != nil {
		return res, err
	}
	return res, tx.Commit()
}

// CancelAuction отменяет идущий аукцион с указанием причины; ставки сохраняются в истории.
func (q *Queries) CancelAuction(ctx context.Context, adminID, lotID int64, reason string) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return err
	}
	if l.Status != model.StatusActive || l.Ended {
		return model.Errf(409, "invalid_state", "Отменить можно только идущий аукцион")
	}
	var bids int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bids WHERE lot_id = $1`, lotID).Scan(&bids); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lots SET status = 'cancelled', cancelled_at = clock_timestamp(),
		       moderation_reason = $2, moderated_by = $3, moderated_at = clock_timestamp()
		WHERE id = $1`, lotID, reasonArg(reason), adminID); err != nil {
		return mapErr(err)
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvAuctionCancelled, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true,
		Details: map[string]any{"title": l.Title, "reason": reason, "bids": bids},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// Действия при разрешении спора по завершённому аукциону.
const (
	DisputeDismiss   = "dismiss"    // спор рассмотрен, результат остаётся
	DisputeRemoveBid = "remove_bid" // удалить спорную ставку; победитель и цена пересчитываются
	DisputeAnnul     = "annul"      // аннулировать результат: аукцион отменяется
)

// DisputeResult — итог разрешения спора.
type DisputeResult struct {
	Annulled bool // аукцион отменён: подписчикам нужно сообщить
}

// ResolveDispute разрешает спорную ситуацию по завершённому аукциону. Каждое действие
// обязательно с причиной и записывается в журнал.
func (q *Queries) ResolveDispute(ctx context.Context, adminID, lotID int64, action string, bidID int64, reason string) (DisputeResult, error) {
	var res DisputeResult
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return res, err
	}
	if !l.finishedNow() {
		return res, model.Errf(409, "invalid_state", "Спор можно разрешить только по завершённому аукциону")
	}
	winner, price, hasWinner, err := leaderOf(ctx, tx, lotID)
	if err != nil {
		return res, err
	}
	base := map[string]any{"title": l.Title, "reason": reason}
	if hasWinner {
		base["winnerId"], base["finalPrice"] = winner, price
	}

	switch action {
	case DisputeDismiss:
		err = insertEvent(ctx, tx, EventInput{
			Type: model.EvDisputeResolved, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true, Details: base,
		})
	case DisputeRemoveBid:
		var bidUser int64
		var amount int
		err = tx.QueryRowContext(ctx, `DELETE FROM bids WHERE id = $1 AND lot_id = $2 RETURNING user_id, amount`, bidID, lotID).
			Scan(&bidUser, &amount)
		if err == sql.ErrNoRows {
			return res, model.Errf(404, "bid_not_found", "Ставка не найдена в этом аукционе")
		}
		if err != nil {
			return res, mapErr(err)
		}
		d := map[string]any{"title": l.Title, "reason": reason, "bidId": bidID, "amount": amount}
		if nw, np, ok, e2 := leaderOf(ctx, tx, lotID); e2 != nil {
			return res, e2
		} else if ok {
			d["newWinnerId"], d["newFinalPrice"] = nw, np
		}
		err = insertEvent(ctx, tx, EventInput{
			Type: model.EvBidRemoved, UserID: bidUser, ActorID: adminID, LotID: lotID, ByAdmin: true, Details: d,
		})
	case DisputeAnnul:
		if _, err = tx.ExecContext(ctx, `
			UPDATE lots SET status = 'cancelled', cancelled_at = clock_timestamp(),
			       moderation_reason = $2, moderated_by = $3, moderated_at = clock_timestamp()
			WHERE id = $1`, lotID, reasonArg(reason), adminID); err != nil {
			return res, mapErr(err)
		}
		res.Annulled = true
		err = insertEvent(ctx, tx, EventInput{
			Type: model.EvAuctionAnnulled, UserID: l.Author, ActorID: adminID, LotID: lotID, ByAdmin: true, Details: base,
		})
	default:
		return res, model.Errf(400, "validation", "action: dismiss, remove_bid или annul")
	}
	if err != nil {
		return res, err
	}
	return res, tx.Commit()
}

// GetAuctionDetail собирает сведения об аукционе для администратора: лот, ставки,
// автора, победителя и итоговую цену.
func (q *Queries) GetAuctionDetail(ctx context.Context, lotID int64) (model.AuctionDetail, error) {
	var d model.AuctionDetail
	lot, err := q.AdminGetLot(ctx, lotID)
	if err != nil {
		return d, err
	}
	d.Lot = lot
	if d.Bids, err = q.GetLotBids(ctx, lotID); err != nil {
		return d, err
	}
	d.Closed = lot.Status == model.StatusFinished || lot.Status == model.StatusCancelled ||
		(lot.Status == model.StatusActive && lot.EndsAt <= time.Now().UnixMilli())
	if d.Author, err = q.publicRef(ctx, lot.AuthorID); err != nil {
		return d, err
	}
	if lot.LeaderID != nil && lot.BidsCount > 0 {
		if d.Winner, err = q.publicRef(ctx, *lot.LeaderID); err != nil {
			return d, err
		}
		price := lot.CurrentPrice
		d.FinalPrice = &price
	}
	return d, nil
}

func (q *Queries) publicRef(ctx context.Context, id int64) (*model.PublicRef, error) {
	var r model.PublicRef
	err := q.db.QueryRowContext(ctx, `SELECT id, username, name, COALESCE(telegram, '') FROM users WHERE id = $1`, id).
		Scan(&r.ID, &r.Username, &r.Name, &r.Telegram)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

/* ---------- старт и завершение по расписанию ---------- */

// Activated — лот, у которого только что начался аукцион.
type Activated struct {
	ID     int64
	EndsAt time.Time
}

// ActivateDueLots запускает аукционы одобренных лотов, у которых наступило время старта.
// Конец аукциона отсчитывается от фактического момента старта.
func (q *Queries) ActivateDueLots(ctx context.Context) ([]Activated, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		UPDATE lots SET status = 'active', starts_at = clock_timestamp(),
		       ends_at = clock_timestamp() + make_interval(secs => `+durationExpr+`::float8)
		WHERE status = 'approved' AND starts_at <= clock_timestamp()
		RETURNING id, ends_at, author_id, title`)
	if err != nil {
		return nil, mapErr(err)
	}
	var out []Activated
	type meta struct {
		author int64
		title  string
	}
	metas := map[int64]meta{}
	for rows.Next() {
		var a Activated
		var m meta
		if err := rows.Scan(&a.ID, &a.EndsAt, &m.author, &m.title); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
		metas[a.ID] = m
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, a := range out {
		m := metas[a.ID]
		if err := insertEvent(ctx, tx, EventInput{
			Type: model.EvAuctionStarted, UserID: m.author, LotID: a.ID,
			Details: map[string]any{"title": m.title, "endsAt": ms(a.EndsAt), "scheduled": true},
		}); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

// MarkFinished фиксирует завершение аукциона, у которого вышел срок: статус становится
// finished, в журнал пишется победитель и итоговая цена. Повторный вызов ничего не меняет.
func (q *Queries) MarkFinished(ctx context.Context, lotID int64) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var author int64
	var title string
	err = tx.QueryRowContext(ctx, `
		UPDATE lots SET status = 'finished'
		WHERE id = $1 AND status = 'active' AND ends_at <= clock_timestamp()
		RETURNING author_id, title`, lotID).Scan(&author, &title)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return mapErr(err)
	}
	d := map[string]any{"title": title}
	if w, p, ok, err := leaderOf(ctx, tx, lotID); err != nil {
		return err
	} else if ok {
		d["winnerId"], d["finalPrice"] = w, p
	}
	var bids int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bids WHERE lot_id = $1`, lotID).Scan(&bids); err != nil {
		return err
	}
	d["bids"] = bids
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvAuctionFinished, UserID: author, LotID: lotID, Details: d,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// FinalizeExpired доводит до статуса finished все аукционы с вышедшим сроком (например,
// закончившиеся, пока сервер был остановлен) и возвращает их id.
func (q *Queries) FinalizeExpired(ctx context.Context) ([]int64, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id FROM lots WHERE status = 'active' AND ends_at <= clock_timestamp() ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var done []int64
	for _, id := range ids {
		if err := q.MarkFinished(ctx, id); err != nil {
			return done, err
		}
		done = append(done, id)
	}
	return done, nil
}
