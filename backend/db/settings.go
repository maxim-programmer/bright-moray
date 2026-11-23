package db

import (
	"auction/model"
	"context"
)

// Ключи таблицы settings.
const (
	keyMinBidStep         = "min_bid_step"
	keyMinDurationMin     = "min_duration_minutes"
	keyMaxDurationMin     = "max_duration_minutes"
	keyDefaultDurationMin = "default_duration_minutes"
	keyAntiSnipeWindow    = "anti_snipe_window_sec"
	keyAntiSnipeExtend    = "anti_snipe_extend_sec"
)

// GetSettings возвращает действующие настройки: значения администратора поверх defaults.
func (q *Queries) GetSettings(ctx context.Context, defaults model.Settings) (model.Settings, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return defaults, err
	}
	defer rows.Close()
	s := defaults
	for rows.Next() {
		var key string
		var v int64
		if err := rows.Scan(&key, &v); err != nil {
			return defaults, err
		}
		switch key {
		case keyMinBidStep:
			s.MinBidStep = int(v)
		case keyMinDurationMin:
			s.MinDurationMinutes = int(v)
		case keyMaxDurationMin:
			s.MaxDurationMinutes = int(v)
		case keyDefaultDurationMin:
			s.DefaultDurationMinutes = int(v)
		case keyAntiSnipeWindow:
			s.AntiSnipeWindowSec = int(v)
		case keyAntiSnipeExtend:
			s.AntiSnipeExtendSec = int(v)
		}
	}
	return s, rows.Err()
}

// SaveSettings записывает все настройки и событие журнала в одной транзакции.
func (q *Queries) SaveSettings(ctx context.Context, adminID int64, s model.Settings, details map[string]any) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, v := range map[string]int{
		keyMinBidStep:         s.MinBidStep,
		keyMinDurationMin:     s.MinDurationMinutes,
		keyMaxDurationMin:     s.MaxDurationMinutes,
		keyDefaultDurationMin: s.DefaultDurationMinutes,
		keyAntiSnipeWindow:    s.AntiSnipeWindowSec,
		keyAntiSnipeExtend:    s.AntiSnipeExtendSec,
	} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value, updated_by) VALUES ($1, $2, $3)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`,
			key, int64(v), adminID); err != nil {
			return mapErr(err)
		}
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvSettingsUpdated, UserID: adminID, ActorID: adminID, ByAdmin: true, Details: details,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
