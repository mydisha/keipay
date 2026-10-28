// Command migrate runs SQL migrations and optionally seeds a demo tenant and the admin user.
package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/config"
	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
)

func main() {
	var (
		down       = flag.Bool("down", false, "roll back all migrations")
		seed       = flag.Bool("seed", false, "seed a demo tenant and the admin user after migrating")
		resetAdmin = flag.Bool("reset-admin", false, "reset admin password")
		password   = flag.String("password", "", "password for reset-admin (auto-generate if empty)")
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
		if err := m.Down(); err != nil && err != migrate.ErrNoChange {
			fatal(err)
		}
		fmt.Println("migrations rolled back")
		return
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		fatal(err)
	}
	fmt.Println("migrations applied")

	if *seed {
		if err := seedDemoTenant(cfg); err != nil {
			fatal(err)
		}
		if err := seedAdmin(cfg); err != nil {
			fatal(err)
		}
	}

	if *resetAdmin {
		if err := resetAdminPassword(cfg, *password); err != nil {
			fatal(err)
		}
	}
}

func seedDemoTenant(cfg *config.Config) error {
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		return err
	}

	repo := postgres.NewTenantRepo(pool)
	svc := service.NewTenantService(repo, nil, cipher)

	const name, slug = "Demo App", "demo"
	if existing, err := repo.GetBySlug(ctx, slug); err == nil && existing != nil {
		fmt.Printf("tenant %s already exists (id=%d), skipping\n", slug, existing.ID)
		return nil
	}
	t, creds, err := svc.Create(ctx, service.CreateTenantInput{Name: name, Slug: slug})
	if err != nil {
		return fmt.Errorf("seed %s: %w", slug, err)
	}
	fmt.Printf("created tenant %s (id=%d)\n  api_key=%s\n  api_secret=%s\n",
		t.Slug, t.ID, creds.APIKey, creds.APISecret)
	fmt.Println("NOTE: store the api_secret value now; it is not recoverable in plaintext later.")
	return nil
}

func seedAdmin(cfg *config.Config) error {
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := postgres.NewAdminRepo(pool)

	// Check if admin already exists
	existing, err := repo.GetByUsername(ctx, "admin")
	if err != nil {
		return fmt.Errorf("check admin user: %w", err)
	}
	if existing != nil {
		fmt.Println("admin user already exists, skipping")
		return nil
	}

	// Generate random password (16 chars)
	passwordBytes := make([]byte, 12)
	if _, err := cryptorand.Read(passwordBytes); err != nil {
		return fmt.Errorf("generate password: %w", err)
	}
	password := hex.EncodeToString(passwordBytes)

	// Hash password with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Create admin user
	admin := &domain.AdminUser{
		Username:           "admin",
		PasswordHash:       string(hash),
		MustChangePassword: true,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := repo.Create(ctx, admin); err != nil {
		return fmt.Errorf("create admin: %w", err)
	}

	fmt.Println("created admin user")
	fmt.Printf("  username=admin\n")
	fmt.Printf("  password=%s\n", password)
	fmt.Println("NOTE: store the password now; it is not recoverable in plaintext later.")
	return nil
}

func resetAdminPassword(cfg *config.Config, password string) error {
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := postgres.NewAdminRepo(pool)

	// Check if admin exists
	existing, err := repo.GetByUsername(ctx, "admin")
	if err != nil {
		return fmt.Errorf("check admin user: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("admin user does not exist, run 'make seed' first")
	}

	// Generate password if not provided
	if password == "" {
		passwordBytes := make([]byte, 12)
		if _, err := cryptorand.Read(passwordBytes); err != nil {
			return fmt.Errorf("generate password: %w", err)
		}
		password = hex.EncodeToString(passwordBytes)
	}

	// Hash password with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Update password
	if err := repo.UpdatePassword(ctx, existing.ID, string(hash)); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	fmt.Println("admin password reset successfully")
	fmt.Printf("  username=admin\n")
	fmt.Printf("  password=%s\n", password)
	fmt.Println("NOTE: store the password now; it is not recoverable in plaintext later.")
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
