package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"auction/auth"
	"auction/db"
)

// Гонка на уровне БД: много горутин одновременно обновляют один и тот же токен.
// Без блокировки строки (SELECT … FOR UPDATE) несколько обновлений успевают
// прочитать «токен не использован» и выпустить по потомку — раундов много, чтобы
// такое окно обязательно поймалось.
func TestRotateRaceIsAtomic(t *testing.T) {
	for _, gap := range []time.Duration{0, time.Hour} {
		e := newEnv(t, strict)
		u := e.newDevice().register()
		ctx := context.Background()

		for round := 0; round < 40; round++ {
			tok, hash, _ := auth.NewRefreshToken()
			if _, err := e.q.CreateSession(ctx, u.ID, hash, time.Hour, 24*time.Hour); err != nil {
				t.Fatal(err)
			}
			_ = tok

			const workers = 16
			var (
				wg                        sync.WaitGroup
				mu                        sync.Mutex
				ok, reused, race, invalid int
			)
			start := make(chan struct{})
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, newHash, _ := auth.NewRefreshToken()
					<-start
					_, err := e.q.RotateRefreshToken(ctx, hash, newHash, time.Hour, gap)
					mu.Lock()
					defer mu.Unlock()
					switch {
					case err == nil:
						ok++
					case errors.Is(err, db.ErrRefreshReused):
						reused++
					case errors.Is(err, db.ErrRefreshRace):
						race++
					case errors.Is(err, db.ErrRefreshInvalid):
						// проигравший ждал блокировку, а победитель к этому моменту уже
						// обнаружил повтор и отозвал семейство
						invalid++
					default:
						t.Errorf("неожиданная ошибка: %v", err)
					}
				}()
			}
			close(start)
			wg.Wait()

			if ok != 1 {
				t.Fatalf("gap=%v раунд %d: успешных ротаций %d (reused=%d race=%d), ожидали ровно 1", gap, round, ok, reused, race)
			}
			if gap == 0 && race != 0 {
				t.Fatalf("при gap=0 не бывает ErrRefreshRace")
			}
			if gap > 0 && (reused != 0 || invalid != 0) {
				t.Fatalf("в пределах окна семейство отзываться не должно (reused=%d invalid=%d)", reused, invalid)
			}
			var children int
			if err := e.sql.QueryRow(`SELECT count(*) FROM refresh_tokens c JOIN refresh_tokens p ON p.replaced_by = c.id
				WHERE encode(p.token_hash,'hex') = $1`, hashHex2(hash)).Scan(&children); err != nil {
				t.Fatal(err)
			}
			if children != 1 {
				t.Fatalf("gap=%v раунд %d: у токена %d потомков, ожидали 1", gap, round, children)
			}
			var family int
			e.sql.QueryRow(`SELECT count(*) FROM refresh_tokens WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE encode(token_hash,'hex') = $1)`, hashHex2(hash)).Scan(&family)
			if family != 2 && gap > 0 {
				t.Fatalf("gap=%v раунд %d: в семействе %d токенов, ожидали 2", gap, round, family)
			}
		}
	}
}
