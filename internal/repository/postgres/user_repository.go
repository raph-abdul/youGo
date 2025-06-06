// Copyright 2025 raph-abdul
// Licensed under the Apache License, Version 2.0.
// Visit http://www.apache.org/licenses/LICENSE-2.0 for details

// Package postgres /youGo/internal/repository/postgres/user_repository.go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"

	"gorm.io/gorm"
	// Import pq driver explicitly if needing to check for specific pq errors like unique violation
	"github.com/jackc/pgx/v5/pgconn"

	"youGo/internal/domain" // Import the domain package for interfaces and entities
)

// UserModel defines the GORM database model for a User in the PostgreSQL database.
// It might include GORM-specific tags for table name, column types, indexes etc.
// It should map closely to the fields in domain.User.
type UserModel struct {
	// gorm.Model // Optional: Embed gorm.Model for ID, CreatedAt, UpdatedAt, DeletedAt
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"` // Example using Postgres function for UUIDs
	Name         string    `gorm:"size:255;not null"`
	Email        string    `gorm:"size:255;uniqueIndex;not null"` // uniqueIndex creates a unique index
	PasswordHash string    `gorm:"not null"`
	IsActive     bool      `gorm:"default:true;not null"`
	Role         string    `gorm:"size:50;not null"`
	CreatedAt    time.Time // GORM automatically handles this if not embedding gorm.Model
	UpdatedAt    time.Time // GORM automatically handles this if not embedding gorm.Model
	// DeletedAt gorm.DeletedAt `gorm:"index"` // Include if using GORM soft deletes
}

// TableName explicitly sets the table name for the UserModel struct.
// GORM defaults to pluralizing the struct name (e.g., "user_models").
func (UserModel) TableName() string {
	return "users" // Or your preferred table name
}

// postgresUserRepository implements domain.UserRepository using GORM/Postgres.
type postgresUserRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new GORM/Postgres user repository instance.
func NewUserRepository(db *gorm.DB) domain.UserRepository {
	//// Auto-migrate the schema for the UserModel.
	//// WARNING: AutoMigrate is convenient but lacks features of full migration tools.
	//// Be cautious in production. Consider using migrate.sh with SQL files instead.
	//err := db.AutoMigrate(&UserModel{})
	//if err != nil {
	//	// Use panic here as failure to migrate likely means the app cannot start correctly.
	//	panic(fmt.Sprintf("failed to auto-migrate User model: %v", err))
	//}
	//fmt.Println("User model migration check/execution complete.") // Add log
	return &postgresUserRepository{db: db}
}

// --- Mapping Functions ---
// Convert between the database model (UserModel) and the domain entity (domain.User).

func toDomainUser(model *UserModel) *domain.User {
	if model == nil {
		return nil
	}
	return &domain.User{
		ID:           model.ID,
		Name:         model.Name,
		Email:        model.Email,
		PasswordHash: model.PasswordHash, // Be careful not to expose this unnecessarily outside auth service
		IsActive:     model.IsActive,
		Role:         model.Role,
		CreatedAt:    model.CreatedAt,
		UpdatedAt:    model.UpdatedAt,
	}
}

func fromDomainUser(dUser *domain.User) *UserModel {
	if dUser == nil {
		return nil
	}
	// Important: Ensure timestamps are handled correctly.
	// GORM usually manages CreatedAt/UpdatedAt on create/update.
	// If ID is generated by DB (like gen_random_uuid()), it might be empty initially.
	return &UserModel{
		ID:           dUser.ID, // Pass ID if known (e.g., for updates)
		Name:         dUser.Name,
		Email:        dUser.Email,
		PasswordHash: dUser.PasswordHash,
		IsActive:     dUser.IsActive,
		Role:         dUser.Role,
		CreatedAt:    dUser.CreatedAt, // Often managed by GORM
		UpdatedAt:    dUser.UpdatedAt, // Often managed by GORM
	}
}

// --- Interface Implementation ---

func (r *postgresUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var model UserModel
	err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error // Use First for primary key lookup
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound // Map to domain error
		}
		// Log the underlying error for debugging?
		// r.logger.Error(...)
		return nil, fmt.Errorf("db error finding user by id [%s]: %w", id, err)
	}
	return toDomainUser(&model), nil
}

func (r *postgresUserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var model UserModel
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound // Map to domain error
		}
		return nil, fmt.Errorf("db error finding user by email [%s]: %w", email, err)
	}
	return toDomainUser(&model), nil
}

func (r *postgresUserRepository) Create(ctx context.Context, user *domain.User) error {
	model := fromDomainUser(user)
	// GORM hooks or DB defaults usually handle CreatedAt/UpdatedAt and potentially ID (like gen_random_uuid())
	err := r.db.WithContext(ctx).Create(model).Error
	if err != nil {
		var pgErr *pgconn.PgError
		// Check if the error is a PostgreSQL error and specifically code 23505 (unique_violation)
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// You can optionally check pgErr.ConstraintName if needed (e.g., contains "email")
			// but returning ErrDuplicateEntry for any unique violation on create is usually okay.
			return domain.ErrDuplicateEntry // Map to domain error
		}

		// Fallback check for GORM's generic duplicate key error, though the pgErr check is more specific
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return domain.ErrDuplicateEntry
		}

		// If it's not a known duplicate error, wrap and return a generic error
		return fmt.Errorf("db error creating user: %w", err)
	}
	// Important: If the ID or timestamps were generated by the DB, update the original domain object
	user.ID = model.ID
	user.CreatedAt = model.CreatedAt
	user.UpdatedAt = model.UpdatedAt
	return nil
}

func (r *postgresUserRepository) Update(ctx context.Context, user *domain.User) error {
	if user.ID == uuid.Nil {
		return domain.ErrNotFound // Or InvalidArgumentError
	}
	model := fromDomainUser(user)
	// Use .Model(&UserModel{}).Where("id = ?", user.ID).Updates(model) for partial updates (non-zero fields)
	// GORM handles UpdatedAt automatically here
	result := r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", user.ID).Updates(model)
	if result.Error != nil {
		var pgErr *pgconn.PgError
		// Check for unique constraint violation on email if it was updated
		if errors.As(result.Error, &pgErr) && pgErr.Code == "23505" {
			// Optionally check pgErr.ConstraintName if you have multiple unique constraints
			return domain.ErrDuplicateEntry
		}
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
			return domain.ErrDuplicateEntry
		}
		return fmt.Errorf("db error updating user [%s]: %w", user.ID, result.Error)
	}
	// Check if any row was actually updated
	if result.RowsAffected == 0 {
		// This might mean the record didn't exist OR the data was identical.
		// For simplicity, treat 0 rows affected on update often as NotFound.
		return domain.ErrNotFound
	}
	// Assume service layer handles setting UpdatedAt if needed before passing 'user'
	return nil
}

func (r *postgresUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	// GORM performs soft delete if gorm.DeletedAt field exists in UserModel
	// Use .Unscoped().Delete(...) for hard delete.
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&UserModel{})
	if result.Error != nil {
		return fmt.Errorf("db error deleting user [%s]: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound // User to delete was not found
	}
	return nil
}
