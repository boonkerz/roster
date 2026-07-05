package store

import (
	"context"
	"database/sql"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
)

func (s *Store) ListDeployPackages(ctx context.Context) ([]model.DeployPackage, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(
		`SELECT id, name, winget, choco, apt, dnf, brew FROM software_packages ORDER BY name`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DeployPackage
	for rows.Next() {
		var p model.DeployPackage
		if err := rows.Scan(&p.ID, &p.Name, &p.Winget, &p.Choco, &p.Apt, &p.Dnf, &p.Brew); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetDeployPackage(ctx context.Context, id string) (*model.DeployPackage, error) {
	var p model.DeployPackage
	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT id, name, winget, choco, apt, dnf, brew FROM software_packages WHERE id=?`), id).
		Scan(&p.ID, &p.Name, &p.Winget, &p.Choco, &p.Apt, &p.Dnf, &p.Brew)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) CreateDeployPackage(ctx context.Context, p *model.DeployPackage) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO software_packages (id, name, winget, choco, apt, dnf, brew) VALUES (?, ?, ?, ?, ?, ?, ?)`),
		p.ID, p.Name, p.Winget, p.Choco, p.Apt, p.Dnf, p.Brew)
	return err
}

func (s *Store) UpdateDeployPackage(ctx context.Context, p *model.DeployPackage) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(
		`UPDATE software_packages SET name=?, winget=?, choco=?, apt=?, dnf=?, brew=? WHERE id=?`),
		p.Name, p.Winget, p.Choco, p.Apt, p.Dnf, p.Brew, p.ID))
}

func (s *Store) DeleteDeployPackage(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM software_packages WHERE id=?`), id))
}
