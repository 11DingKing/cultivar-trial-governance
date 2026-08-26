package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func (d *DB) InsertInstitution(ctx context.Context, executor Executor, institution domain.Institution) error {
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO institutions(id,name,region,active,created_at) VALUES(?,?,?,?,?)`,
		institution.ID, strings.TrimSpace(institution.Name), institution.Region, boolInt(institution.Active), formatTime(institution.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert institution %s: %w", institution.ID, err)
	}
	return nil
}

func (d *DB) GetInstitution(ctx context.Context, id string) (domain.Institution, error) {
	var value domain.Institution
	var active int
	var created string
	err := d.sql.QueryRowContext(ctx, `SELECT id,name,region,active,created_at FROM institutions WHERE id=?`, id).
		Scan(&value.ID, &value.Name, &value.Region, &active, &created)
	if err != nil {
		return domain.Institution{}, translateNotFound("institution", id, err)
	}
	value.Active = active == 1
	value.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.Institution{}, fmt.Errorf("scan institution %s: %w", id, err)
	}
	return value, nil
}

func (d *DB) InsertUser(ctx context.Context, executor Executor, user domain.User) error {
	if err := user.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO users(
        id,institution_id,email,display_name,role,region,active,password_hash,created_at,updated_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?)`, user.ID, user.InstitutionID, strings.ToLower(strings.TrimSpace(user.Email)),
		user.DisplayName, user.Role, user.Region, boolInt(user.Active), user.PasswordHash, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert user %s: %w", user.ID, err)
	}
	return nil
}

func scanUser(scanner interface{ Scan(...any) error }) (domain.User, error) {
	var user domain.User
	var role string
	var active int
	var created, updated string
	err := scanner.Scan(&user.ID, &user.InstitutionID, &user.Email, &user.DisplayName, &role, &user.Region, &active, &user.PasswordHash, &created, &updated)
	if err != nil {
		return domain.User{}, err
	}
	user.Role = domain.Role(role)
	user.Active = active == 1
	if user.CreatedAt, err = parseTime(created); err != nil {
		return domain.User{}, err
	}
	if user.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

const userColumns = `id,institution_id,email,display_name,role,region,active,password_hash,created_at,updated_at`

func (d *DB) GetUser(ctx context.Context, id string) (domain.User, error) {
	value, err := scanUser(d.sql.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=?`, id))
	if err != nil {
		return domain.User{}, translateNotFound("user", id, err)
	}
	return value, nil
}

func (d *DB) FindUserByEmail(ctx context.Context, email string) (domain.User, error) {
	value, err := scanUser(d.sql.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))))
	if err != nil {
		return domain.User{}, translateNotFound("user", email, err)
	}
	return value, nil
}

func (d *DB) InsertSession(ctx context.Context, executor Executor, session domain.Session) error {
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at,revoked_at,created_at) VALUES(?,?,?,?,?,?)`,
		session.ID, session.UserID, session.TokenHash, formatTime(session.ExpiresAt), nil, formatTime(session.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert session %s: %w", session.ID, err)
	}
	return nil
}

func (d *DB) FindSessionByHash(ctx context.Context, hash string) (domain.Session, domain.User, error) {
	var session domain.Session
	var user domain.User
	var expires, created string
	var revoked sql.NullString
	var role string
	var active int
	var userCreated, userUpdated string
	err := d.sql.QueryRowContext(ctx, `SELECT
        s.id,s.user_id,s.token_hash,s.expires_at,s.revoked_at,s.created_at,
        u.id,u.institution_id,u.email,u.display_name,u.role,u.region,u.active,u.password_hash,u.created_at,u.updated_at
        FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, hash).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &expires, &revoked, &created,
		&user.ID, &user.InstitutionID, &user.Email, &user.DisplayName, &role, &user.Region, &active, &user.PasswordHash, &userCreated, &userUpdated)
	if err != nil {
		return domain.Session{}, domain.User{}, translateNotFound("session", "token", err)
	}
	user.Role = domain.Role(role)
	user.Active = active == 1
	if session.ExpiresAt, err = parseTime(expires); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if session.CreatedAt, err = parseTime(created); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if session.RevokedAt, err = optionalTime(revoked); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if user.CreatedAt, err = parseTime(userCreated); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if user.UpdatedAt, err = parseTime(userUpdated); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	return session, user, nil
}

func (d *DB) RevokeSession(ctx context.Context, id string, revokedAt string) error {
	result, err := d.sql.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, revokedAt, id)
	if err != nil {
		return fmt.Errorf("revoke session %s: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke session count: %w", err)
	}
	if count == 0 {
		var existing sql.NullString
		if err := d.sql.QueryRowContext(ctx, `SELECT revoked_at FROM sessions WHERE id=?`, id).Scan(&existing); err != nil {
			return translateNotFound("session", id, err)
		}
	}
	return nil
}

func (d *DB) PurgeExpiredSessions(ctx context.Context, before string) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ? AND revoked_at IS NOT NULL`, before)
	if err != nil {
		return 0, fmt.Errorf("purge expired sessions: %w", err)
	}
	return result.RowsAffected()
}
