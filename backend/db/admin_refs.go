package db

import (
	"auction/model"
	"context"
	"database/sql"
)

// Справочники (категории, подкатегории, общежития) ведёт администратор.
// Каждое изменение пишется в журнал в той же транзакции.

func (q *Queries) refTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func adminEvent(ctx context.Context, tx *sql.Tx, adminID int64, typ string, details map[string]any) error {
	return insertEvent(ctx, tx, EventInput{Type: typ, UserID: adminID, ActorID: adminID, ByAdmin: true, Details: details})
}

// CreateCategory добавляет категорию. ErrConflict — такой id уже есть.
func (q *Queries) CreateCategory(ctx context.Context, adminID int64, id, name, description string) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO categories (id, name, description) VALUES ($1, $2, $3)`, id, name, description); err != nil {
			return mapErr(err)
		}
		return adminEvent(ctx, tx, adminID, model.EvCategoryCreated, map[string]any{"id": id, "name": name})
	})
}

func (q *Queries) UpdateCategory(ctx context.Context, adminID int64, id, name, description string) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE categories SET name = $2, description = $3 WHERE id = $1`, id, name, description)
		if err != nil {
			return mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return adminEvent(ctx, tx, adminID, model.EvCategoryUpdated, map[string]any{"id": id, "name": name})
	})
}

// DeleteCategory удаляет категорию вместе с подкатегориями. ErrBadRef — на неё ссылаются лоты.
func (q *Queries) DeleteCategory(ctx context.Context, adminID int64, id string) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		var name string
		if err := tx.QueryRowContext(ctx, `DELETE FROM categories WHERE id = $1 RETURNING name`, id).Scan(&name); err != nil {
			return mapErr(err)
		}
		return adminEvent(ctx, tx, adminID, model.EvCategoryDeleted, map[string]any{"id": id, "name": name})
	})
}

// CreateSubcategory добавляет подкатегорию. ErrNotFound — нет такой категории,
// ErrConflict — id подкатегории уже занят (он уникален во всём справочнике).
func (q *Queries) CreateSubcategory(ctx context.Context, adminID int64, categoryID, id, name string) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE id = $1)`, categoryID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO subcategories (id, category_id, name) VALUES ($1, $2, $3)`, id, categoryID, name); err != nil {
			return mapErr(err)
		}
		return adminEvent(ctx, tx, adminID, model.EvSubCreated,
			map[string]any{"id": id, "categoryId": categoryID, "name": name})
	})
}

func (q *Queries) UpdateSubcategory(ctx context.Context, adminID int64, categoryID, id, name string) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE subcategories SET name = $3 WHERE category_id = $1 AND id = $2`, categoryID, id, name)
		if err != nil {
			return mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return adminEvent(ctx, tx, adminID, model.EvSubUpdated,
			map[string]any{"id": id, "categoryId": categoryID, "name": name})
	})
}

// DeleteSubcategory удаляет подкатегорию. ErrBadRef — на неё ссылаются лоты.
func (q *Queries) DeleteSubcategory(ctx context.Context, adminID int64, categoryID, id string) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		var name string
		err := tx.QueryRowContext(ctx,
			`DELETE FROM subcategories WHERE category_id = $1 AND id = $2 RETURNING name`, categoryID, id).Scan(&name)
		if err != nil {
			return mapErr(err)
		}
		return adminEvent(ctx, tx, adminID, model.EvSubDeleted,
			map[string]any{"id": id, "categoryId": categoryID, "name": name})
	})
}

// maxDormID — предел SMALLINT.
const maxDormID = 32767

// CreateDorm добавляет общежитие с очередным номером. ErrConflict — название уже есть.
func (q *Queries) CreateDorm(ctx context.Context, adminID int64, name string) (model.Dorm, error) {
	var d model.Dorm
	err := q.refTx(ctx, func(tx *sql.Tx) error {
		// номера выдаются как MAX+1: параллельные добавления выстраиваем в очередь
		if _, err := tx.ExecContext(ctx, `LOCK TABLE dorms IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}
		var dup bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM dorms WHERE lower(name) = lower($1))`, name).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return ErrConflict
		}
		var next int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) + 1 FROM dorms`).Scan(&next); err != nil {
			return err
		}
		if next > maxDormID {
			return model.Errf(409, "limit", "Достигнут предел числа общежитий")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO dorms (id, name) VALUES ($1, $2)`, next, name); err != nil {
			return mapErr(err)
		}
		d = model.Dorm{ID: int16(next), Name: name}
		return adminEvent(ctx, tx, adminID, model.EvDormCreated, map[string]any{"id": next, "name": name})
	})
	return d, err
}

func (q *Queries) UpdateDorm(ctx context.Context, adminID int64, id int16, name string) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		var dup bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM dorms WHERE lower(name) = lower($1) AND id <> $2)`, name, id).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return ErrConflict
		}
		res, err := tx.ExecContext(ctx, `UPDATE dorms SET name = $2 WHERE id = $1`, id, name)
		if err != nil {
			return mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return adminEvent(ctx, tx, adminID, model.EvDormUpdated, map[string]any{"id": id, "name": name})
	})
}

// DeleteDorm удаляет общежитие. ErrBadRef — в нём живут пользователи.
func (q *Queries) DeleteDorm(ctx context.Context, adminID int64, id int16) error {
	return q.refTx(ctx, func(tx *sql.Tx) error {
		var name string
		if err := tx.QueryRowContext(ctx, `DELETE FROM dorms WHERE id = $1 RETURNING name`, id).Scan(&name); err != nil {
			return mapErr(err)
		}
		return adminEvent(ctx, tx, adminID, model.EvDormDeleted, map[string]any{"id": id, "name": name})
	})
}
