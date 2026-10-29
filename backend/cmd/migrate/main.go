// Command migrate runs SQL migrations, optionally seeds a demo test app, and
// recovers console access with -reset-admin.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/config"
	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
)

func main() {
	var (
		down       = flag.Bool("down", false, "roll back all migrations")
		seed       = flag.Bool("seed", false, "seed a demo test app after migrating")
		resetAdmin = flag.Bool("reset-admin", false, "reset a console user's password, or create the first owner")
		email      = flag.String("email", "", "user for -reset-admin (default: the oldest owner)")
		password   = flag.String("password", "", "new password for -reset-admin (generated if empty)")
	)
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	m, err := migrate.New("file://migrations", cfg.PostgresDSN)
	if err != nil {
		fatal(err)
	}

	if *down {
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fatal(err)
		}
		fmt.Println("migrations rolled back")
		return
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		fatal(err)
	}
	fmt.Println("migrations applied")

	if !*seed && !*resetAdmin {
		return
	}
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		fatal(err)
	}
	defer pool.Close()

	if *seed {
		cipher, err := crypto.New(cfg.EncryptionKey)
		if err != nil {
			fatal(err)
		}
		if err := runSeed(ctx, pool, cipher, os.Stdout); err != nil {
			fatal(err)
		}
	}
	if *resetAdmin {
		hasher := service.NewBcryptHasher(service.DefaultBcryptCost)
		if err := runResetAdmin(ctx, pool, hasher, *email, *password, os.Stdout); err != nil {
			fatal(err)
		}
	}
}

// runSeed creates the demo test app. It never creates a user: the first
// owner is made in the console's first-run setup or with -reset-admin.
func runSeed(ctx context.Context, pool *pgxpool.Pool, cipher service.SecretCipher, out io.Writer) error {
	ctx = domain.WithActor(ctx, domain.SystemActor("seed"))
	const slug = "demo"
	created, err := seedApp(ctx, pool, cipher, "Demo App", slug, domain.EnvTest)
	if err != nil {
		return err
	}
	if created == nil {
		fmt.Fprintf(out, "app %s (%s) already exists, skipping\n", slug, domain.EnvTest)
	} else {
		fmt.Fprintf(out, "created app %s (%s, %s)\n  api_key=%s\n  api_secret=%s\n",
			created.App.Slug, created.App.PublicID, created.App.Environment,
			created.APIKey, created.APISecret)
	}

	n, err := postgres.NewUserRepo(pool).Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Fprintln(out, "no console user yet: open the console to create the first owner, or run -reset-admin -email <email>")
	}
	return nil
}

// seedApp creates an app through AppService, so it is audited as the actor
// in ctx. It returns nil when the slug already exists in env.
func seedApp(ctx context.Context, pool *pgxpool.Pool, cipher service.SecretCipher, name, slug string, env domain.Environment) (*service.AppCreated, error) {
	apps := postgres.NewAppRepo(pool)
	if _, err := apps.GetBySlug(ctx, env, slug); err == nil {
		return nil, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("check app %s: %w", slug, err)
	}
	audit := service.NewAuditService(postgres.NewAuditRepo(pool))
	svc := service.NewAppService(apps, postgres.NewAPIKeyRepo(pool), postgres.NewConnectionRepo(pool), cipher,
		postgres.NewTxManager(pool), audit)
	created, err := svc.Create(ctx, service.CreateAppInput{Name: name, Slug: slug, Environment: env})
	if err != nil {
		return nil, fmt.Errorf("seed %s: %w", slug, err)
	}
	return created, nil
}

// runResetAdmin recovers console access from the server shell, audited as
// system:cli. See service.UserService.Recover for the rules.
func runResetAdmin(ctx context.Context, pool *pgxpool.Pool, hasher service.PasswordHasher, email, password string, out io.Writer) error {
	ctx = domain.WithActor(ctx, domain.SystemActor("cli"))
	audit := service.NewAuditService(postgres.NewAuditRepo(pool))
	users := service.NewUserService(postgres.NewUserRepo(pool), postgres.NewTxManager(pool), audit, hasher)
	res, err := users.Recover(ctx, email, password)
	if err != nil {
		return err
	}
	if res.Created {
		fmt.Fprintln(out, "created owner")
	} else {
		fmt.Fprintln(out, "password reset; the user is enabled and signed out everywhere")
	}
	fmt.Fprintf(out, "  email=%s\n", res.User.Email)
	if res.Password != "" {
		fmt.Fprintf(out, "  password=%s\n", res.Password)
		fmt.Fprintln(out, "NOTE: store the password now; it is not recoverable in plaintext later. It must be changed at first sign-in.")
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", describe(err))
	os.Exit(1)
}

// describe turns field validation errors into a readable line.
func describe(err error) string {
	var coded *domain.Error
	if errors.As(err, &coded) {
		if fields, ok := coded.Details["fields"].([]domain.FieldError); ok && len(fields) > 0 {
			msg := "invalid input:"
			for _, f := range fields {
				msg += fmt.Sprintf(" %s (%s)", f.Field, f.Rule)
			}
			return msg
		}
	}
	return err.Error()
}
