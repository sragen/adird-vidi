package passenger

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"adird.id/vidi/internal/shared"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// GetByID returns a passenger (user) by UUID.
func (r *Repository) GetByID(ctx context.Context, id string) (*shared.User, error) {
	u := &shared.User{}
	err := r.db.QueryRow(ctx, `
		SELECT id, phone, name, created_at FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Phone, &u.Name, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// UpdateName sets the passenger's display name.
func (r *Repository) UpdateName(ctx context.Context, id, name string) (*shared.User, error) {
	u := &shared.User{}
	err := r.db.QueryRow(ctx, `
		UPDATE users SET name = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id, phone, name, created_at
	`, id, name).Scan(&u.ID, &u.Phone, &u.Name, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update user name: %w", err)
	}
	return u, nil
}
