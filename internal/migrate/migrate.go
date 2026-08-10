package migrate

import (
	"context"
	"embed"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed sql/*.sql
var migrations embed.FS

func Up(ctx context.Context, databaseURL string) error {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return err
	}
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	sub, err := fs.Sub(migrations, "sql")
	if err != nil {
		return err
	}
	goose.SetBaseFS(sub)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, ".")
}
