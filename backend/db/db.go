package db

import (
	"bright-moray/model"
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Queries struct {
	db *sql.DB
}

func New(db *sql.DB) *Queries {
	return &Queries{db: db}
}

func (q *Queries) CreateUser(username, password, name, telegram string, dormID int16) (model.User, error) {
	var user model.User
	row := q.db.QueryRowContext(context.Background(),
		"INSERT INTO users (username, password, name, telegram, dorm_id) VALUES ($1, $2, $3, $4, $5) RETURNING id, username, name, telegram, dorm_id, created_at",
		username, password, name, telegram, dormID)
	err := row.Scan(&user.ID, &user.Username, &user.Name, &user.Telegram, &user.DormID, &user.CreatedAt)
	return user, err
}

func (q *Queries) LoginByUsername(username string) (model.User, error) {
	var user model.User
	row := q.db.QueryRowContext(context.Background(),
		"SELECT id, username, password, name, telegram, dorm_id, created_at FROM users WHERE username = $1", username)
	err := row.Scan(&user.ID, &user.Username, &user.Password, &user.Name, &user.Telegram, &user.DormID, &user.CreatedAt)
	return user, err
}

func (q *Queries) GetUserByID(id int64) (model.User, error) {
	var user model.User
	row := q.db.QueryRowContext(context.Background(),
		"SELECT id, username, name, telegram, dorm_id, created_at FROM users WHERE id = $1", id)
	err := row.Scan(&user.ID, &user.Username, &user.Name, &user.Telegram, &user.DormID, &user.CreatedAt)
	return user, err
}

func (q *Queries) UpdateProfile(id int64, name, telegram string) (model.User, error) {
	var user model.User
	row := q.db.QueryRowContext(context.Background(),
		"UPDATE users SET name = $1, telegram = $2 WHERE id = $3 RETURNING id, username, name, telegram, dorm_id, created_at",
		name, telegram, id)
	err := row.Scan(&user.ID, &user.Username, &user.Name, &user.Telegram, &user.DormID, &user.CreatedAt)
	return user, err
}

func (q *Queries) ChangePassword(id int64, password string) error {
	_, err := q.db.ExecContext(context.Background(),
		"UPDATE users SET password = $1 WHERE id = $2", password, id)
	return err
}

func (q *Queries) ChangeUsername(id int64, newUsername string) (model.User, error) {
	var user model.User
	row := q.db.QueryRowContext(context.Background(),
		"UPDATE users SET username = $1 WHERE id = $2 RETURNING id, username, name, telegram, dorm_id, created_at",
		newUsername, id)
	err := row.Scan(&user.ID, &user.Username, &user.Name, &user.Telegram, &user.DormID, &user.CreatedAt)
	return user, err
}

func (q *Queries) CreateLot(l model.CreateLotRequest, authorID int64) (model.Lot, error) {
	endsAt := time.Now().Add(time.Duration(l.Hours * float64(time.Hour)))
	var lot model.Lot
	row := q.db.QueryRowContext(context.Background(),
		"INSERT INTO lots (title, description, image, category_id, subcategory_id, author_id, start_price, step, ends_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id, title, description, image, category_id, subcategory_id, author_id, start_price, step, created_at, ends_at, status",
		l.Title, l.Description, l.Image, l.Category, l.Subcategory, authorID, l.StartPrice, l.Step, endsAt)
	err := row.Scan(&lot.ID, &lot.Title, &lot.Description, &lot.Image, &lot.CategoryID, &lot.SubcategoryID, &lot.AuthorID, &lot.StartPrice, &lot.Step, &lot.CreatedAt, &lot.EndsAt, &lot.Status)
	return lot, err
}

func (q *Queries) GetLotByID(id int64) (model.Lot, error) {
	var lot model.Lot
	row := q.db.QueryRowContext(context.Background(),
		"SELECT id, title, description, image, category_id, subcategory_id, author_id, start_price, step, created_at, ends_at, status FROM lots WHERE id = $1", id)
	err := row.Scan(&lot.ID, &lot.Title, &lot.Description, &lot.Image, &lot.CategoryID, &lot.SubcategoryID, &lot.AuthorID, &lot.StartPrice, &lot.Step, &lot.CreatedAt, &lot.EndsAt, &lot.Status)
	return lot, err
}

func (q *Queries) ListLots(status string, limit, offset int) ([]model.Lot, error) {
	rows, err := q.db.QueryContext(context.Background(),
		"SELECT id, title, description, image, category_id, subcategory_id, author_id, start_price, step, created_at, ends_at, status FROM lots WHERE status = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
		status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lots []model.Lot
	for rows.Next() {
		var lot model.Lot
		if err := rows.Scan(&lot.ID, &lot.Title, &lot.Description, &lot.Image, &lot.CategoryID, &lot.SubcategoryID, &lot.AuthorID, &lot.StartPrice, &lot.Step, &lot.CreatedAt, &lot.EndsAt, &lot.Status); err != nil {
			return nil, err
		}
		lots = append(lots, lot)
	}
	return lots, rows.Err()
}

func (q *Queries) CancelLot(lotID, authorID int64) error {
	res, err := q.db.ExecContext(context.Background(),
		"UPDATE lots SET status = 'cancelled' WHERE id = $1 AND author_id = $2 AND status = 'active'", lotID, authorID)
	if err != nil {
		return err
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		return fmt.Errorf("lot not found or not cancellable")
	}
	return nil
}

func (q *Queries) GetMyLots(authorID int64) ([]model.Lot, error) {
	rows, err := q.db.QueryContext(context.Background(),
		"SELECT id, title, description, image, category_id, subcategory_id, author_id, start_price, step, created_at, ends_at, status FROM lots WHERE author_id = $1 ORDER BY created_at DESC", authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lots []model.Lot
	for rows.Next() {
		var lot model.Lot
		if err := rows.Scan(&lot.ID, &lot.Title, &lot.Description, &lot.Image, &lot.CategoryID, &lot.SubcategoryID, &lot.AuthorID, &lot.StartPrice, &lot.Step, &lot.CreatedAt, &lot.EndsAt, &lot.Status); err != nil {
			return nil, err
		}
		lots = append(lots, lot)
	}
	return lots, rows.Err()
}

func (q *Queries) GetMyBidLots(userID int64) ([]model.Lot, error) {
	rows, err := q.db.QueryContext(context.Background(),
		"SELECT id, title, description, image, category_id, subcategory_id, author_id, start_price, step, created_at, ends_at, status FROM lots WHERE id IN (SELECT lot_id FROM bids WHERE user_id = $1) ORDER BY created_at DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lots []model.Lot
	for rows.Next() {
		var lot model.Lot
		if err := rows.Scan(&lot.ID, &lot.Title, &lot.Description, &lot.Image, &lot.CategoryID, &lot.SubcategoryID, &lot.AuthorID, &lot.StartPrice, &lot.Step, &lot.CreatedAt, &lot.EndsAt, &lot.Status); err != nil {
			return nil, err
		}
		lots = append(lots, lot)
	}
	return lots, rows.Err()
}

func (q *Queries) GetLotBids(lotID int64) ([]model.Bid, error) {
	rows, err := q.db.QueryContext(context.Background(),
		"SELECT id, lot_id, user_id, amount, created_at FROM bids WHERE lot_id = $1 ORDER BY amount DESC", lotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bids []model.Bid
	for rows.Next() {
		var bid model.Bid
		if err := rows.Scan(&bid.ID, &bid.LotID, &bid.UserID, &bid.Amount, &bid.CreatedAt); err != nil {
			return nil, err
		}
		bids = append(bids, bid)
	}
	return bids, rows.Err()
}

func (q *Queries) GetLotWinner(lotID int64) (int64, error) {
	var userID int64
	row := q.db.QueryRowContext(context.Background(),
		"SELECT user_id FROM bids WHERE lot_id = $1 ORDER BY amount DESC LIMIT 1", lotID)
	err := row.Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return userID, err
}

func (q *Queries) PlaceBid(lotID, userID int64, amount int) (model.Bid, error) {
	var bid model.Bid
	row := q.db.QueryRowContext(context.Background(),
		"INSERT INTO bids (lot_id, user_id, amount) VALUES ($1, $2, $3) RETURNING id, lot_id, user_id, amount, created_at",
		lotID, userID, amount)
	err := row.Scan(&bid.ID, &bid.LotID, &bid.UserID, &bid.Amount, &bid.CreatedAt)
	return bid, err
}

func (q *Queries) GetUserBid(lotID, userID int64) (model.Bid, error) {
	var bid model.Bid
	row := q.db.QueryRowContext(context.Background(),
		"SELECT id, lot_id, user_id, amount, created_at FROM bids WHERE lot_id = $1 AND user_id = $2", lotID, userID)
	err := row.Scan(&bid.ID, &bid.LotID, &bid.UserID, &bid.Amount, &bid.CreatedAt)
	return bid, err
}

func (q *Queries) GetCategories() ([]model.Category, error) {
	rows, err := q.db.QueryContext(context.Background(),
		"SELECT id, name, description FROM categories ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := make(map[string]*model.Category)
	var result []model.Category

	for rows.Next() {
		var cat model.Category
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.Description); err != nil {
			return nil, err
		}
		categories[cat.ID] = &cat
		result = append(result, cat)
	}

	rowsSub, err := q.db.QueryContext(context.Background(),
		"SELECT id, category_id, name FROM subcategories ORDER BY category_id, id")
	if err != nil {
		return nil, err
	}
	defer rowsSub.Close()

	for rowsSub.Next() {
		var sub model.Subcategory
		var catID string
		if err := rowsSub.Scan(&sub.ID, &catID, &sub.Name); err != nil {
			return nil, err
		}
		if cat, ok := categories[catID]; ok {
			cat.Subs = append(cat.Subs, sub)
		}
	}

	return result, nil
}

func (q *Queries) GetDorms() ([]model.Dorm, error) {
	rows, err := q.db.QueryContext(context.Background(),
		"SELECT id, name FROM dorms ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dorms []model.Dorm
	for rows.Next() {
		var d model.Dorm
		if err := rows.Scan(&d.ID, &d.Name); err != nil {
			return nil, err
		}
		dorms = append(dorms, d)
	}
	return dorms, nil
}

func (q *Queries) GetUserStats(userID int64) (model.StatsResponse, error) {
	var stats model.StatsResponse

	row := q.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM lots WHERE author_id = $1", userID)
	row.Scan(&stats.LotsCreated)

	rows, _ := q.db.QueryContext(context.Background(),
		`SELECT
			COUNT(CASE WHEN l.status = 'finished' AND b1.user_id = $1 THEN 1 END),
			COUNT(CASE WHEN l.status = 'finished' AND b1.user_id != $1 AND b1.amount = (SELECT MAX(amount) FROM bids WHERE lot_id = l.id) THEN 1 END)
		FROM lots l
		LEFT JOIN bids b1 ON l.id = b1.lot_id AND l.status = 'finished'
		WHERE l.author_id = $1`, userID)
	if rows.Next() {
		rows.Scan(&stats.LotsWon, &stats.LotsLost)
	}

	row = q.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM bids WHERE user_id = $1", userID)
	row.Scan(&stats.TotalBids)

	return stats, nil
}

func (q *Queries) CountLots(status string) (int, error) {
	var count int
	row := q.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM lots WHERE status = $1", status)
	row.Scan(&count)
	return count, nil
}
