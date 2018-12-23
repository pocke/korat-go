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
	var tableName string
	err := row.Scan(&tableName)
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
			id integer not null primary key,
			displayName string not null,
			urlBase string not null,
			apiUrlBase string not null,
			accessToken string not null
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
		var id int
		err := row.Scan(&id)

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

	err = dbMigrate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}
