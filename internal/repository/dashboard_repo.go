package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

// lowStockThreshold: stock <= this counts as "needs attention" (PRD C.11).
const lowStockThreshold = 5

type DashboardRepo struct {
	pool *pgxpool.Pool
}

func NewDashboardRepo(pool *pgxpool.Pool) *DashboardRepo {
	return &DashboardRepo{pool: pool}
}

// Get computes the admin dashboard metrics (PRD C.11). from/to bound the
// order window (orders.created_at); nil = no bound. All queries reuse the
// existing status/user/created_at indexes.
func (r *DashboardRepo) Get(ctx context.Context, from, to *model.TimeWindow) (*model.Dashboard, error) {
	cond, args := periodCond(from, to)
	where := ""
	if cond != "" {
		where = "WHERE " + cond
	}

	d := &model.Dashboard{OrdersByStatus: map[string]int64{}}

	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE status = 'active'`).Scan(&d.TotalUsers); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM products WHERE is_active AND deleted_at IS NULL`).Scan(&d.TotalProducts); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT status, count(*)::bigint FROM orders `+where+`
		 GROUP BY status`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		d.OrdersByStatus[status] = n
		d.TotalOrders += n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(total_amount)::bigint, 0) FROM orders
		 WHERE status IN ('PAID', 'PROCESSING', 'SHIPPED', 'COMPLETED')`+andCond(cond), args...).Scan(&d.Revenue); err != nil {
		return nil, err
	}

	lowRows, err := r.pool.Query(ctx,
		`SELECT id, name, stock FROM products
		 WHERE is_active AND deleted_at IS NULL AND stock <= $1
		 ORDER BY stock
		 LIMIT 20`, lowStockThreshold)
	if err != nil {
		return nil, err
	}
	defer lowRows.Close()
	for lowRows.Next() {
		var p model.DashboardLowStock
		if err := lowRows.Scan(&p.ID, &p.Name, &p.Stock); err != nil {
			return nil, err
		}
		d.LowStock = append(d.LowStock, p)
	}
	return d, lowRows.Err()
}

// periodCond builds the created_at window condition ("created_at >= $1 [AND
// created_at < $2]") for the given (nullable) window, reusing the orders
// indexes. andCond wraps it for queries that already have a WHERE clause.
func periodCond(from, to *model.TimeWindow) (string, []any) {
	args := []any{}
	conds := ""
	if from != nil {
		args = append(args, *from)
		conds = "created_at >= $1"
	}
	if to != nil {
		args = append(args, *to)
		if conds != "" {
			conds += " AND created_at < $" + fmt.Sprint(len(args))
		} else {
			conds = "created_at < $1"
		}
	}
	return conds, args
}

func andCond(cond string) string {
	if cond == "" {
		return ""
	}
	return " AND " + cond
}
