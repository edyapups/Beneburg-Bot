package database

import (
	"beneburg/pkg/database/model"
	"context"
	"database/sql"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// ResultInfo is the small result DTO callers need from update operations.
type ResultInfo struct{ RowsAffected int64 }

//go:generate mockgen -source=database.go -destination=./mocks/mock_database.go -package=mock_database
type Database interface {
	Migrate(ctx context.Context) error
	CreateUser(context.Context, *model.User) (*model.User, error)
	UpdateOrCreateUser(context.Context, *model.User) (*model.User, error)
	CreateOrProlongToken(context.Context, int64) (*model.Token, error)
	GetUserByToken(context.Context, string) (*model.User, error)
	GetAllUsers(context.Context) ([]*model.User, error)
	GetUserByID(context.Context, uint) (*model.User, error)
	GetUserByTelegramID(context.Context, int64) (*model.User, error)
	UpdateUserByID(context.Context, uint, *model.User) (*model.User, error)
	AcceptUser(context.Context, uint) (*ResultInfo, error)
	RejectUser(context.Context, uint) (*ResultInfo, error)
	SetUserStatus(context.Context, uint, string) (*ResultInfo, error)
	CreateForm(context.Context, *model.Form) (*model.Form, error)
	GetFormByID(context.Context, uint) (*model.Form, error)
	AcceptForm(context.Context, uint) (*ResultInfo, error)
	RejectForm(context.Context, uint) (*ResultInfo, error)
	GetActualForm(context.Context, int64) (*model.Form, error)
	GetLastForm(context.Context, int64) (*model.Form, error)
	GetAllUserForms(context.Context, int64) ([]*model.Form, error)
	GetAllForms(context.Context) ([]*model.Form, error)
	GetAllAcceptedFormsWithUser(context.Context) ([]*model.Form, error)
}

type database struct {
	db      *sql.DB
	logger  *zap.Logger
	uuidGen func() uuid.UUID
}

var _ Database = (*database)(nil)
var qb = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

func NewDatabase(dsn string, logger *zap.Logger) (Database, error) {
	if dsn == "" {
		dsn = "beneburg.db"
	}
	db, err := sql.Open("sqlite3", dsn+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return NewDatabaseWithDB(db, logger), nil
}
func NewDatabaseWithDB(db *sql.DB, logger *zap.Logger) Database {
	return &database{db: db, logger: logger, uuidGen: uuid.New}
}

func (d *database) Migrate(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	for _, migration := range migrations {
		var found int
		if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, migration.version).Scan(&found); err != nil {
			return err
		}
		if found != 0 {
			continue
		}
		tx, err := d.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, migration.sql); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(?)`, migration.version)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (d *database) CreateUser(ctx context.Context, u *model.User) (*model.User, error) {
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
	q, a, err := qb.Insert(model.TableNameUser).Columns("created_at", "updated_at", "telegram_id", "username", "first_name", "last_name", "status").Values(u.CreatedAt, u.UpdatedAt, u.TelegramID, u.Username, u.FirstName, u.LastName, defaultUserStatus(u.Status)).ToSql()
	if err != nil {
		return nil, err
	}
	r, err := d.db.ExecContext(ctx, q, a...)
	if err != nil {
		return nil, err
	}
	id, _ := r.LastInsertId()
	u.ID = uint(id)
	u.Status = defaultUserStatus(u.Status)
	return u, nil
}
func defaultUserStatus(status string) string {
	if status == "" {
		return model.UserStatusNew
	}
	return status
}

func (d *database) UpdateOrCreateUser(ctx context.Context, u *model.User) (*model.User, error) {
	values := map[string]interface{}{"updated_at": time.Now().UTC()}
	if u.FirstName != "" {
		values["first_name"] = u.FirstName
	}
	if u.LastName != nil {
		values["last_name"] = u.LastName
	}
	if u.Username != nil {
		values["username"] = u.Username
	}
	if u.Status == model.UserStatusActive || u.Status == model.UserStatusNotActive {
		values["status"] = u.Status
	}
	q, a, err := qb.Update(model.TableNameUser).SetMap(values).Where(squirrel.Eq{"telegram_id": u.TelegramID}).ToSql()
	if err != nil {
		return nil, err
	}
	r, err := d.db.ExecContext(ctx, q, a...)
	if err != nil {
		return nil, err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return d.CreateUser(ctx, u)
	}
	return d.GetUserByTelegramID(ctx, u.TelegramID)
}
func (d *database) CreateOrProlongToken(ctx context.Context, telegramID int64) (*model.Token, error) {
	t := &model.Token{UUID: d.uuidGen().String(), UserTelegramId: telegramID, ExpireAt: time.Now().UTC().Add(24 * time.Hour)}
	q, a, err := qb.Insert(model.TableNameToken).Columns("uuid", "user_telegram_id", "expire_at").Values(t.UUID, t.UserTelegramId, t.ExpireAt).Suffix("ON CONFLICT(user_telegram_id) DO UPDATE SET uuid=excluded.uuid, expire_at=excluded.expire_at").ToSql()
	if err != nil {
		return nil, err
	}
	_, err = d.db.ExecContext(ctx, q, a...)
	return t, err
}
func (d *database) GetUserByToken(ctx context.Context, token string) (*model.User, error) {
	q, a, _ := qb.Select(userColumns("u")...).From("users u JOIN tokens t ON t.user_telegram_id = u.telegram_id").Where(squirrel.Eq{"t.uuid": token}).Where("t.expire_at > CURRENT_TIMESTAMP").Where("u.deleted_at IS NULL").ToSql()
	return scanUser(d.db.QueryRowContext(ctx, q, a...))
}
func (d *database) GetAllUsers(ctx context.Context) ([]*model.User, error) {
	q, a, _ := qb.Select(userColumns("")...).From("users").Where("deleted_at IS NULL").ToSql()
	return scanUsers(d.db.QueryContext(ctx, q, a...))
}
func (d *database) GetUserByID(ctx context.Context, id uint) (*model.User, error) {
	q, a, _ := qb.Select(userColumns("")...).From("users").Where(squirrel.Eq{"id": id}).Where("deleted_at IS NULL").ToSql()
	return scanUser(d.db.QueryRowContext(ctx, q, a...))
}
func (d *database) GetUserByTelegramID(ctx context.Context, id int64) (*model.User, error) {
	q, a, _ := qb.Select(userColumns("")...).From("users").Where(squirrel.Eq{"telegram_id": id}).Where("deleted_at IS NULL").ToSql()
	return scanUser(d.db.QueryRowContext(ctx, q, a...))
}
func (d *database) UpdateUserByID(ctx context.Context, id uint, u *model.User) (*model.User, error) {
	q, a, e := qb.Update("users").Set("username", u.Username).Set("first_name", u.FirstName).Set("last_name", u.LastName).Set("status", u.Status).Set("updated_at", time.Now().UTC()).Where(squirrel.Eq{"id": id}).ToSql()
	if e != nil {
		return nil, e
	}
	if _, e = d.db.ExecContext(ctx, q, a...); e != nil {
		return nil, e
	}
	return d.GetUserByID(ctx, id)
}
func (d *database) setUserStatus(ctx context.Context, id uint, status string) (*ResultInfo, error) {
	q, a, e := qb.Update("users").Set("status", status).Set("updated_at", time.Now().UTC()).Where(squirrel.Eq{"id": id}).ToSql()
	if e != nil {
		return nil, e
	}
	r, e := d.db.ExecContext(ctx, q, a...)
	if e != nil {
		return nil, e
	}
	n, _ := r.RowsAffected()
	return &ResultInfo{n}, nil
}
func (d *database) AcceptUser(c context.Context, id uint) (*ResultInfo, error) {
	return d.setUserStatus(c, id, model.UserStatusAccepted)
}
func (d *database) RejectUser(c context.Context, id uint) (*ResultInfo, error) {
	return d.setUserStatus(c, id, model.UserStatusRejected)
}
func (d *database) SetUserStatus(c context.Context, id uint, s string) (*ResultInfo, error) {
	return d.setUserStatus(c, id, s)
}

func (d *database) CreateForm(ctx context.Context, f *model.Form) (*model.Form, error) {
	now := time.Now().UTC()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	if f.Gender == "" {
		f.Gender = "undefined"
	}
	if f.Status == "" {
		f.Status = model.FormStatusNew
	}
	q, a, e := qb.Insert("forms").Columns("created_at", "updated_at", "user_telegram_id", "name", "age", "gender", "about", "hobbies", "work", "education", "cover_letter", "contacts", "status").Values(f.CreatedAt, f.UpdatedAt, f.UserTelegramId, f.Name, f.Age, f.Gender, f.About, f.Hobbies, f.Work, f.Education, f.CoverLetter, f.Contacts, f.Status).ToSql()
	if e != nil {
		return nil, e
	}
	r, e := d.db.ExecContext(ctx, q, a...)
	if e != nil {
		return nil, e
	}
	id, _ := r.LastInsertId()
	f.ID = uint(id)
	return f, nil
}
func (d *database) GetFormByID(ctx context.Context, id uint) (*model.Form, error) {
	q, a, _ := qb.Select(formColumns("f")...).From("forms f JOIN users u ON u.telegram_id=f.user_telegram_id").Where(squirrel.Eq{"f.id": id}).Where("f.deleted_at IS NULL").ToSql()
	return scanForm(d.db.QueryRowContext(ctx, q, a...))
}
func (d *database) setFormStatus(ctx context.Context, id uint, status string) (*ResultInfo, error) {
	q, a, e := qb.Update("forms").Set("status", status).Set("updated_at", time.Now().UTC()).Where(squirrel.Eq{"id": id}).ToSql()
	if e != nil {
		return nil, e
	}
	r, e := d.db.ExecContext(ctx, q, a...)
	if e != nil {
		return nil, e
	}
	n, _ := r.RowsAffected()
	return &ResultInfo{n}, nil
}
func (d *database) AcceptForm(c context.Context, id uint) (*ResultInfo, error) {
	return d.setFormStatus(c, id, model.FormStatusAccepted)
}
func (d *database) RejectForm(c context.Context, id uint) (*ResultInfo, error) {
	return d.setFormStatus(c, id, model.FormStatusRejected)
}
func (d *database) forms(ctx context.Context, where ...squirrel.Sqlizer) ([]*model.Form, error) {
	b := qb.Select(formColumns("f")...).From("forms f JOIN users u ON u.telegram_id=f.user_telegram_id").Where("f.deleted_at IS NULL")
	for _, w := range where {
		b = b.Where(w)
	}
	q, a, e := b.ToSql()
	if e != nil {
		return nil, e
	}
	return scanForms(d.db.QueryContext(ctx, q, a...))
}
func (d *database) GetActualForm(c context.Context, id int64) (*model.Form, error) {
	fs, e := d.forms(c, squirrel.Eq{"f.user_telegram_id": id, "f.status": model.FormStatusAccepted}, squirrel.Expr("1=1 ORDER BY f.created_at DESC LIMIT 1"))
	if e != nil {
		return nil, e
	}
	if len(fs) == 0 {
		return nil, sql.ErrNoRows
	}
	return fs[0], nil
}
func (d *database) GetLastForm(c context.Context, id int64) (*model.Form, error) {
	fs, e := d.forms(c, squirrel.Eq{"f.user_telegram_id": id}, squirrel.Expr("1=1 ORDER BY f.created_at DESC LIMIT 1"))
	if e != nil {
		return nil, e
	}
	if len(fs) == 0 {
		return nil, sql.ErrNoRows
	}
	return fs[0], nil
}
func (d *database) GetAllUserForms(c context.Context, id int64) ([]*model.Form, error) {
	return d.forms(c, squirrel.Eq{"f.user_telegram_id": id})
}
func (d *database) GetAllForms(c context.Context) ([]*model.Form, error) { return d.forms(c) }
func (d *database) GetAllAcceptedFormsWithUser(c context.Context) ([]*model.Form, error) {
	return d.forms(c, squirrel.Expr("f.status = ?", model.FormStatusAccepted), squirrel.Expr("u.status = ?", model.UserStatusActive), squirrel.Expr("f.created_at = (SELECT MAX(f2.created_at) FROM forms f2 WHERE f2.user_telegram_id=f.user_telegram_id AND f2.status=? AND f2.deleted_at IS NULL)", model.FormStatusAccepted))
}

type scanner interface{ Scan(...interface{}) error }

func userColumns(p string) []string {
	if p != "" {
		p += "."
	}
	return []string{p + "id", p + "created_at", p + "updated_at", p + "deleted_at", p + "telegram_id", p + "username", p + "first_name", p + "last_name", p + "status"}
}
func scanUser(s scanner) (*model.User, error) {
	u := new(model.User)
	e := s.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Status)
	return u, e
}
func scanUsers(rows *sql.Rows, err error) ([]*model.User, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func formColumns(p string) []string {
	if p != "" {
		p += "."
	}
	cols := []string{}
	for _, c := range []string{"id", "created_at", "updated_at", "deleted_at", "user_telegram_id", "name", "age", "gender", "about", "hobbies", "work", "education", "cover_letter", "contacts", "status"} {
		cols = append(cols, p+c)
	}
	for _, c := range userColumns("u") {
		cols = append(cols, c)
	}
	return cols
}
func scanForm(s scanner) (*model.Form, error) {
	f := new(model.Form)
	e := s.Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt, &f.UserTelegramId, &f.Name, &f.Age, &f.Gender, &f.About, &f.Hobbies, &f.Work, &f.Education, &f.CoverLetter, &f.Contacts, &f.Status, &f.User.ID, &f.User.CreatedAt, &f.User.UpdatedAt, &f.User.DeletedAt, &f.User.TelegramID, &f.User.Username, &f.User.FirstName, &f.User.LastName, &f.User.Status)
	return f, e
}
func scanForms(rows *sql.Rows, err error) ([]*model.Form, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Form
	for rows.Next() {
		f, e := scanForm(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
