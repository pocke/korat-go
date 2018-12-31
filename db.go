package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/luna-duclos/instrumentedsql"
	"github.com/mattn/go-sqlite3"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
)

var Conn *sql.DB

func dbMigrate() error {
	row := Conn.QueryRow(`select name from sqlite_master where type='table' and name='migration_info'`)
	var blackhole string
	err := row.Scan(&blackhole)
	if err == sql.ErrNoRows {
		_, err := Conn.Exec(`create table migration_info (
			id integer not null primary key
		)`)
		if err != nil {
			return errors.WithStack(err)
		}
	} else if err != nil {
		return errors.WithStack(err)
	}

	err = doMigration(1, `
		create table accounts (
			id          integer not null primary key,
			displayName string not null,
			urlBase     string not null,
			apiUrlBase  string not null,
			accessToken string not null
		);

		create table channels (
			id            integer not null primary key,
			displayName   string not null,
			system        string,
			queries       string not null,

			accountID     integer not null,

			FOREIGN KEY(accountID) REFERENCES accounts(id)
		);

		create table github_users (
			id          integer not null primary key,
			login       string not null,
			avatarURL   string not null
		);

		create table issues (
			id            integer not null primary key,
			number        integer not null,
			title         string not null,
			userID        integer not null,
			repoOwner     string not null,
			repoName      string not null,
			state         string not null,
			locked        bool not null,
			comments      integer not null,
			createdAt     string not null,
			updatedAt     string not null,
			closedAt      string,
			isPullRequest boolean not null,
			body          string not null,
			alreadyRead   boolean not null,
			milestoneID   integer,

			FOREIGN KEY(userID) REFERENCES github_users(id)
			FOREIGN KEY(milestoneID) REFERENCES milestones(id)
		);

		create table labels (
			id          integer not null primary key,
			name        string not null,
			color       string not null,
			'default'   boolean not null
		);

		create table milestones (
			id            integer not null primary key,
			number        integer not null,
			title         string not null,
			description   string not null,
			state         string not null,
			createdAt     string not null,
			updatedAt     string not null,
			closedAt      string
		);

		create table assigned_labels_to_issue (
			id          integer not null primary key,
			issueID     integer not null,
			labelID     integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id)
			FOREIGN KEY(labelID) REFERENCES labels(id)
		);
		create unique index uniq_issue_label on assigned_labels_to_issue(issueID, labelID);

		create table assigned_users_to_issue (
			id          integer not null primary key,
			issueID     integer not null,
			userID      integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id)
			FOREIGN KEY(userID) REFERENCES github_users(id)
		);
		create unique index uniq_assigned_user_to_issue on assigned_users_to_issue(issueID, userID);
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	err = doMigration(2, `
		create table channel_issues (
			id            integer not null primary key,
			issueID       integer not null,
			channelID     integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id)
			FOREIGN KEY(channelID) REFERENCES channels(id)
		);
		create unique index uniq_channel_issue on channel_issues(issueID, channelID);
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func doMigration(id int, query string) error {
	ctx := context.Background()
	return tx(func(tx *sql.Tx) error {
		exist, err := rowExist(ctx, "migration_info", id, tx)
		if err != nil {
			return err
		}

		if exist {
			return nil
		}

		_, err = tx.Exec(query)
		if err != nil {
			return errors.WithStack(err)
		}
		_, err = tx.Exec(`insert into migration_info(id) values(?)`, id)
		if err != nil {
			return errors.WithStack(err)
		}
		return nil
	})
}

func tx(f func(*sql.Tx) error) error {
	tx, err := Conn.Begin()
	if err != nil {
		return errors.WithStack(err)
	}

	err = f(tx)
	if err != nil {
		tx.Rollback()
		return errors.Wrap(err, "Transaction Rollbacked")
	}
	tx.Commit()
	return nil
}

func init() {
	logger := instrumentedsql.LoggerFunc(func(ctx context.Context, msg string, keyvals ...interface{}) {
		// log.Printf("%s %v", msg, keyvals)
	})
	sql.Register("instrumented-sqlite", instrumentedsql.WrapDriver(&sqlite3.SQLiteDriver{}, instrumentedsql.WithLogger(logger)))

	fname, err := homedir.Expand("~/.cache/korat/development.sqlite3")
	if err != nil {
		panic(err)
	}

	db, err := sql.Open("instrumented-sqlite", fname)
	if err != nil {
		panic(err)
	}
	Conn = db

	_, err = Conn.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		panic(err)
	}

	err = dbMigrate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}

type QueryRowable interface {
	QueryRow(string, ...interface{}) *sql.Row
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func rowExist(ctx context.Context, tbl string, id int, conn QueryRowable) (bool, error) {
	row := conn.QueryRowContext(ctx, fmt.Sprintf(`select 1 from %s where id = ?`, tbl), id)
	var blackhole int
	err := row.Scan(&blackhole)

	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, errors.WithStack(err)
	}
	return true, nil
}
