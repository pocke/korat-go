package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
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
			accessToken string not null,

			_createdAt   integer not null,
			_updatedAt   integer not null
		);

		create table channels (
			id            integer not null primary key,
			displayName   string not null,
			system        string,
			queries       string not null,

			accountID     integer not null,

			_createdAt    integer not null,
			_updatedAt    integer not null,

			FOREIGN KEY(accountID) REFERENCES accounts(id)
		);

		create table github_users (
			id          integer not null primary key,
			login       string not null,
			avatarURL   string not null,

			_createdAt   integer not null,
			_updatedAt   integer not null
		);

		create table issues (
			id            integer not null primary key,
			number        integer not null,
			title         string not null,
			userID        integer not null,
			repoOwner     string not null,
			repoName      string not null,
			state         string not null,
			locked        string not null,
			comments      integer not null,
			createdAt     integer not null,
			updatedAt     integer not null,
			closedAt      integer,
			isPullRequest boolean not null,
			body          string not null,
			alreadyRead   boolean not null,

			_createdAt   integer not null,
			_updatedAt   integer not null,

			FOREIGN KEY(userID) REFERENCES github_users(id)
		);

		create table labels (
			id          integer not null primary key,
			name        string not null,
			color       string not null,
			'default'   boolean not null,

			_createdAt   integer not null,
			_updatedAt   integer not null
		);

		create table milestones (
			id            integer not null primary key,
			number        integer not null,
			title         string not null,
			description   string not null,
			state         string not null,
			createdAt     integer not null,
			updatedAt     integer not null,
			closedAt      integer,

			_createdAt   integer not null,
			_updatedAt   integer not null
		);

		create table assigned_labels_to_issue (
			id          integer not null primary key,
			issueID     integer not null,
			labelID     integer not null,

			_createdAt   integer not null,
			_updatedAt   integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id)
			FOREIGN KEY(labelID) REFERENCES labels(id)
		);

		create table assigned_users_to_issue (
			id          integer not null primary key,
			issueID     integer not null,
			userID      integer not null,

			_createdAt   integer not null,
			_updatedAt   integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id)
			FOREIGN KEY(userID) REFERENCES github_users(id)
		);

		create table assigned_milestones_to_issue (
			id          integer not null primary key,
			issueID     integer not null,
			milestoneID integer not null,

			_createdAt   integer not null,
			_updatedAt   integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id)
			FOREIGN KEY(milestoneID) REFERENCES milestones(id)
		);
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func doMigration(id int, query string) error {
	return tx(func(tx *sql.Tx) error {
		row := tx.QueryRow(`select id from migration_info where id = ?`, id)
		var blackhole int
		err := row.Scan(&blackhole)

		if err == sql.ErrNoRows {
			_, err := tx.Exec(query)
			if err != nil {
				return errors.WithStack(err)
			}
			_, err = tx.Exec(`insert into migration_info(id) values(?)`, id)
			if err != nil {
				return errors.WithStack(err)
			}
		} else if err != nil {
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
	fname, err := homedir.Expand("~/.cache/korat/development.sqlite3")
	if err != nil {
		panic(err)
	}

	db, err := sql.Open("sqlite3", fname)
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
