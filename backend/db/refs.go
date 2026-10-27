package db

import (
	"context"

	"auction/model"
)

func (q *Queries) GetDorms(ctx context.Context) ([]model.Dorm, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT id, name FROM dorms ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dorms := make([]model.Dorm, 0)
	for rows.Next() {
		var d model.Dorm
		if err := rows.Scan(&d.ID, &d.Name); err != nil {
			return nil, err
		}
		dorms = append(dorms, d)
	}
	return dorms, rows.Err()
}

func (q *Queries) GetCategories(ctx context.Context) ([]model.Category, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT id, name, description FROM categories ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]model.Category, 0)
	index := map[string]int{}
	for rows.Next() {
		c := model.Category{Subs: []model.Subcategory{}}
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			return nil, err
		}
		index[c.ID] = len(result)
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	subs, err := q.db.QueryContext(ctx, `SELECT id, category_id, name FROM subcategories ORDER BY category_id, id`)
	if err != nil {
		return nil, err
	}
	defer subs.Close()
	for subs.Next() {
		var s model.Subcategory
		var catID string
		if err := subs.Scan(&s.ID, &catID, &s.Name); err != nil {
			return nil, err
		}
		if i, ok := index[catID]; ok {
			result[i].Subs = append(result[i].Subs, s)
		}
	}
	return result, subs.Err()
}

func (q *Queries) SubcategoryExists(ctx context.Context, categoryID, subID string) (bool, error) {
	var ok bool
	err := q.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM subcategories WHERE category_id = $1 AND id = $2)`,
		categoryID, subID).Scan(&ok)
	return ok, err
}
